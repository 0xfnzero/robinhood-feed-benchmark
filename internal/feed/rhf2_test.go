package feed

import (
	"os"
	"testing"
	"time"
)

func TestDecodeRHF2FrameFromSample(t *testing.T) {
	raw, err := os.ReadFile("/tmp/19770-sample.bin")
	if err != nil {
		t.Skip("sample capture not present:", err)
	}
	var frames []RHF2Frame
	offset := 0
	for offset < len(raw) {
		frame, n, err := DecodeRHF2Frame(raw[offset:])
		if err != nil {
			if err == ErrRHF2Incomplete {
				break // capture may end mid-frame
			}
			t.Fatalf("decode at %d: %v", offset, err)
		}
		frames = append(frames, frame)
		offset += n
	}
	if len(frames) < 10 {
		t.Fatalf("expected many frames, got %d", len(frames))
	}
	first := frames[0]
	if first.SeqTo == 0 || first.Timestamp < 1_700_000_000 {
		t.Fatalf("unexpected first frame: %+v", first)
	}
	obs := ObservationFromRHF2(first)
	if obs.SequenceNumber != uint64(first.SeqTo) {
		t.Fatalf("observation seq mismatch: %+v", obs)
	}
	if obs.FeedTimestamp.Unix() != int64(first.Timestamp) {
		t.Fatalf("observation ts mismatch: %v vs %d", obs.FeedTimestamp, first.Timestamp)
	}
	if time.Since(obs.FeedTimestamp) > 365*24*time.Hour {
		t.Fatalf("timestamp looks wrong: %v", obs.FeedTimestamp)
	}
}

func TestDecodeRHF2Incomplete(t *testing.T) {
	if _, _, err := DecodeRHF2Frame([]byte("RHF2")); err != ErrRHF2Incomplete {
		t.Fatalf("want incomplete, got %v", err)
	}
}

func TestResolveVendorRHF2(t *testing.T) {
	endpoint, err := ResolveFormat("RHF2", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Name != VendorRHF2 || endpoint.URL != RHF2DefaultListenURL {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
}
