package feed

import (
	"encoding/json"
	"testing"
)

func TestEncodeNitroEnvelopeRoundTrip(t *testing.T) {
	tx := []byte{0x02, 0xf8, 0x6f, 0x82, 0x12, 0x37}
	raw, err := EncodeNitroEnvelope(42, 1789894538, [][]byte{tx})
	if err != nil {
		t.Fatal(err)
	}
	observations, err := DecodeObservations(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].SequenceNumber != 42 {
		t.Fatalf("unexpected observations: %+v", observations)
	}
	if observations[0].BlockHash == "" || len(observations[0].BlockHash) != 66 {
		t.Fatalf("unexpected block hash: %q", observations[0].BlockHash)
	}

	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if int(envelope["version"].(float64)) != 1 {
		t.Fatalf("unexpected version: %#v", envelope["version"])
	}
}

func TestEncodeNitroEnvelopeRequiresTxs(t *testing.T) {
	if _, err := EncodeNitroEnvelope(1, 1, nil); err == nil {
		t.Fatal("expected error")
	}
}
