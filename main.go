package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"git.sr.ht/~spc/go-log"
	"github.com/pelletier/go-toml"
	"github.com/redhatinsights/yggdrasil/worker"
	pb "github.com/redhatinsights/yggdrasil_v0/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// defaultHandler is the directive this worker registers as when the
// configuration does not name one.
const defaultHandler = "foreman_rh_cloud"

var yggdDispatchSocketAddr string
var yggdHandler string

func main() {
	var logLevel string
	flag.StringVar(&logLevel, "log-level", "", "set log level (error, warn, info, debug, trace)")
	flag.Parse()

	setupLogging(logLevel)
	loadConfig()

	yggdHandler = os.Getenv("FORWARDER_HANDLER")
	if yggdHandler == "" {
		log.Infof("FORWARDER_HANDLER not set, defaulting to %v", defaultHandler)
		yggdHandler = defaultHandler
	}

	postUrl, ok := os.LookupEnv("FORWARDER_URL")
	if !ok {
		log.Fatal("Missing FORWARDER_URL environment variable")
	}

	postUser, ok := os.LookupEnv("FORWARDER_USER")
	if !ok {
		log.Fatal("Missing FORWARDER_USER environment variable")
	}

	postPassword, ok := os.LookupEnv("FORWARDER_PASSWORD")
	if !ok {
		log.Fatal("Missing FORWARDER_PASSWORD environment variable")
	}

	fs := &forwarderServer{
		Url:        postUrl,
		Username:   postUser,
		Password:   postPassword,
		HTTPClient: buildHTTPClient(),
	}

	// yggdrasil 0.2.z execs its workers with YGG_SOCKET_ADDR pointing at the
	// dispatcher's gRPC socket. yggdrasil 0.4.z never sets it, because workers
	// are standalone D-Bus services it does not start itself, so the presence
	// of the variable is what selects the transport.
	yggdDispatchSocketAddr, ok = os.LookupEnv("YGG_SOCKET_ADDR")
	if ok {
		log.Info("YGG_SOCKET_ADDR environment variable found; attempting gRPC connection")
		serveGRPC(fs)
	} else {
		log.Info("YGG_SOCKET_ADDR environment variable not found; attempting D-Bus connection")
		serveDBus(fs)
	}
}

// setupLogging applies the -log-level flag, falling back to YGG_LOG_LEVEL and
// then to info.
func setupLogging(logLevel string) {
	if logLevel == "" {
		logLevel = os.Getenv("YGG_LOG_LEVEL")
	}
	if logLevel == "" {
		log.SetLevel(log.LevelInfo)
		return
	}

	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Errorf("cannot parse log level %q: %v", logLevel, err)
		log.SetLevel(log.LevelInfo)
		return
	}
	log.SetLevel(level)
}

// loadConfig reads the worker's TOML configuration named by CONFIG_FILE and
// exports its "env" array into the process environment. CONFIG_FILE is set by
// the caller: yggdrasil execs gRPC workers with it, and the D-Bus systemd unit
// sets it via Environment=. CONFIG_FILE being unset is not an error, as the
// same values may be supplied directly through the environment.
func loadConfig() {
	configFile, ok := os.LookupEnv("CONFIG_FILE")
	if !ok {
		log.Debug("CONFIG_FILE not set; using the environment only")
		return
	}

	config, err := toml.LoadFile(configFile)
	if err != nil {
		log.Fatal(fmt.Errorf("cannot load config: %w", err))
	}

	envEntries, ok := config.GetArray("env").([]string)
	if !ok {
		log.Debugf("no env array in %v", configFile)
		return
	}

	for _, value := range envEntries {
		key, val, found := strings.Cut(value, "=")
		if !found {
			log.Warnf("ignoring malformed env entry in %v: %v", configFile, key)
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			log.Fatal(fmt.Errorf("cannot set env var %s: %w", key, err))
		}
	}

	if err := os.Setenv("FORWARDER_HANDLER", strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile))); err != nil {
		log.Fatal(fmt.Errorf("cannot set FORWARDER_HANDLER: %w", err))
	}
}

// serveGRPC registers with the yggdrasil 0.2.z dispatcher and serves the Worker
// gRPC service. It does not return.
func serveGRPC(fs *forwarderServer) {
	// Dial the dispatcher on its well-known address.
	conn, err := grpc.NewClient(yggdDispatchSocketAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Errorf("failed to close connection: %v", err)
		}
	}()

	// Create a dispatcher client
	c := pb.NewDispatcherClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Register as a handler as defined in the ENV variable.
	r, err := c.Register(ctx, &pb.RegistrationRequest{Handler: yggdHandler, Pid: int64(os.Getpid())})
	if err != nil {
		log.Fatal(err)
	}
	if !r.GetRegistered() {
		log.Fatalf("handler registration failed: %v", err)
	}

	// Listen on the provided socket address.
	l, err := net.Listen("unix", r.GetAddress())
	if err != nil {
		log.Fatal(err)
	}

	// Register as a Worker service with gRPC and start accepting connections.
	s := grpc.NewServer()
	pb.RegisterWorkerServer(s, fs)
	if err := s.Serve(l); err != nil {
		log.Fatal(err)
	}
}

// serveDBus claims com.redhat.Yggdrasil1.Worker1.<directive> on the system bus
// and dispatches incoming messages to fs.forward. It does not return until the
// process is signalled.
func serveDBus(fs *forwarderServer) {
	// remoteContent is false: the message content is a playbook URL that
	// Foreman resolves itself, so yggdrasil must pass it through rather than
	// fetch it. This matches the gRPC registration, which does not set
	// DetachedContent.
	w, err := worker.NewWorker(yggdHandler, false, nil, nil, fs.forward, nil)
	if err != nil {
		log.Fatalf("cannot create worker: %v", err)
	}

	// Set up a channel to receive the TERM or INT signal over and clean up
	// before quitting.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	if err := w.Connect(quit); err != nil {
		log.Fatalf("cannot connect: %v", err)
	}
}

func buildHTTPClient() *http.Client {
	caFile, ok := os.LookupEnv("FORWARDER_CA_FILE")
	if !ok {
		return &http.Client{}
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatalf("cannot read CA file %s: %v", caFile, err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		log.Fatalf("failed to parse CA certificate from %s", caFile)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		},
	}
}
