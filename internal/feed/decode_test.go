package feed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDecodeObservations(t *testing.T) {
	hash := "0x" + fmt.Sprintf("%064x", 42)
	payload := []byte(fmt.Sprintf(`{"version":1,"messages":[{"sequenceNumber":7,"blockHash":%q,"message":{"message":{"header":{"timestamp":1700000000}}}}]}`, hash))
	observations, err := DecodeObservations(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].SequenceNumber != 7 || observations[0].BlockHash != hash {
		t.Fatalf("unexpected observations: %+v", observations)
	}
	if got := observations[0].FeedTimestamp.Unix(); got != 1700000000 {
		t.Fatalf("timestamp = %d", got)
	}
}

func TestRunReceivesObservation(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	hash := "0x" + fmt.Sprintf("%064x", 42)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(clientVersionHeader) != "2" {
			t.Errorf("client version header = %q", request.Header.Get(clientVersionHeader))
		}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		payload := fmt.Sprintf(`{"version":1,"messages":[{"sequenceNumber":7,"blockHash":%q,"message":{"message":{"header":{"timestamp":%d}}}}]}`, hash, time.Now().Unix())
		_ = connection.WriteMessage(websocket.TextMessage, []byte(payload))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	updates := make(chan Update, 16)
	go Run(ctx, Endpoint{Name: "local", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, 5*time.Second, updates)
	for {
		select {
		case update := <-updates:
			if update.Kind == UpdateObservation {
				if update.Sequence != 7 || update.BlockHash != hash {
					t.Fatalf("unexpected update: %+v", update)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for observation")
		}
	}
}

func TestDecodeObservationsRejectsMalformedFrames(t *testing.T) {
	tests := [][]byte{
		[]byte(`{"version":2,"messages":[]}`),
		[]byte(`{"version":1,"messages":[{}]}`),
		[]byte(`{"version":1,"messages":[{"sequenceNumber":1,"blockHash":"0x01","message":{"message":{"header":{"timestamp":1}}}}]}`),
		[]byte(`{"version":1,"messages":[]} {}`),
	}
	for _, payload := range tests {
		if _, err := DecodeObservations(payload); !errors.Is(err, ErrMalformedFrame) {
			t.Fatalf("error = %v, want ErrMalformedFrame", err)
		}
	}
}

func TestValidateEndpoint(t *testing.T) {
	valid := []string{
		"wss://feed.example.com",
		"ws://localhost:8080",
		"ws://127.0.0.1:8080",
		"ws://[::1]:8080",
		"ws://192.0.2.10:9642/feed",
	}
	for _, address := range valid {
		if err := ValidateEndpoint(Endpoint{Name: "test", URL: address}); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
	invalid := []string{"http://feed.example.com", "wss://user:pass@feed.example.com", "not-a-url"}
	for _, address := range invalid {
		if err := ValidateEndpoint(Endpoint{Name: "test", URL: address}); err == nil {
			t.Errorf("%s unexpectedly accepted", address)
		}
	}
}
