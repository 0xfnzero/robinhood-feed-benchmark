package feed

import (
	"encoding/binary"
	"testing"
	"time"
)

func encodeTestRBH1(kind, flags, tx uint8, seq uint32, eventNS uint64, payload []byte) []byte {
	ext := 0
	if flags&rbh1FlagEventNS != 0 {
		ext += 8
	}
	out := make([]byte, rbh1HeaderSize+ext+len(payload))
	copy(out[:4], rbh1Magic)
	out[4] = rbh1Version
	out[5] = flags
	out[6] = kind
	out[7] = tx
	binary.LittleEndian.PutUint32(out[8:12], seq)
	binary.LittleEndian.PutUint32(out[12:16], uint32(len(payload)))
	off := rbh1HeaderSize
	if flags&rbh1FlagEventNS != 0 {
		binary.LittleEndian.PutUint64(out[off:off+8], eventNS)
		off += 8
	}
	copy(out[off:], payload)
	return out
}

func TestDecodeRBH1Frame(t *testing.T) {
	ns := uint64(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC).UnixNano())
	payload := []byte{0x02, 0xf8, 0xad, 0x82}
	wire := encodeTestRBH1(rbh1KindEthTx, rbh1FlagFinal|rbh1FlagEventNS, 0, 72230069, ns, payload)
	frame, n, err := DecodeRBH1Frame(wire)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(wire) {
		t.Fatalf("consumed %d want %d", n, len(wire))
	}
	if frame.Seq != 72230069 || frame.Kind != rbh1KindEthTx || frame.TxIndex != 0 {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if !frame.HasEvent || frame.EventNS != ns {
		t.Fatalf("event_ns mismatch: %+v", frame)
	}
	if string(frame.Payload) != string(payload) {
		t.Fatalf("payload mismatch")
	}
	obs := ObservationFromRBH1(frame)
	if obs.SequenceNumber != 72230069 || obs.BlockHash != "" {
		t.Fatalf("observation: %+v", obs)
	}
	if obs.FeedTimestamp.UnixNano() != int64(ns) {
		t.Fatalf("ts mismatch %v vs %d", obs.FeedTimestamp, ns)
	}
}

func TestDecodeRBH1Incomplete(t *testing.T) {
	if _, _, err := DecodeRBH1Frame([]byte("RBH1")); err != ErrRBH1Incomplete {
		t.Fatalf("want incomplete, got %v", err)
	}
}

func TestResolveFormatRBH1(t *testing.T) {
	endpoint, err := ResolveFormat("RBH1", "ws://127.0.0.1:9642/bin", "tok", "X-Token")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Name != VendorRBH1 || endpoint.URL != "ws://127.0.0.1:9642/bin" {
		t.Fatalf("unexpected: %+v", endpoint)
	}
	if endpoint.Token != "tok" || endpoint.AuthHeader != "X-Token" {
		t.Fatalf("auth: %+v", endpoint)
	}
	tcp, err := ResolveFormat("RBH1", "rbh1://127.0.0.1:19791", "tok", "")
	if err != nil {
		t.Fatal(err)
	}
	if !IsRBH1Endpoint(tcp) {
		t.Fatalf("expected RBH1 endpoint: %+v", tcp)
	}
	udp, err := ResolveFormat("RBH1", "udp://0.0.0.0:19792", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !IsRBH1Endpoint(udp) || udp.URL != "udp://0.0.0.0:19792" {
		t.Fatalf("expected UDP RBH1 endpoint: %+v", udp)
	}
}

func encodeTestRBH1Frag(kind, flags, tx uint8, seq uint32, eventNS uint64, totalLen uint32, fragIdx, fragCnt uint16, payload []byte) []byte {
	flags |= rbh1FlagFrag
	ext := 8
	if flags&rbh1FlagEventNS != 0 {
		ext += 8
	}
	out := make([]byte, rbh1HeaderSize+ext+len(payload))
	copy(out[:4], rbh1Magic)
	out[4] = rbh1Version
	out[5] = flags
	out[6] = kind
	out[7] = tx
	binary.LittleEndian.PutUint32(out[8:12], seq)
	binary.LittleEndian.PutUint32(out[12:16], uint32(len(payload)))
	off := rbh1HeaderSize
	if flags&rbh1FlagEventNS != 0 {
		binary.LittleEndian.PutUint64(out[off:off+8], eventNS)
		off += 8
	}
	binary.LittleEndian.PutUint32(out[off:off+4], totalLen)
	binary.LittleEndian.PutUint16(out[off+4:off+6], fragIdx)
	binary.LittleEndian.PutUint16(out[off+6:off+8], fragCnt)
	off += 8
	copy(out[off:], payload)
	return out
}

func TestRBH1UDPReassembler(t *testing.T) {
	ns := uint64(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC).UnixNano())
	body := []byte("abcdefghijklmnopqrstuvwxyz012345")
	reasm := newRBH1UDPReassembler()
	now := time.Now()

	part0 := encodeTestRBH1Frag(rbh1KindEthTx, rbh1FlagFinal|rbh1FlagEventNS, 0, 42, ns, uint32(len(body)), 0, 2, body[:16])
	if _, ok, err := reasm.Push(part0, now); err != nil || ok {
		t.Fatalf("part0: ok=%v err=%v", ok, err)
	}
	part1 := encodeTestRBH1Frag(rbh1KindEthTx, rbh1FlagFinal, 0, 42, 0, uint32(len(body)), 1, 2, body[16:])
	frame, ok, err := reasm.Push(part1, now)
	if err != nil || !ok {
		t.Fatalf("part1: ok=%v err=%v", ok, err)
	}
	if frame.Seq != 42 || string(frame.Payload) != string(body) || !frame.HasEvent || frame.EventNS != ns {
		t.Fatalf("reassembled: %+v payload=%q", frame, frame.Payload)
	}
}
