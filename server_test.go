package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/redhatinsights/yggdrasil/protocol"
)

func TestDispatch(t *testing.T) {
	input := &pb.Data{}

	got := jsonData(input)
	want := "{\"response_to\":\"\",\"metadata\":null,\"content\":null,\"directive\":\"\"}"

	if string(got) != want {
		t.Fatalf(`Got: %q, Wanted: %q`, string(got), want)
	}
}

func TestBuildHTTPClient_NoCAFile(t *testing.T) {
	t.Setenv("FORWARDER_CA_FILE", "")
	os.Unsetenv("FORWARDER_CA_FILE")
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
	defer resp.Body.Close()

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

	receipt, err := server.Send(nil, data)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected non-nil receipt")
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
