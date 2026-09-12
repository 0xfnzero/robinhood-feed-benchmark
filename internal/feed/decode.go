package feed

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrMalformedFrame = errors.New("malformed feed frame")

type Observation struct {
	SequenceNumber uint64
	BlockHash      string
	FeedTimestamp  time.Time
}

type envelope struct {
	Version  *uint64       `json:"version"`
	Messages []wireMessage `json:"messages"`
}

type wireMessage struct {
	SequenceNumber *uint64         `json:"sequenceNumber"`
	Message        *messageWrapper `json:"message"`
	BlockHash      string          `json:"blockHash"`
}

type messageWrapper struct {
	Message *incomingMessage `json:"message"`
}

type incomingMessage struct {
	Header *messageHeader `json:"header"`
}

type messageHeader struct {
	Timestamp *uint64 `json:"timestamp"`
}

// DecodeObservations extracts stable identifiers used to compare the same
// sequencer message across feeds. It intentionally avoids transaction decoding
// so receive timestamps stay as close as possible to the socket read.
func DecodeObservations(data []byte) ([]Observation, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value envelope
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: decode JSON: %v", ErrMalformedFrame, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%w: multiple JSON values", ErrMalformedFrame)
		}
		return nil, fmt.Errorf("%w: trailing JSON: %v", ErrMalformedFrame, err)
	}
	if value.Version == nil || *value.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported or missing version", ErrMalformedFrame)
	}

	observations := make([]Observation, 0, len(value.Messages))
	for index, message := range value.Messages {
		if message.SequenceNumber == nil || message.Message == nil ||
			message.Message.Message == nil || message.Message.Message.Header == nil ||
			message.Message.Message.Header.Timestamp == nil {
			return nil, fmt.Errorf("%w: message %d is missing required fields", ErrMalformedFrame, index)
		}
		blockHash := strings.ToLower(strings.TrimSpace(message.BlockHash))
		if !validHash(blockHash) {
			return nil, fmt.Errorf("%w: message %d has invalid block hash", ErrMalformedFrame, index)
		}
		timestamp := *message.Message.Message.Header.Timestamp
		if timestamp > uint64(^uint64(0)>>1) {
			return nil, fmt.Errorf("%w: message %d has invalid timestamp", ErrMalformedFrame, index)
		}
		observations = append(observations, Observation{
			SequenceNumber: *message.SequenceNumber,
			BlockHash:      blockHash,
			FeedTimestamp:  time.Unix(int64(timestamp), 0), // #nosec G115 -- bounded above.
		})
	}
	return observations, nil
}

func validHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}
