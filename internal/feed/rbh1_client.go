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

// IsRBH1Endpoint reports whether the endpoint is an RBH1 binary TCP stream.
// WebSocket /bin is handled by the generic WS consumer (magic detect).
func IsRBH1Endpoint(endpoint Endpoint) bool {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "rbh1", "rbh1-listen", "tcp-rbh1":
		return true
	case "tcp":
		// Ambiguous with RHF2; only treat as RBH1 when Name/vendor says so.
		return strings.EqualFold(endpoint.Name, VendorRBH1)
	default:
		return false
	}
}

func rbh1ListenMode(parsed *url.URL) bool {
	switch strings.ToLower(parsed.Scheme) {
	case "rbh1-listen":
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "" || host == "0.0.0.0" || host == "*" || host == "listen"
}

func runRBH1(ctx context.Context, endpoint Endpoint, maxAge time.Duration, output chan<- Update) {
	delay := reconnectMinimum
	for {
		if !send(ctx, output, Update{Kind: UpdateConnecting, Endpoint: endpoint.Name, At: time.Now()}) {
			return
		}
		started := time.Now()
		conn, err := openRBH1(ctx, endpoint)
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
		progress, consumeErr := consumeRBH1(ctx, endpoint.Name, conn, maxAge, output)
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

func openRBH1(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return nil, err
	}
	port := parsed.Port()
	if port == "" {
		return nil, errors.New("rbh1 URL must include a port")
	}
	var conn net.Conn
	if rbh1ListenMode(parsed) {
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
			ch <- accepted{conn: c, err: e}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-ch:
			if result.err != nil {
				return nil, result.err
			}
			conn = result.conn
		}
	} else {
		var dialer net.Dialer
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
		if err != nil {
			return nil, err
		}
	}
	tuneTCP(conn)
	// Optional first-line token auth (gateway REQUIRE_X_TOKEN=true path).
	if token := strings.TrimSpace(endpoint.Token); token != "" {
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := io.WriteString(conn, token+"\n"); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("rbh1 auth write: %w", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		line := make([]byte, 64)
		n, err := conn.Read(line)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("rbh1 auth read: %w", err)
		}
		resp := strings.TrimSpace(string(line[:n]))
		if !strings.HasPrefix(resp, "OK") {
			_ = conn.Close()
			return nil, fmt.Errorf("rbh1 auth rejected: %s", resp)
		}
	}
	return conn, nil
}

func consumeRBH1(ctx context.Context, name string, conn net.Conn, maxAge time.Duration, output chan<- Update) (bool, error) {
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
				frame, consumed, decodeErr := DecodeRBH1Frame(buf)
				if decodeErr != nil {
					if errors.Is(decodeErr, ErrRBH1Incomplete) {
						break
					}
					if idx := indexRBH1(buf, 1); idx > 0 {
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
				if frame.Frag != nil {
					continue // stream path should not fragment; skip stray UDP frags
				}
				if frame.TxIndex != 0 {
					continue
				}
				if hasLast && frame.Seq == lastSeq {
					continue
				}
				lastSeq = frame.Seq
				hasLast = true
				observation := ObservationFromRBH1(frame)
				age := receivedAt.Sub(observation.FeedTimestamp)
				if age < -maxAge || age > maxAge {
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

func indexRBH1(data []byte, start int) int {
	if start < 0 {
		start = 0
	}
	for i := start; i+4 <= len(data); i++ {
		if data[i] == 'R' && data[i+1] == 'B' && data[i+2] == 'H' && data[i+3] == '1' {
			return i
		}
	}
	return -1
}
