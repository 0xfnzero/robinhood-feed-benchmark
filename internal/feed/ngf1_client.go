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

// IsNGF1Endpoint reports whether the endpoint is an NGF1 TCP stream.
func IsNGF1Endpoint(endpoint Endpoint) bool {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "ngf1", "ngf1-listen":
		return true
	default:
		return false
	}
}

func ngf1ListenMode(parsed *url.URL) bool {
	switch strings.ToLower(parsed.Scheme) {
	case "ngf1-listen":
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "" || host == "0.0.0.0" || host == "*" || host == "listen"
}

func runNGF1(ctx context.Context, endpoint Endpoint, maxAge time.Duration, output chan<- Update) {
	delay := reconnectMinimum
	for {
		if !send(ctx, output, Update{Kind: UpdateConnecting, Endpoint: endpoint.Name, At: time.Now()}) {
			return
		}
		started := time.Now()
		conn, err := openNGF1(ctx, endpoint.URL)
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
		progress, consumeErr := consumeNGF1(ctx, endpoint.Name, conn, maxAge, output)
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

func openNGF1(ctx context.Context, rawURL string) (net.Conn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	port := parsed.Port()
	if port == "" {
		return nil, errors.New("ngf1 URL must include a port")
	}
	if ngf1ListenMode(parsed) {
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
			tuneTCP(a.conn)
			return a.conn, nil
		}
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return nil, err
	}
	tuneTCP(conn)
	return conn, nil
}

func consumeNGF1(ctx context.Context, name string, conn net.Conn, maxAge time.Duration, output chan<- Update) (bool, error) {
	buf := make([]byte, 0, 256<<10)
	tmp := make([]byte, 64<<10)
	var lastSeq uint32
	var hasLast bool
	progress := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return progress, err
		}
		n, err := conn.Read(tmp)
		receivedAt := time.Now()
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				frames, consumed, decodeErr := DecodeNGF1Frame(buf)
				if decodeErr != nil {
					if errors.Is(decodeErr, ErrNGF1Incomplete) {
						break
					}
					if idx := indexNGF1(buf, 1); idx > 0 {
						buf = buf[idx:]
						continue
					}
					if len(buf) > maxMessageBytes {
						buf = buf[:0]
					}
					break
				}
				buf = buf[consumed:]
				progress = true
				for _, frame := range frames {
					if frame.TxIndex != 0 {
						continue
					}
					if hasLast && frame.SeqTo == lastSeq {
						continue
					}
					lastSeq = frame.SeqTo
					hasLast = true
					observation := ObservationFromRHF2(frame)
					age := receivedAt.Sub(observation.FeedTimestamp)
					if maxAge > 0 && (age < -maxAge || age > maxAge) {
						continue
					}
					if !send(ctx, output, Update{
						Kind: UpdateObservation, Endpoint: name, At: receivedAt,
						Sequence: observation.SequenceNumber, BlockHash: observation.BlockHash,
						FeedTimestamp: observation.FeedTimestamp, FrameBytes: frame.Size,
					}) {
						return progress, ctx.Err()
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return progress, errors.New("connection closed")
			}
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
