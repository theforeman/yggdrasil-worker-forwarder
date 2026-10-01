package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"git.sr.ht/~spc/go-log"
	"github.com/redhatinsights/yggdrasil/worker"
	pb "github.com/redhatinsights/yggdrasil_v0/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// forwarderServer forwards messages received from yggdrasil to the Foreman
// cloud_request API as an HTTP POST. It serves both transports: it implements
// the Worker gRPC service as defined by the yggdrasil 0.2.z gRPC protocol, and
// its forward method satisfies worker.RxFunc for the yggdrasil 0.4.z D-Bus
// protocol. Both paths produce an identical request body.
type forwarderServer struct {
	pb.UnimplementedWorkerServer
	Url        string
	Username   string
	Password   string
	HTTPClient *http.Client
}

type httpMessage struct {
	ResponseTo string            `json:"response_to"`
	Metadata   map[string]string `json:"metadata"`
	Content    []byte            `json:"content"`
	Directive  string            `json:"directive"`
}

// marshalMessage renders the POST body sent to the Foreman cloud_request API.
//
// Content is deliberately a []byte: encoding/json emits it as standard-alphabet
// base64, which is what the Foreman side expects. The receiving controller
// decodes it with Ruby's Base64.decode64, which implements the standard
// alphabet and *silently discards* characters outside it, so a URL-safe
// encoding would corrupt any payload containing '-' or '_'.
func marshalMessage(responseTo string, directive string, metadata map[string]string, content []byte) []byte {
	data := httpMessage{
		ResponseTo: responseTo,
		Metadata:   metadata,
		Content:    content,
		Directive:  directive,
	}

	dataJson, err := json.Marshal(data)
	if err != nil {
		log.Errorf("failed to marshal message data to JSON: %v", err)
		return nil
	}

	return dataJson
}

// post sends an already marshalled message to the Foreman cloud_request API.
// Failures are logged and swallowed: a transient HTTP or TLS error must not
// take the worker down, since yggdrasil would then stop delivering messages
// entirely until the service is restarted.
func (s *forwarderServer) post(dataJson []byte) {
	if dataJson == nil {
		return
	}

	log.Infof("sending %v", string(dataJson))

	request, err := http.NewRequest("POST", s.Url, bytes.NewBuffer(dataJson))
	if err != nil {
		log.Errorf("failed to build HTTP request for %s: %v", s.Url, err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(s.Username, s.Password)

	response, err := s.HTTPClient.Do(request)
	if err != nil {
		log.Errorf("failed to send HTTP POST to %s: %v", s.Url, err)
		return
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Errorf("failed to close response body: %v", err)
		}
	}()

	log.Tracef("response Status: %v", response.Status)
	log.Tracef("response Headers: %+v", response.Header)
	body, _ := io.ReadAll(response.Body)
	log.Tracef("response Body: %v", string(body))
}

// Send implements the "Send" method of the Worker gRPC service.
func (s *forwarderServer) Send(ctx context.Context, d *pb.Data) (*pb.Receipt, error) {
	go func() {
		log.Tracef("received data: %#v", d)

		// Dial the Dispatcher and call "Finish"
		conn, err := grpc.NewClient(yggdDispatchSocketAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Errorf("failed to connect to dispatcher at %s: %v", yggdDispatchSocketAddr, err)
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				log.Errorf("failed to close connection: %v", err)
			}
		}()

		s.post(jsonData(d))
	}()

	// Respond to the start request that the work was accepted.
	return &pb.Receipt{}, nil
}

// forward implements worker.RxFunc, the D-Bus counterpart of Send.
//
// responseTo is taken from the incoming message id rather than the rx
// responseTo argument, so that the body is byte-identical to the one the gRPC
// path builds from pb.Data.GetMessageId().
func (s *forwarderServer) forward(
	w *worker.Worker,
	addr string,
	rcvId string,
	responseTo string,
	metadata map[string]string,
	data []byte,
) error {
	log.Tracef("received data: addr=%v id=%v responseTo=%v metadata=%v", addr, rcvId, responseTo, metadata)

	s.post(marshalMessage(rcvId, addr, metadata, data))
	return nil
}

func jsonData(d *pb.Data) []byte {
	return marshalMessage(d.GetMessageId(), d.GetDirective(), d.GetMetadata(), d.GetContent())
}
