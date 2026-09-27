package feed

import (
	"context"
	"encoding/base64"
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

func TestDecodeObservationsBatch(t *testing.T) {
	hash := "0x" + fmt.Sprintf("%064x", 42)
	// Concat layout (legacy): Batch(kind=3) + two SignedTx typed stubs.
	l2Concat := []byte{3, 4, 0x02, 0x80, 4, 0x02, 0x80}
	// Gateway / Nitro layout: u64 BE length-prefix per child.
	l2LenPref := []byte{
		3,
		0, 0, 0, 0, 0, 0, 0, 3, 4, 0x02, 0x80,
		0, 0, 0, 0, 0, 0, 0, 3, 4, 0x02, 0x80,
	}
	for _, l2 := range [][]byte{l2Concat, l2LenPref} {
		payload := []byte(fmt.Sprintf(
			`{"version":1,"messages":[{"sequenceNumber":99,"blockHash":%q,"message":{"message":{"header":{"timestamp":1700000000},"l2Msg":%q}}}]}`,
			hash, base64.StdEncoding.EncodeToString(l2)))
		observations, err := DecodeObservations(payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(observations) != 2 {
			t.Fatalf("want 2 txs, got %+v (l2=%x)", observations, l2)
		}
		if observations[0].TxIndex != 0 || observations[1].TxIndex != 1 {
			t.Fatalf("tx indexes: %+v", observations)
		}
		if observations[0].SequenceNumber != 99 {
			t.Fatalf("seq: %+v", observations)
		}
	}
}

func TestDecodeObservationsMultiMessageSameSeq(t *testing.T) {
	hash := "0x" + fmt.Sprintf("%064x", 7)
	l2a := base64.StdEncoding.EncodeToString([]byte{4, 0x02, 0x80})
	l2b := base64.StdEncoding.EncodeToString([]byte{4, 0x02, 0x80})
	payload := []byte(fmt.Sprintf(
		`{"version":1,"messages":[{"sequenceNumber":5,"blockHash":%q,"message":{"message":{"header":{"timestamp":1},"l2Msg":%q}}},{"sequenceNumber":5,"blockHash":%q,"message":{"message":{"header":{"timestamp":1},"l2Msg":%q}}}]}`,
		hash, l2a, hash, l2b))
	observations, err := DecodeObservations(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[0].TxIndex != 0 || observations[1].TxIndex != 1 {
		t.Fatalf("got %+v", observations)
	}
}
