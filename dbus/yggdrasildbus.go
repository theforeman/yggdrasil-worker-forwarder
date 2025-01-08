package main

import (
	"bytes"
	b64 "encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"git.sr.ht/~spc/go-log"
	"github.com/pelletier/go-toml"
	"github.com/redhatinsights/yggdrasil/worker"
)

type Message struct {
	MessageID  string            `json:"message_id"`
	ResponseTo string            `json:"response_to"`
	Directive  string            `json:"directive"`
	Metadata   map[string]string `json:"metadata"`
	Content    string            `json:"content"`
}

func postToForwarder(dataJson []byte) {
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

	fmt.Println("11111000000 sending", string(dataJson))

	// Call http post
	request, _ := http.NewRequest("POST", postUrl, bytes.NewBuffer(dataJson))
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(postUser, postPassword)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()

	log.Tracef("response Status: %v", response.Status)
	log.Tracef("response Headers: %+v", response.Header)
	body, _ := io.ReadAll(response.Body)
	log.Tracef("response Body: %v", string(body))
}

func setupToml(configFile string) error {
	config, err := toml.LoadFile(configFile)
	if err != nil {
		log.Fatal(fmt.Errorf("cannot load config: %w", err))
		return err
	}
	for _, value := range config.GetArray("env").([]string) {
		split := strings.Split(value, "=")
		err = os.Setenv(split[0], split[1])
		if err != nil {
			return err
		}
	}
	err = os.Setenv("FORWARDER_HANDLER", strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile)))
	if err != nil {
		return err
	}
	return nil
}

// forward handles the forward message and sets the channel that will manage the
// cancel message. It runs a loop and a sleep according to the loop and
// sleep parameters, then calls the forward function to transmit the
// message. If there is a cancellation message during the loop or the sleep
// time, it will cancel the transmission of the message and finish the work.
func forward(
	w *worker.Worker,
	addr string,
	rcvId string,
	responseTo string,
	metadata map[string]string,
	data []byte,
) error {

	msg := Message{
		Directive:  addr,
		MessageID:  rcvId,
		ResponseTo: responseTo,
		Metadata:   metadata,
		Content:    b64.URLEncoding.EncodeToString(data),
	}

	dataJson, error := json.Marshal(msg)
	if error != nil {
		log.Fatal(error)
	}

	postToForwarder(dataJson)
	return nil
}

func main() {
	var (
		logLevel string
	)

	const DefaultConfigFile = "/etc/rhc/workers/foreman_rh_cloud.toml"
	const DirectiveName = "foreman_rh_cloud"

	var err error
	configFile, ok := os.LookupEnv("CONFIG_FILE")
	if ok {
		err = setupToml(configFile)
	} else {
		err = setupToml(DefaultConfigFile)
	}

	if err != nil {
		log.Fatalf("error: cannot parse file: %v", err)
	}

	flag.StringVar(&logLevel, "log-level", "debug", "set log level")

	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("error: cannot parse log level: %v", err)
	}
	log.SetLevel(level)

	w, err := worker.NewWorker(
		DirectiveName,
		false,
		nil,
		nil,
		forward,
		nil,
	)
	if err != nil {
		log.Fatalf("error: cannot create worker: %v", err)
	}

	// Set up a channel to receive the TERM or INT signal over and clean up
	// before quitting.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	if err := w.Connect(quit); err != nil {
		log.Fatalf("error: cannot connect: %v", err)
	}
}
