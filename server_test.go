package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/redhatinsights/yggdrasil_v0/protocol"
)

func TestDispatch(t *testing.T) {
	input := &pb.Data{}

	got := jsonData(input)
	want := "{\"response_to\":\"\",\"metadata\":null,\"content\":null,\"directive\":\"\"}"

	if string(got) != want {
		t.Fatalf(`Got: %q, Wanted: %q`, string(got), want)
	}
}

// The Foreman controller decodes the content field with Ruby's Base64.decode64,
// which implements the standard alphabet and silently drops characters outside
// it. Encoding with the URL-safe alphabet would therefore corrupt the payload
// rather than fail loudly: this test vector encodes to "/+++" as standard
// base64 and "_---" as URL-safe, and decode64("_---") returns an empty string.
func TestMarshalMessageUsesStandardBase64(t *testing.T) {
	content := []byte{0xFF, 0xEF, 0xBE}

	got := string(marshalMessage("id-1", "foreman_rh_cloud", nil, content))

	if !strings.Contains(got, `"content":"/+++"`) {
		t.Fatalf("expected standard-alphabet base64 %q in body, got %s", "/+++", got)
	}
	if strings.Contains(got, "_---") {
		t.Fatalf("body used the URL-safe base64 alphabet, which Foreman cannot decode: %s", got)
	}
}

// Both transports must put the same message on the wire, so that moving a
// Satellite from gRPC to D-Bus does not change what Foreman receives.
func TestForwardAndSendProduceIdenticalBody(t *testing.T) {
	metadata := map[string]string{"return_url": "https://example.com/r", "hosts": "a,b"}
	content := []byte("https://example.com/playbook.yml")

	grpcBody := jsonData(&pb.Data{
		MessageId: "msg-1",
		Metadata:  metadata,
		Content:   content,
		Directive: "foreman_rh_cloud",
	})

	// The D-Bus rx callback receives the same values as positional arguments.
	dbusBody := marshalMessage("msg-1", "foreman_rh_cloud", metadata, content)

	if string(grpcBody) != string(dbusBody) {
		t.Fatalf("transports disagree:\n gRPC: %s\nD-Bus: %s", grpcBody, dbusBody)
	}
}

func TestForwarderServer_Forward_PostsMessage(t *testing.T) {
	type received struct {
		body string
		user string
		pass string
	}
	got := make(chan received, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		got <- received{body: string(body), user: user, pass: pass}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	server := &forwarderServer{
		Url:        ts.URL,
		Username:   "testuser",
		Password:   "testpass",
		HTTPClient: &http.Client{},
	}

	metadata := map[string]string{"sat_org_id": "1"}
	if err := server.forward(nil, "foreman_rh_cloud", "msg-1", "", metadata, []byte("hello")); err != nil {
		t.Fatalf("forward returned error: %v", err)
	}

	select {
	case r := <-got:
		want := string(marshalMessage("msg-1", "foreman_rh_cloud", metadata, []byte("hello")))
		if r.body != want {
			t.Errorf("body mismatch:\n got: %s\nwant: %s", r.body, want)
		}
		if r.user != "testuser" || r.pass != "testpass" {
			t.Errorf("basic auth mismatch: got %s/%s", r.user, r.pass)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("forward did not POST to the server")
	}
}

// A failing POST must be logged and swallowed; if it took the process down,
// yggdrasil would stop delivering messages until the unit was restarted.
func TestForwarderServer_Forward_SurvivesPostFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close() // nothing is listening, so the POST fails

	server := &forwarderServer{
		Url:        ts.URL,
		Username:   "u",
		Password:   "p",
		HTTPClient: &http.Client{},
	}

	if err := server.forward(nil, "foreman_rh_cloud", "msg-1", "", nil, []byte("hello")); err != nil {
		t.Fatalf("forward should not surface transport errors, got: %v", err)
	}
}

func TestBuildHTTPClient_NoCAFile(t *testing.T) {
	t.Setenv("FORWARDER_CA_FILE", "")
	if err := os.Unsetenv("FORWARDER_CA_FILE"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	client := buildHTTPClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Transport != nil {
		t.Fatal("expected nil Transport when no CA file is set")
	}
}

func TestBuildHTTPClient_WithCAFile(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	caFile := writeTLSServerCA(t, ts)

	t.Setenv("FORWARDER_CA_FILE", caFile)
	client := buildHTTPClient()

	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("request to TLS server failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBuildHTTPClient_RejectsUnknownCA(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wrongCAFile := writeUnrelatedCA(t)

	t.Setenv("FORWARDER_CA_FILE", wrongCAFile)
	client := buildHTTPClient()

	_, err := client.Get(ts.URL)
	if err == nil {
		t.Fatal("expected TLS error when using wrong CA, got none")
	}
}

func TestForwarderServer_Send_UsesHTTPClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	server := &forwarderServer{
		Url:        ts.URL,
		Username:   "testuser",
		Password:   "testpass",
		HTTPClient: &http.Client{},
	}

	data := &pb.Data{
		MessageId: "test-123",
		Content:   []byte("hello"),
		Directive: "foreman_rh_cloud",
	}

	receipt, err := server.Send(context.TODO(), data)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected non-nil receipt")
	}
}

// forward() must use rcvId (the incoming message ID) as response_to, not the
// rx responseTo argument. This keeps the wire format identical to the gRPC
// path, which reads from pb.Data.GetMessageId().
func TestForward_UsesRcvIdAsResponseTo(t *testing.T) {
	type received struct{ body string }
	got := make(chan received, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{body: string(body)}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	server := &forwarderServer{
		Url:        ts.URL,
		Username:   "u",
		Password:   "p",
		HTTPClient: &http.Client{},
	}

	// rcvId="msg-42", responseTo="other-id" — forward must use rcvId
	if err := server.forward(nil, "foreman_rh_cloud", "msg-42", "other-id", nil, []byte("data")); err != nil {
		t.Fatalf("forward returned error: %v", err)
	}

	select {
	case r := <-got:
		if !strings.Contains(r.body, `"response_to":"msg-42"`) {
			t.Errorf("expected response_to=msg-42, got: %s", r.body)
		}
		if strings.Contains(r.body, `"response_to":"other-id"`) {
			t.Error("forward must not use the rx responseTo argument")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("forward did not POST")
	}
}

func TestMarshalMessage_NilContent(t *testing.T) {
	got := string(marshalMessage("id-1", "d", nil, nil))
	if !strings.Contains(got, `"content":null`) {
		t.Errorf("nil content should marshal as null, got: %s", got)
	}
}

func TestMarshalMessage_EmptyContent(t *testing.T) {
	got := string(marshalMessage("id-1", "d", nil, []byte{}))
	// encoding/json marshals []byte{} as "" (empty base64)
	if !strings.Contains(got, `"content":""`) {
		t.Errorf("empty content should marshal as empty string, got: %s", got)
	}
}

func TestMarshalMessage_PreservesAllFields(t *testing.T) {
	metadata := map[string]string{"key": "val"}
	content := []byte("payload")
	got := string(marshalMessage("resp-1", "foreman_rh_cloud", metadata, content))

	for _, want := range []string{
		`"response_to":"resp-1"`,
		`"directive":"foreman_rh_cloud"`,
		`"key":"val"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s in body, got: %s", want, got)
		}
	}
}

func TestLoadConfig_ExportsEnvVars(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman_rh_cloud.toml")
	configContent := `env = ["TEST_LOAD_CFG_A=alpha", "TEST_LOAD_CFG_B=beta"]` + "\n"
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONFIG_FILE", configFile)
	// Clean up env vars we're about to set
	t.Cleanup(func() {
		_ = os.Unsetenv("TEST_LOAD_CFG_A")
		_ = os.Unsetenv("TEST_LOAD_CFG_B")
	})

	loadConfig()

	if got := os.Getenv("TEST_LOAD_CFG_A"); got != "alpha" {
		t.Errorf("TEST_LOAD_CFG_A = %q, want %q", got, "alpha")
	}
	if got := os.Getenv("TEST_LOAD_CFG_B"); got != "beta" {
		t.Errorf("TEST_LOAD_CFG_B = %q, want %q", got, "beta")
	}
}

func TestLoadConfig_SetsHandlerFromFilename(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "my_custom_handler.toml")
	if err := os.WriteFile(configFile, []byte("env = []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONFIG_FILE", configFile)

	loadConfig()

	if got := os.Getenv("FORWARDER_HANDLER"); got != "my_custom_handler" {
		t.Errorf("FORWARDER_HANDLER = %q, want %q", got, "my_custom_handler")
	}
}

func TestLoadConfig_NoFileIsNotAnError(t *testing.T) {
	t.Setenv("CONFIG_FILE", "")
	_ = os.Unsetenv("CONFIG_FILE")

	// loadConfig should not panic or fatal when no config file exists
	loadConfig()
}

func TestLoadConfig_EnvValueWithEquals(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "test.toml")
	configContent := `env = ["TEST_LOAD_CFG_EQ=a=b=c"]` + "\n"
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONFIG_FILE", configFile)
	t.Cleanup(func() { _ = os.Unsetenv("TEST_LOAD_CFG_EQ") })

	loadConfig()

	if got := os.Getenv("TEST_LOAD_CFG_EQ"); got != "a=b=c" {
		t.Errorf("TEST_LOAD_CFG_EQ = %q, want %q (value with = signs)", got, "a=b=c")
	}
}

func writeUnrelatedCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	caFile := filepath.Join(t.TempDir(), "wrong-ca.pem")
	caBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(caFile, caBytes, 0644); err != nil {
		t.Fatalf("failed to write CA file: %v", err)
	}
	return caFile
}

func writeTLSServerCA(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	caBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ts.TLS.Certificates[0].Certificate[0],
	})
	if err := os.WriteFile(caFile, caBytes, 0644); err != nil {
		t.Fatalf("failed to write CA file: %v", err)
	}
	return caFile
}
