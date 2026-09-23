package feed

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// NGF1 envelope observed on :19780 (intranet TCP push).
// Distinct from the DIRECT wire format (VendorDirect).
//
// Wire:
//
//	NGF1 header (39B, plen u32 BE @35) | payload (PRH1 + RHF2…) | trailer (32B)
//
// Inner RHF2 may have body[0]=0 and timestamp=0; we patch those so the standard
// RHF2 decoder accepts the frame (arrival order is unchanged).

const (
	ngf1Magic      = "NGF1"
	ngf1HeaderSize = 39
	ngf1TrailerSize = 32
	ngf1PlenOffset = 35
)

var (
	ErrNGF1Incomplete = errors.New("ngf1: incomplete frame")
)

// DecodeNGF1Frame peels one NGF1 envelope and returns inner RHF2 frames (patched).
// consumed is the full NGF1 frame size (header+payload+trailer).
func DecodeNGF1Frame(data []byte) (frames []RHF2Frame, consumed int, err error) {
	if len(data) < ngf1HeaderSize {
		return nil, 0, ErrNGF1Incomplete
	}
	if string(data[:4]) != ngf1Magic {
		return nil, 0, fmt.Errorf("ngf1: bad magic %q", data[:4])
	}
	plen := int(binary.BigEndian.Uint32(data[ngf1PlenOffset : ngf1PlenOffset+4]))
	total := ngf1HeaderSize + plen + ngf1TrailerSize
	if plen < 0 || plen > maxMessageBytes || total < ngf1HeaderSize {
		return nil, 0, fmt.Errorf("ngf1: invalid payload length %d", plen)
	}
	if len(data) < total {
		return nil, 0, ErrNGF1Incomplete
	}
	payload := data[ngf1HeaderSize : ngf1HeaderSize+plen]
	frames = extractPatchedRHF2(payload)
	return frames, total, nil
}

func extractPatchedRHF2(payload []byte) []RHF2Frame {
	out := make([]RHF2Frame, 0, 4)
	i := indexRHF2(payload, 0)
	if i < 0 {
		return out
	}
	for i+rhf2HeaderSize <= len(payload) {
		if i < 0 || i+4 > len(payload) || string(payload[i:i+4]) != rhf2Magic {
			j := indexRHF2(payload, i+1)
			if j < 0 {
				break
			}
			i = j
			continue
		}
		patched := patchRHF2Wire(payload[i:])
		frame, n, err := DecodeRHF2Frame(patched)
		if err != nil {
			if errors.Is(err, ErrRHF2Incomplete) {
				break
			}
			j := indexRHF2(payload, i+1)
			if j < 0 {
				break
			}
			i = j
			continue
		}
		out = append(out, frame)
		i += n
	}
	return out
}

// patchRHF2Wire copies one candidate RHF2 frame and fixes NGF1-sourced quirks:
// body marker 0→1, timestamp 0→now (unix secs).
func patchRHF2Wire(data []byte) []byte {
	if len(data) < rhf2HeaderSize {
		return data
	}
	plen := int(binary.BigEndian.Uint32(data[44:48]))
	total := rhf2HeaderSize + plen + rhf2TrailerSize
	if plen < 16 || plen > maxMessageBytes || len(data) < total {
		return data
	}
	out := append([]byte(nil), data[:total]...)
	body := rhf2HeaderSize
	if out[body] == 0 {
		out[body] = 1
	}
	ts := binary.BigEndian.Uint64(out[body+4 : body+12])
	if ts == 0 {
		binary.BigEndian.PutUint64(out[body+4:body+12], uint64(time.Now().Unix())) // #nosec G115
	}
	return out
}

func indexNGF1(data []byte, start int) int {
	if start < 0 {
		start = 0
	}
	for i := start; i+4 <= len(data); i++ {
		if data[i] == 'N' && data[i+1] == 'G' && data[i+2] == 'F' && data[i+3] == '1' {
			return i
		}
	}
	return -1
}
