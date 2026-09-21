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

// IsRHF2Endpoint reports whether the endpoint is an RHF2 binary TCP stream.
func IsRHF2Endpoint(endpoint Endpoint) bool {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "tcp", "rhf2", "tcp-listen", "rhf2-listen":
		return true
	default:
		return false
	}
}

func rhf2ListenMode(parsed *url.URL) bool {
	switch strings.ToLower(parsed.Scheme) {
	case "tcp-listen", "rhf2-listen":
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "" || host == "0.0.0.0" || host == "*" || host == "listen"
}

func runRHF2(ctx context.Context, endpoint Endpoint, maxAge time.Duration, output chan<- Update) {
	delay := reconnectMinimum
	for {
		if !send(ctx, output, Update{Kind: UpdateConnecting, Endpoint: endpoint.Name, At: time.Now()}) {
			return
		}
		started := time.Now()
		conn, err := openRHF2(ctx, endpoint.URL)
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
		progress, consumeErr := consumeRHF2(ctx, endpoint.Name, conn, maxAge, output)
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

func OpenRHF2(ctx context.Context, rawURL string) (net.Conn, error) {
	return openRHF2(ctx, rawURL)
}

func openRHF2(ctx context.Context, rawURL string) (net.Conn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	port := parsed.Port()
	if port == "" {
		return nil, errors.New("rhf2 URL must include a port")
	}
	if rhf2ListenMode(parsed) {
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
			conn, err := listener.Accept()
			ch <- accepted{conn: conn, err: err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-ch:
			if result.err != nil {
				return nil, result.err
			}
			tuneTCP(result.conn)
			return result.conn, nil
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

func tuneTCP(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetReadBuffer(4 << 20)
	}
}

func consumeRHF2(ctx context.Context, name string, conn net.Conn, maxAge time.Duration, output chan<- Update) (bool, error) {
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
				frame, consumed, decodeErr := DecodeRHF2Frame(buf)
				if decodeErr != nil {
					if errors.Is(decodeErr, ErrRHF2Incomplete) {
						break
					}
					// Resync on next magic if the stream drifts.
					if idx := indexRHF2(buf, 1); idx > 0 {
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

func indexRHF2(data []byte, start int) int {
	if start < 0 {
		start = 0
	}
	for i := start; i+4 <= len(data); i++ {
		if data[i] == 'R' && data[i+1] == 'H' && data[i+2] == 'F' && data[i+3] == '2' {
			return i
		}
	}
	return -1
}
