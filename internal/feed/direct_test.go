package feed

import (
	"encoding/binary"
	"testing"
)

func TestDecodeDirectITEM(t *testing.T) {
	headerJSON := []byte(`{"kind":3}`)
	l2Item := []byte{3, 1, 2, 3, 4}
	payloadLen := directPrefixSize + len(headerJSON) + len(l2Item)
	laf := uint32(directFlag) | uint32(payloadLen)

	outer := make([]byte, directOuterHeaderSize)
	binary.LittleEndian.PutUint32(outer[0:4], laf)
	binary.LittleEndian.PutUint64(outer[4:12], 1_700_000_000_000_000_000)
	binary.LittleEndian.PutUint64(outer[12:20], 1_700_000_000_000_100_000)

	payload := make([]byte, payloadLen)
	payload[0] = 1 // version
	payload[1] = directRecordItem
	payload[2] = 0
	payload[3] = 0
	binary.LittleEndian.PutUint64(payload[4:12], 42)
	binary.LittleEndian.PutUint32(payload[12:16], 0) // ordinal
	binary.LittleEndian.PutUint32(payload[16:20], uint32(len(headerJSON)))
	binary.LittleEndian.PutUint32(payload[20:24], uint32(len(l2Item)))
	copy(payload[24:], headerJSON)
	copy(payload[24+len(headerJSON):], l2Item)

	gotLen, eventNS, sendNS, err := DecodeDirectOuter(outer)
	if err != nil {
		t.Fatal(err)
	}
	if gotLen != payloadLen || eventNS == 0 || sendNS == 0 {
		t.Fatalf("outer: len=%d event=%d send=%d", gotLen, eventNS, sendNS)
	}
	frame, err := DecodeDirectPayload(payload, eventNS, sendNS)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Sequence != 42 || frame.Ordinal != 0 || frame.RecordType != directRecordItem {
		t.Fatalf("frame=%+v", frame)
	}
	if string(frame.First) != string(headerJSON) || string(frame.Second) != string(l2Item) {
		t.Fatalf("fields mismatch first=%q second=%q", frame.First, frame.Second)
	}
}

func TestDecodeDirectRejectsNGF1FalsePositive(t *testing.T) {
	// ASCII "NGF1...." — bit 0x10000000 happens to be set, but payload_len is huge.
	hdr := []byte("NGF1\x04\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	_, _, _, err := DecodeDirectOuter(hdr)
	if err == nil {
		t.Fatal("expected reject of NGF1 false-positive")
	}
}
