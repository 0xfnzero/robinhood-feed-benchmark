package feed

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	nitroEnvelopeVersion     = 1
	l2MessageKindSignedTx    = 4
	l1MessageTypeL2Message   = 3
	nitroSequencerSender     = "0xa4b000000000000000000073657175656e636572"
)

// EncodeNitroEnvelope builds an official-style Nitro Feed JSON frame from one
// or more raw Ethereum transactions belonging to the same sequence.
//
// RHF2 streams do not carry a chain-canonical blockHash; a deterministic
// synthetic hash is filled so the envelope stays structurally valid.
func EncodeNitroEnvelope(sequence uint64, timestamp uint64, txs [][]byte) ([]byte, error) {
	if len(txs) == 0 {
		return nil, fmt.Errorf("nitro encode: empty tx set for seq %d", sequence)
	}
	blockHash := syntheticBlockHash(sequence, timestamp, txs)
	messages := make([]map[string]any, 0, len(txs))
	for _, tx := range txs {
		if len(tx) == 0 {
			continue
		}
		l2 := make([]byte, 1+len(tx))
		l2[0] = l2MessageKindSignedTx
		copy(l2[1:], tx)
		messages = append(messages, map[string]any{
			"sequenceNumber": sequence,
			"blockHash":      blockHash,
			"message": map[string]any{
				"message": map[string]any{
					"header": map[string]any{
						"kind":        l1MessageTypeL2Message,
						"sender":      nitroSequencerSender,
						"blockNumber": sequence,
						"timestamp":   timestamp,
						"requestId":   nil,
						"baseFeeL1":   nil,
					},
					"l2Msg": base64.StdEncoding.EncodeToString(l2),
				},
				"delayedMessagesRead": 0,
			},
		})
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("nitro encode: no usable txs for seq %d", sequence)
	}
	envelope := map[string]any{
		"version":  nitroEnvelopeVersion,
		"messages": messages,
	}
	return json.Marshal(envelope)
}

func syntheticBlockHash(sequence, timestamp uint64, txs [][]byte) string {
	h := sha256.New()
	var hdr [16]byte
	hdr[0] = byte(sequence >> 56)
	hdr[1] = byte(sequence >> 48)
	hdr[2] = byte(sequence >> 40)
	hdr[3] = byte(sequence >> 32)
	hdr[4] = byte(sequence >> 24)
	hdr[5] = byte(sequence >> 16)
	hdr[6] = byte(sequence >> 8)
	hdr[7] = byte(sequence)
	hdr[8] = byte(timestamp >> 56)
	hdr[9] = byte(timestamp >> 48)
	hdr[10] = byte(timestamp >> 40)
	hdr[11] = byte(timestamp >> 32)
	hdr[12] = byte(timestamp >> 24)
	hdr[13] = byte(timestamp >> 16)
	hdr[14] = byte(timestamp >> 8)
	hdr[15] = byte(timestamp)
	_, _ = h.Write(hdr[:])
	for _, tx := range txs {
		_, _ = h.Write(tx)
	}
	sum := h.Sum(nil)
	return "0x" + hex.EncodeToString(sum)
}
