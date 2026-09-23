package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// IsDirectEndpoint reports whether the endpoint is a DIRECT TCP stream.
func IsDirectEndpoint(endpoint Endpoint) bool {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "direct", "direct-listen", "tcp-direct":
		return true
	default:
		return false
	}
}

func directListenMode(parsed *url.URL) bool {
	switch strings.ToLower(parsed.Scheme) {
	case "direct-listen":
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "" || host == "0.0.0.0" || host == "*" || host == "listen"
}

func runDirect(ctx context.Context, endpoint Endpoint, maxAge time.Duration, output chan<- Update) {
	delay := reconnectMinimum
	for {
		if !send(ctx, output, Update{Kind: UpdateConnecting, Endpoint: endpoint.Name, At: time.Now()}) {
			return
		}
		started := time.Now()
		conn, err := openDirect(ctx, endpoint.URL)
		if err != nil {
			if !send(ctx, output, Update{Kind: UpdateDisconnected, Endpoint: endpoint.Name, At: time.Now(), Err: fmt.Errorf("connection failed: %w", err)}) {
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
		progress, consumeErr := consumeDirect(ctx, endpoint.Name, conn, maxAge, output)
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

func openDirect(ctx context.Context, rawURL string) (net.Conn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	port := parsed.Port()
	if port == "" {
		return nil, errors.New("direct URL must include a port")
	}
	if directListenMode(parsed) {
		host := parsed.Hostname()
		if host == "" || host == "*" || host == "listen" {
			host = "0.0.0.0"
		}
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(host, port))
		if err != nil {
			return nil, err
		}
		defer listener.Close()
		type accepted struct {
			conn net.Conn
			err  error
		}
		ch := make(chan accepted, 1)
		go func() {
			c, e := listener.Accept()
			ch <- accepted{c, e}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case a := <-ch:
			if a.err != nil {
				return nil, a.err
			}
			tuneDirectTCP(a.conn)
			return a.conn, nil
		}
	}
	dialer := (&net.Dialer{}).DialContext
	conn, err := dialer(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return nil, err
	}
	tuneDirectTCP(conn)
	return conn, nil
}

func tuneDirectTCP(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetReadBuffer(4 << 20)
	}
}

func consumeDirect(ctx context.Context, name string, conn net.Conn, maxAge time.Duration, output chan<- Update) (bool, error) {
	progress := false
	// Dedup (sequence, ordinal); emit observation on first ITEM per sequence.
	seenSeq := make(map[uint64]struct{}, 4096)
	seenItem := make(map[[2]uint64]struct{}, 8192)
	buf := make([]byte, 0, 256<<10)
	tmp := make([]byte, 64<<10)

	for {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return progress, err
		}
		for len(buf) < directOuterHeaderSize {
			n, err := conn.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				if errors.Is(err, io.EOF) && len(buf) == 0 {
					return progress, errors.New("connection closed")
				}
				if len(buf) == 0 {
					return progress, err
				}
				break
			}
			select {
			case <-ctx.Done():
				return progress, ctx.Err()
			default:
			}
		}
		if len(buf) < directOuterHeaderSize {
			return progress, ErrDirectIncomplete
		}

		payloadLen, eventNS, sendNS, err := DecodeDirectOuter(buf[:directOuterHeaderSize])
		if err != nil {
			return progress, err
		}
		total := directOuterHeaderSize + payloadLen
		for len(buf) < total {
			n, err := conn.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				return progress, fmt.Errorf("direct: short payload: %w", err)
			}
		}
		receivedAt := time.Now()
		frame, err := DecodeDirectPayload(buf[directOuterHeaderSize:total], eventNS, sendNS)
		if err != nil {
			return progress, err
		}
		buf = buf[total:]

		switch frame.RecordType {
		case directRecordItem:
			key := [2]uint64{frame.Sequence, uint64(frame.Ordinal)}
			if _, ok := seenItem[key]; ok {
				continue
			}
			seenItem[key] = struct{}{}
			// First ITEM for this sequence drives first-arrival race.
			if _, ok := seenSeq[frame.Sequence]; ok {
				continue
			}
			seenSeq[frame.Sequence] = struct{}{}
			obs := ObservationFromDirectITEM(frame, receivedAt)
			age := receivedAt.Sub(obs.FeedTimestamp)
			if maxAge > 0 && (age < -maxAge || age > maxAge) {
				continue
			}
			progress = true
			if !send(ctx, output, Update{
				Kind:          UpdateObservation,
				Endpoint:      name,
				At:            receivedAt,
				Sequence:      obs.SequenceNumber,
				BlockHash:     obs.BlockHash,
				FeedTimestamp: obs.FeedTimestamp,
				FrameBytes:    frame.OuterSize,
			}) {
				return progress, nil
			}
		case directRecordFinal:
			// Canonical completion — unused for first-arrival race.
			continue
		}
	}
}
