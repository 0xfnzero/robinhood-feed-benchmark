package feed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const rbh1UDPFragTTL = 5 * time.Second

type rbh1FragPending struct {
	totalLen uint32
	fragCnt  uint16
	parts    map[uint16][]byte
	eventNS  uint64
	hasEvent bool
	kind     uint8
	txIndex  uint8
	flags    uint8
	seenAt   time.Time
}

type rbh1FragKey struct {
	seq     uint32
	txIndex uint8
}

type rbh1UDPReassembler struct {
	pending map[rbh1FragKey]*rbh1FragPending
}

func newRBH1UDPReassembler() *rbh1UDPReassembler {
	return &rbh1UDPReassembler{pending: make(map[rbh1FragKey]*rbh1FragPending)}
}

// Push returns a complete (non-frag) frame when the datagram finishes a unit.
func (r *rbh1UDPReassembler) Push(data []byte, now time.Time) (RBH1Frame, bool, error) {
	r.expire(now)
	frame, _, err := DecodeRBH1Frame(data)
	if err != nil {
		return RBH1Frame{}, false, err
	}
	if frame.Frag == nil {
		return frame, true, nil
	}
	frag := frame.Frag
	key := rbh1FragKey{seq: frame.Seq, txIndex: frame.TxIndex}
	entry := r.pending[key]
	if entry == nil || entry.fragCnt != frag.FragCnt || entry.totalLen != frag.TotalLen {
		entry = &rbh1FragPending{
			totalLen: frag.TotalLen,
			fragCnt:  frag.FragCnt,
			parts:    make(map[uint16][]byte, frag.FragCnt),
			kind:     frame.Kind,
			txIndex:  frame.TxIndex,
			flags:    frame.Flags &^ rbh1FlagFrag,
			seenAt:   now,
		}
		r.pending[key] = entry
	}
	if frame.HasEvent {
		entry.eventNS = frame.EventNS
		entry.hasEvent = true
	}
	entry.parts[frag.FragIdx] = append([]byte(nil), frame.Payload...)
	entry.seenAt = now
	if len(entry.parts) < int(entry.fragCnt) {
		return RBH1Frame{}, false, nil
	}
	body := make([]byte, 0, entry.totalLen)
	for i := uint16(0); i < entry.fragCnt; i++ {
		part, ok := entry.parts[i]
		if !ok {
			return RBH1Frame{}, false, nil
		}
		body = append(body, part...)
	}
	delete(r.pending, key)
	if uint32(len(body)) != entry.totalLen {
		return RBH1Frame{}, false, fmt.Errorf("rbh1 udp: reassembled len %d != %d", len(body), entry.totalLen)
	}
	complete := RBH1Frame{
		Flags:    entry.flags,
		Kind:     entry.kind,
		TxIndex:  entry.txIndex,
		Seq:      frame.Seq,
		EventNS:  entry.eventNS,
		HasEvent: entry.hasEvent,
		Payload:  body,
		Size:     len(data),
	}
	return complete, true, nil
}

func (r *rbh1UDPReassembler) expire(now time.Time) {
	for key, entry := range r.pending {
		if now.Sub(entry.seenAt) > rbh1UDPFragTTL {
			delete(r.pending, key)
		}
	}
}

func isRBH1UDP(parsed *url.URL) bool {
	switch strings.ToLower(parsed.Scheme) {
	case "udp", "udp-rbh1", "rbh1-udp":
		return true
	default:
		return false
	}
}

func runRBH1UDP(ctx context.Context, endpoint Endpoint, maxAge time.Duration, output chan<- Update) {
	delay := reconnectMinimum
	for {
		if !send(ctx, output, Update{Kind: UpdateConnecting, Endpoint: endpoint.Name, At: time.Now()}) {
			return
		}
		started := time.Now()
		conn, err := openRBH1UDP(ctx, endpoint)
		if err != nil {
			if !send(ctx, output, Update{Kind: UpdateDisconnected, Endpoint: endpoint.Name, At: time.Now(), Err: fmt.Errorf("udp listen failed: %w", err)}) {
				return
			}
			if !wait(ctx, delay) {
				return
			}
			delay = nextDelay(delay)
			continue
		}
		if !send(ctx, output, Update{Kind: UpdateConnected, Endpoint: endpoint.Name, At: time.Now(), ConnectTime: time.Since(started)}) {
			_ = conn.Close()
			return
		}
		progress, consumeErr := consumeRBH1UDP(ctx, endpoint.Name, conn, maxAge, output)
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
		if !send(ctx, output, Update{Kind: UpdateDisconnected, Endpoint: endpoint.Name, At: time.Now(), Err: consumeErr}) {
			return
		}
		if progress {
			delay = reconnectMinimum
		}
		if !wait(ctx, delay) {
			return
		}
		delay = nextDelay(delay)
	}
}

func openRBH1UDP(ctx context.Context, endpoint Endpoint) (net.PacketConn, error) {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return nil, err
	}
	port := parsed.Port()
	if port == "" {
		return nil, errors.New("rbh1 udp URL must include a port")
	}
	host := parsed.Hostname()
	if host == "" || host == "*" || host == "listen" {
		host = "0.0.0.0"
	}
	var listenConfig net.ListenConfig
	return listenConfig.ListenPacket(ctx, "udp", net.JoinHostPort(host, port))
}

func consumeRBH1UDP(ctx context.Context, name string, conn net.PacketConn, maxAge time.Duration, output chan<- Update) (bool, error) {
	buf := make([]byte, 64<<10)
	reasm := newRBH1UDPReassembler()
	progress := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return progress, err
		}
		n, _, err := conn.ReadFrom(buf)
		receivedAt := time.Now()
		if n > 0 {
			frame, ok, decodeErr := reasm.Push(buf[:n], receivedAt)
			if decodeErr != nil {
				// Bad datagram; keep listening.
			} else if ok {
				progress = true
				if !emitRBH1Frame(ctx, name, frame, receivedAt, maxAge, frame.Size, output) {
					return progress, ctx.Err()
				}
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					return progress, ctx.Err()
				}
				continue
			}
			return progress, err
		}
		if ctx.Err() != nil {
			return progress, ctx.Err()
		}
	}
}
