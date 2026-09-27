package feed

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrMalformedFrame = errors.New("malformed feed frame")

// l2MessageKindBatch / l2MessageKindSignedTx live in nitro_encode.go (same package).

type Observation struct {
	SequenceNumber uint64
	TxIndex        uint32
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
	L2Msg  string         `json:"l2Msg"`
}

type messageHeader struct {
	Timestamp *uint64 `json:"timestamp"`
}

// DecodeObservations extracts race keys across Nitro Text feeds.
//
// Official and gateway envelopes may carry either:
//   - one messages[] entry per tx (same sequenceNumber), or
//   - one messages[] entry whose l2Msg is a Batch (kind=3) of SignedTx (kind=4)
//
// Every eth tx becomes its own Observation with TxIndex 0..N-1 per sequence
// so binary feeds (RHF2/RBH1/DIRECT) can match on (seq, tx_index).
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
	nextTx := make(map[uint64]uint32, len(value.Messages))
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
		seq := *message.SequenceNumber
		ts := time.Unix(int64(timestamp), 0) // #nosec G115 -- bounded above.
		txCount := countL2SignedTxs(message.Message.Message.L2Msg)
		if txCount < 1 {
			txCount = 1
		}
		for i := 0; i < txCount; i++ {
			txIndex := nextTx[seq]
			nextTx[seq] = txIndex + 1
			observations = append(observations, Observation{
				SequenceNumber: seq,
				TxIndex:        txIndex,
				BlockHash:      blockHash,
				FeedTimestamp:  ts,
			})
		}
	}
	return observations, nil
}

// countL2SignedTxs returns how many SignedTx children are in a base64 l2Msg.
// Unknown / empty payloads return 0 (caller treats as one logical message).
func countL2SignedTxs(l2b64 string) int {
	l2b64 = strings.TrimSpace(l2b64)
	if l2b64 == "" {
		return 0
	}
	raw, err := base64.StdEncoding.DecodeString(l2b64)
	if err != nil || len(raw) == 0 {
		return 0
	}
	switch raw[0] {
	case l2MessageKindSignedTx:
		if _, ok := ethTxWireLen(raw[1:]); ok {
			return 1
		}
		return 1 // structurally a SignedTx even if RLP peek fails
	case l2MessageKindBatch:
		return countBatchSignedTxs(raw[1:])
	default:
		return 0
	}
}

// countBatchSignedTxs counts SignedTx children inside an L2 Batch body (after kind=3).
//
// Supports both layouts seen in the wild:
//  1. Gateway / Arbitrum Nitro: u64 BE length-prefix per child
//     `u64_be(len) || (4 || eth_tx) || …`
//  2. Concatenated (legacy tests / some relays): `(4 || eth_tx) || …`
func countBatchSignedTxs(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	// Prefer length-prefixed form when the first 8 bytes look like a length and
	// the byte at that offset is SignedTx (kind=4). Gateway always encodes this way.
	if len(body) >= 9 {
		itemLen := int(uint64(body[0])<<56 | uint64(body[1])<<48 | uint64(body[2])<<40 |
			uint64(body[3])<<32 | uint64(body[4])<<24 | uint64(body[5])<<16 |
			uint64(body[6])<<8 | uint64(body[7]))
		if itemLen >= 1 && itemLen <= len(body)-8 && body[8] == l2MessageKindSignedTx {
			return countBatchSignedTxsLenPrefixed(body)
		}
	}
	return countBatchSignedTxsConcat(body)
}

func countBatchSignedTxsLenPrefixed(body []byte) int {
	count := 0
	off := 0
	for off+8 <= len(body) {
		itemLen := int(uint64(body[off])<<56 | uint64(body[off+1])<<48 | uint64(body[off+2])<<40 |
			uint64(body[off+3])<<32 | uint64(body[off+4])<<24 | uint64(body[off+5])<<16 |
			uint64(body[off+6])<<8 | uint64(body[off+7]))
		off += 8
		if itemLen < 1 || off+itemLen > len(body) {
			break
		}
		child := body[off : off+itemLen]
		off += itemLen
		if child[0] != l2MessageKindSignedTx {
			break
		}
		count++
	}
	return count
}

func countBatchSignedTxsConcat(body []byte) int {
	count := 0
	off := 0
	for off < len(body) {
		if body[off] != l2MessageKindSignedTx {
			break
		}
		off++
		n, ok := ethTxWireLen(body[off:])
		if !ok || n <= 0 {
			break
		}
		off += n
		count++
	}
	return count
}

func validHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}
