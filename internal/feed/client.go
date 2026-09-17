package feed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const OfficialMainnetURL = "wss://feed.mainnet.chain.robinhood.com"

const (
	clientVersionHeader = "Arbitrum-Feed-Client-Version"
	maxMessageBytes     = 16 << 20
	readTimeout         = 30 * time.Second
	writeTimeout        = 5 * time.Second
	pingInterval        = 10 * time.Second
	reconnectMinimum    = 250 * time.Millisecond
	reconnectMaximum    = 10 * time.Second
)

type Endpoint struct {
	Name  string
	URL   string
	Token string
}

type UpdateKind uint8

const (
	UpdateConnecting UpdateKind = iota + 1
	UpdateConnected
	UpdateDisconnected
	UpdateObservation
)

type Update struct {
	Kind          UpdateKind
	Endpoint      string
	At            time.Time
	ConnectTime   time.Duration
	Sequence      uint64
	BlockHash     string
	FeedTimestamp time.Time
	FrameBytes    int
	Err           error
}

func ValidateEndpoint(endpoint Endpoint) error {
	if strings.TrimSpace(endpoint.Name) == "" {
		return errors.New("feed name must not be empty")
	}
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("feed %q has an invalid URL", endpoint.Name)
	}
	if parsed.Scheme == "wss" {
		return nil
	}
	if parsed.Scheme == "ws" && isLoopback(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("feed %q must use wss; ws is allowed only for loopback", endpoint.Name)
}

func Run(ctx context.Context, endpoint Endpoint, maxAge time.Duration, output chan<- Update) {
	delay := reconnectMinimum
	for {
		if !send(ctx, output, Update{Kind: UpdateConnecting, Endpoint: endpoint.Name, At: time.Now()}) {
			return
		}
		started := time.Now()
		connection, response, err := dial(ctx, endpoint)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
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
			_ = connection.Close()
			return
		}
		progress, consumeErr := consume(ctx, endpoint.Name, connection, maxAge, output)
		_ = connection.Close()
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

func dial(ctx context.Context, endpoint Endpoint) (*websocket.Conn, *http.Response, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   512 << 10,
		WriteBufferSize:  64 << 10,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			conn, err := d.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetNoDelay(true)
				_ = tcp.SetReadBuffer(4 << 20)
				_ = tcp.SetWriteBuffer(1 << 20)
			}
			return conn, nil
		},
	}
	headers := http.Header{clientVersionHeader: []string{"2"}}
	if endpoint.Token != "" {
		headers.Set("Authorization", "Bearer "+endpoint.Token)
	}
	return dialer.DialContext(ctx, endpoint.URL, headers)
}

func consume(ctx context.Context, name string, connection *websocket.Conn, maxAge time.Duration, output chan<- Update) (bool, error) {
	connection.SetReadLimit(maxMessageBytes)
	if err := connection.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return false, err
	}
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(readTimeout))
	})
	done := make(chan struct{})
	go keepAlive(ctx, connection, done)
	defer close(done)

	progress := false
	for {
		if err := connection.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return progress, err
		}
		messageType, payload, err := connection.ReadMessage()
		// Timestamp immediately after the full WebSocket frame is available.
		receivedAt := time.Now()
		if err != nil {
			return progress, errors.New("connection closed")
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		observations, err := DecodeObservations(payload)
		if err != nil {
			// Skip malformed frames instead of tearing down the whole connection.
			continue
		}
		progress = true
		for index, observation := range observations {
			age := receivedAt.Sub(observation.FeedTimestamp)
			if age < -maxAge || age > maxAge {
				continue
			}
			frameBytes := 0
			if index == 0 {
				frameBytes = len(payload)
			}
			if !send(ctx, output, Update{
				Kind: UpdateObservation, Endpoint: name, At: receivedAt,
				Sequence: observation.SequenceNumber, BlockHash: observation.BlockHash,
				FeedTimestamp: observation.FeedTimestamp, FrameBytes: frameBytes,
			}) {
				return progress, ctx.Err()
			}
		}
	}
}

func keepAlive(ctx context.Context, connection *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout))
		case <-ctx.Done():
			_ = connection.Close()
			return
		case <-done:
			return
		}
	}
}

func send(ctx context.Context, output chan<- Update, update Update) bool {
	select {
	case output <- update:
		return true
	case <-ctx.Done():
		return false
	}
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextDelay(delay time.Duration) time.Duration {
	if delay >= reconnectMaximum || delay > reconnectMaximum-delay {
		return reconnectMaximum
	}
	return delay * 2
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
