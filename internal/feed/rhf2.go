package feed

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	rhf2Magic       = "RHF2"
	rhf2HeaderSize  = 48
	rhf2TrailerSize = 16
)

// RHF2Frame is one bare-metal binary feed message (typically one transaction).
type RHF2Frame struct {
	Flags     uint16
	SeqFrom   uint32
	SeqTo     uint32 // L2 block / Nitro sequenceNumber
	TxIndex   uint8
	Timestamp uint64 // unix seconds
	Payload   []byte // raw eth tx bytes (may be type-prefixed)
	Size      int
}

var (
	ErrRHF2Incomplete = errors.New("rhf2: incomplete frame")
)

// DecodeRHF2Frame parses one length-delimited RHF2 frame starting at data[0].
// Returns the frame, bytes consumed, or an error if the buffer is incomplete/invalid.
func DecodeRHF2Frame(data []byte) (RHF2Frame, int, error) {
	if len(data) < rhf2HeaderSize {
		return RHF2Frame{}, 0, ErrRHF2Incomplete
	}
	if string(data[:4]) != rhf2Magic {
		return RHF2Frame{}, 0, fmt.Errorf("rhf2: bad magic %q", data[:4])
	}
	version := binary.BigEndian.Uint16(data[4:6])
	if version != 2 {
		return RHF2Frame{}, 0, fmt.Errorf("rhf2: unsupported version %d", version)
	}
	flags := binary.BigEndian.Uint16(data[6:8])
	seqFrom := binary.BigEndian.Uint32(data[16:20])
	seqTo := binary.BigEndian.Uint32(data[24:28])
	payloadLen := binary.BigEndian.Uint32(data[44:48])
	total := rhf2HeaderSize + int(payloadLen) + rhf2TrailerSize
	if payloadLen > maxMessageBytes || total < rhf2HeaderSize {
		return RHF2Frame{}, 0, fmt.Errorf("rhf2: invalid payload length %d", payloadLen)
	}
	if len(data) < total {
		return RHF2Frame{}, 0, ErrRHF2Incomplete
	}
	body := data[rhf2HeaderSize : rhf2HeaderSize+int(payloadLen)]
	if len(body) < 16 {
		return RHF2Frame{}, 0, errors.New("rhf2: short body")
	}
	if body[0] != 1 {
		return RHF2Frame{}, 0, fmt.Errorf("rhf2: unexpected body marker %#x", body[0])
	}
	frame := RHF2Frame{
		Flags:     flags,
		SeqFrom:   seqFrom,
		SeqTo:     seqTo,
		TxIndex:   body[3],
		Timestamp: binary.BigEndian.Uint64(body[4:12]),
		Payload:   append([]byte(nil), body[16:]...),
		Size:      total,
	}
	return frame, total, nil
}

// ObservationFromRHF2 builds a benchmark observation for a block's first transaction.
func ObservationFromRHF2(frame RHF2Frame) Observation {
	return Observation{
		SequenceNumber: uint64(frame.SeqTo),
		BlockHash:      "", // bare-metal stream has no Nitro blockHash; matching is by sequence
		FeedTimestamp:  time.Unix(int64(frame.Timestamp), 0), // #nosec G115 -- feed timestamps are unix seconds
	}
}
