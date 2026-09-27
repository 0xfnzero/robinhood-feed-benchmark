package feed

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	rbh1Magic      = "RBH1"
	rbh1HeaderSize = 16
	rbh1Version    = 1

	rbh1FlagFinal   = 1 << 0
	rbh1FlagEventNS = 1 << 1
	rbh1FlagFrag    = 1 << 2

	rbh1KindEthTx = 1
	rbh1KindNitro = 2
)

// RBH1Frame is one RBH1 wire unit (complete eth_tx / nitro body, or a UDP frag chunk).
type RBH1Frame struct {
	Flags     uint8
	Kind      uint8
	TxIndex   uint8
	Seq       uint32
	EventNS   uint64 // 0 if FLAG_EVENT_NS absent
	HasEvent  bool
	Payload   []byte
	Frag      *RBH1Frag
	Size      int
}

type RBH1Frag struct {
	TotalLen uint32
	FragIdx  uint16
	FragCnt  uint16
}

var (
	ErrRBH1Incomplete = errors.New("rbh1: incomplete frame")
)

func rbh1ExtLen(flags uint8) int {
	n := 0
	if flags&rbh1FlagEventNS != 0 {
		n += 8
	}
	if flags&rbh1FlagFrag != 0 {
		n += 8
	}
	return n
}

// DecodeRBH1Frame parses one RBH1 unit starting at data[0].
func DecodeRBH1Frame(data []byte) (RBH1Frame, int, error) {
	if len(data) < rbh1HeaderSize {
		return RBH1Frame{}, 0, ErrRBH1Incomplete
	}
	if string(data[:4]) != rbh1Magic {
		return RBH1Frame{}, 0, fmt.Errorf("rbh1: bad magic %q", data[:4])
	}
	version := data[4]
	if version != rbh1Version {
		return RBH1Frame{}, 0, fmt.Errorf("rbh1: unsupported version %d", version)
	}
	flags := data[5]
	kind := data[6]
	if kind != rbh1KindEthTx && kind != rbh1KindNitro {
		return RBH1Frame{}, 0, fmt.Errorf("rbh1: bad kind %d", kind)
	}
	txIndex := data[7]
	seq := binary.LittleEndian.Uint32(data[8:12])
	payloadLen := binary.LittleEndian.Uint32(data[12:16])
	if payloadLen > maxMessageBytes {
		return RBH1Frame{}, 0, fmt.Errorf("rbh1: invalid payload length %d", payloadLen)
	}
	exts := rbh1ExtLen(flags)
	total := rbh1HeaderSize + exts + int(payloadLen)
	if len(data) < total {
		return RBH1Frame{}, 0, ErrRBH1Incomplete
	}
	off := rbh1HeaderSize
	var eventNS uint64
	hasEvent := false
	if flags&rbh1FlagEventNS != 0 {
		eventNS = binary.LittleEndian.Uint64(data[off : off+8])
		hasEvent = true
		off += 8
	}
	var frag *RBH1Frag
	if flags&rbh1FlagFrag != 0 {
		totalLen := binary.LittleEndian.Uint32(data[off : off+4])
		fragIdx := binary.LittleEndian.Uint16(data[off+4 : off+6])
		fragCnt := binary.LittleEndian.Uint16(data[off+6 : off+8])
		if fragCnt == 0 || fragIdx >= fragCnt {
			return RBH1Frame{}, 0, errors.New("rbh1: bad frag")
		}
		frag = &RBH1Frag{TotalLen: totalLen, FragIdx: fragIdx, FragCnt: fragCnt}
		off += 8
	}
	payload := append([]byte(nil), data[off:off+int(payloadLen)]...)
	return RBH1Frame{
		Flags:    flags,
		Kind:     kind,
		TxIndex:  txIndex,
		Seq:      seq,
		EventNS:  eventNS,
		HasEvent: hasEvent,
		Payload:  payload,
		Frag:     frag,
		Size:     total,
	}, total, nil
}

// ObservationFromRBH1 builds a race observation for one eth_tx unit (seq, tx_index).
// Matching is by (seq, tx_index); RBH1 has no Nitro blockHash.
func ObservationFromRBH1(frame RBH1Frame) Observation {
	var ts time.Time
	if frame.HasEvent && frame.EventNS > 0 {
		ts = time.Unix(0, int64(frame.EventNS)) // #nosec G115 -- feed event_ns is wall-clock ns
	} else {
		ts = time.Now()
	}
	return Observation{
		SequenceNumber: uint64(frame.Seq),
		TxIndex:        uint32(frame.TxIndex),
		BlockHash:      "",
		FeedTimestamp:  ts,
	}
}

// emitRBH1Frame publishes one complete RBH1 unit into the race channel.
// KIND_ETH_TX → one (seq, tx_index) event; KIND_NITRO → expand Batch/messages.
// Returns false if the consumer context is cancelled.
func emitRBH1Frame(
	ctx context.Context,
	name string,
	frame RBH1Frame,
	receivedAt time.Time,
	maxAge time.Duration,
	frameBytes int,
	output chan<- Update,
) bool {
	if frame.Kind == rbh1KindNitro {
		observations, err := DecodeObservations(frame.Payload)
		if err != nil {
			return true
		}
		for i, observation := range observations {
			age := receivedAt.Sub(observation.FeedTimestamp)
			if maxAge > 0 && (age < -maxAge || age > maxAge) {
				continue
			}
			bytes := 0
			if i == 0 {
				bytes = frameBytes
			}
			if !send(ctx, output, Update{
				Kind: UpdateObservation, Endpoint: name, At: receivedAt,
				Sequence: observation.SequenceNumber, TxIndex: observation.TxIndex,
				BlockHash: observation.BlockHash,
				FeedTimestamp: observation.FeedTimestamp, FrameBytes: bytes,
			}) {
				return false
			}
		}
		return true
	}
	if frame.Kind != rbh1KindEthTx {
		return true
	}
	observation := ObservationFromRBH1(frame)
	age := receivedAt.Sub(observation.FeedTimestamp)
	if maxAge > 0 && (age < -maxAge || age > maxAge) {
		return true
	}
	return send(ctx, output, Update{
		Kind: UpdateObservation, Endpoint: name, At: receivedAt,
		Sequence: observation.SequenceNumber, TxIndex: observation.TxIndex,
		BlockHash: observation.BlockHash,
		FeedTimestamp: observation.FeedTimestamp, FrameBytes: frameBytes,
	})
}

// IsRBH1Wire reports whether payload looks like a complete RBH1 frame prefix.
func IsRBH1Wire(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == rbh1Magic
}
