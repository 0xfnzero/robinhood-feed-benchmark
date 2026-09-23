package feed

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// DIRECT binary protocol. All multi-byte fields are little-endian
// unless noted otherwise.

const (
	directOuterHeaderSize = 20
	directPrefixSize      = 24
	directFlag            = 0x10000000
	directLengthMask      = 0x0fffffff
	directMaxPayload      = 4 << 20
	directMaxL2Item       = 256 << 10

	directRecordItem  = 1
	directRecordFinal = 3
)

// DirectFrame is one decoded DIRECT payload (ITEM or SEQUENCE_FINAL).
type DirectFrame struct {
	RelayEventNS uint64
	RelaySendNS  uint64
	Version      uint8
	RecordType   uint8
	TypeField    uint8 // ITEM: must be 0; FINAL: reconstruction kind
	Sequence     uint64
	Ordinal      uint32 // ITEM ordinal, or FINAL item_count
	First        []byte
	Second       []byte
	OuterSize    int // 20 + payload_len
}

var (
	ErrDirectIncomplete = errors.New("direct: incomplete frame")
	ErrDirectNotDirect  = errors.New("direct: missing DIRECT flag or invalid length")
)

// DecodeDirectOuter parses the 20-byte outer header.
func DecodeDirectOuter(header []byte) (payloadLen int, eventNS, sendNS uint64, err error) {
	if len(header) < directOuterHeaderSize {
		return 0, 0, 0, ErrDirectIncomplete
	}
	laf := binary.LittleEndian.Uint32(header[0:4])
	if laf&directFlag == 0 {
		return 0, 0, 0, ErrDirectNotDirect
	}
	payloadLen = int(laf & directLengthMask)
	if payloadLen < directPrefixSize || payloadLen > directMaxPayload {
		return 0, 0, 0, fmt.Errorf("direct: invalid payload_len %d", payloadLen)
	}
	eventNS = binary.LittleEndian.Uint64(header[4:12])
	sendNS = binary.LittleEndian.Uint64(header[12:20])
	return payloadLen, eventNS, sendNS, nil
}

// DecodeDirectPayload parses the DIRECT prefix + fields.
func DecodeDirectPayload(payload []byte, eventNS, sendNS uint64) (DirectFrame, error) {
	if len(payload) < directPrefixSize {
		return DirectFrame{}, ErrDirectIncomplete
	}
	version := payload[0]
	recordType := payload[1]
	typeField := payload[2]
	reserved := payload[3]
	if version != 1 {
		return DirectFrame{}, fmt.Errorf("direct: unsupported version %d", version)
	}
	if reserved != 0 {
		return DirectFrame{}, fmt.Errorf("direct: reserved must be 0 (got %d)", reserved)
	}
	seq := binary.LittleEndian.Uint64(payload[4:12])
	ordinal := binary.LittleEndian.Uint32(payload[12:16])
	firstLen := binary.LittleEndian.Uint32(payload[16:20])
	secondLen := binary.LittleEndian.Uint32(payload[20:24])
	need := directPrefixSize + int(firstLen) + int(secondLen)
	if len(payload) != need {
		return DirectFrame{}, fmt.Errorf("direct: payload_len mismatch got=%d want=%d", len(payload), need)
	}
	first := append([]byte(nil), payload[directPrefixSize:directPrefixSize+int(firstLen)]...)
	second := append([]byte(nil), payload[directPrefixSize+int(firstLen):need]...)

	switch recordType {
	case directRecordItem:
		if typeField != 0 {
			return DirectFrame{}, fmt.Errorf("direct: ITEM type field must be 0 (got %d)", typeField)
		}
		if secondLen > directMaxL2Item {
			return DirectFrame{}, fmt.Errorf("direct: L2 item too large (%d)", secondLen)
		}
	case directRecordFinal:
		// typeField = reconstruction kind; ordinal = item count
	default:
		return DirectFrame{}, fmt.Errorf("direct: unknown record type %d", recordType)
	}

	return DirectFrame{
		RelayEventNS: eventNS,
		RelaySendNS:  sendNS,
		Version:      version,
		RecordType:   recordType,
		TypeField:    typeField,
		Sequence:     seq,
		Ordinal:      ordinal,
		First:        first,
		Second:       second,
		OuterSize:    directOuterHeaderSize + len(payload),
	}, nil
}

// ObservationFromDirectITEM builds a race observation from the first-seen ITEM
// for a sequence (first-arrival race path).
func ObservationFromDirectITEM(frame DirectFrame, receivedAt time.Time) Observation {
	feedTS := receivedAt
	if frame.RelayEventNS > 0 {
		feedTS = time.Unix(0, int64(frame.RelayEventNS)) // #nosec G115
	}
	return Observation{
		SequenceNumber: frame.Sequence,
		BlockHash:      "", // DIRECT has no Nitro blockHash; match by sequence
		FeedTimestamp:  feedTS,
	}
}
