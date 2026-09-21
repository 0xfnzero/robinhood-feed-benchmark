package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/0xfnzero/robinhood-feed-benchmark/internal/feed"
	"github.com/gorilla/websocket"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Println("rhf2-relay", version)
		return nil
	}

	fs := flag.NewFlagSet("rhf2-relay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rhf2URL := fs.String("rhf2-url", envOr("FEED_URL_1", feed.RHF2DefaultListenURL), "RHF2 listen/dial URL, e.g. tcp://0.0.0.0:19770")
	nitroHost := fs.String("nitro-host", envOr("NITRO_FEED_HOST", "0.0.0.0"), "Nitro WebSocket bind host")
	nitroPort := fs.Int("nitro-port", envInt("NITRO_FEED_PORT", 9642), "Nitro WebSocket bind port")
	nitroPath := fs.String("nitro-path", envOr("NITRO_FEED_PATH", "/feed"), "Nitro WebSocket path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nitroPort <= 0 || *nitroPort > 65535 {
		return errors.New("NITRO_FEED_PORT must be 1-65535")
	}
	if !strings.HasPrefix(*nitroPath, "/") {
		*nitroPath = "/" + *nitroPath
	}

	endpoint, err := feed.ResolveFormat("RHF2", *rhf2URL, "", "")
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := newHub()
	mux := http.NewServeMux()
	mux.HandleFunc(*nitroPath, hub.handleFeed)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != *nitroPath {
			http.NotFound(w, r)
			return
		}
		hub.handleFeed(w, r)
	})

	addr := net.JoinHostPort(*nitroHost, strconv.Itoa(*nitroPort))
	server := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(os.Stderr, "rhf2-relay %s\n", version)
	fmt.Fprintf(os.Stderr, "RHF2 input:  %s\n", endpoint.URL)
	fmt.Fprintf(os.Stderr, "Nitro output: ws://%s%s\n", addr, *nitroPath)

	errCh := make(chan error, 2)
	go func() {
		errCh <- ingestRHF2(ctx, endpoint.URL, hub)
	}()
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func ingestRHF2(ctx context.Context, rhf2URL string, hub *hub) error {
	delay := 250 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "[rhf2] waiting for connection on %s\n", rhf2URL)
		conn, err := feed.OpenRHF2(ctx, rhf2URL)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(os.Stderr, "[rhf2] accept/dial failed: %v\n", err)
			if !sleep(ctx, delay) {
				return ctx.Err()
			}
			if delay < 10*time.Second {
				delay *= 2
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "[rhf2] peer connected: %s\n", conn.RemoteAddr())
		delay = 250 * time.Millisecond
		err = pumpRHF2(ctx, conn, hub)
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "[rhf2] peer disconnected: %v\n", err)
		if !sleep(ctx, delay) {
			return ctx.Err()
		}
	}
}

type blockBuffer struct {
	seq  uint32
	ts   uint64
	txs  [][]byte
	open bool
}

func pumpRHF2(ctx context.Context, conn net.Conn, hub *hub) error {
	buf := make([]byte, 0, 256<<10)
	tmp := make([]byte, 64<<10)
	var current blockBuffer
	flush := func() error {
		if !current.open || len(current.txs) == 0 {
			current = blockBuffer{}
			return nil
		}
		payload, err := feed.EncodeNitroEnvelope(uint64(current.seq), current.ts, current.txs)
		current = blockBuffer{}
		if err != nil {
			return err
		}
		hub.broadcast(payload)
		return nil
	}

	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			_ = flush()
			return err
		}
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				frame, consumed, decodeErr := feed.DecodeRHF2Frame(buf)
				if decodeErr != nil {
					if errors.Is(decodeErr, feed.ErrRHF2Incomplete) {
						break
					}
					if idx := indexOfRHF2(buf, 1); idx > 0 {
						buf = buf[idx:]
						continue
					}
					if len(buf) > 16<<20 {
						buf = buf[:0]
					}
					break
				}
				buf = buf[consumed:]
				if current.open && frame.SeqTo != current.seq {
					if err := flush(); err != nil {
						return err
					}
				}
				if !current.open {
					current = blockBuffer{seq: frame.SeqTo, ts: frame.Timestamp, open: true}
				}
				if len(frame.Payload) > 0 {
					current.txs = append(current.txs, append([]byte(nil), frame.Payload...))
				}
				if frame.Timestamp > current.ts {
					current.ts = frame.Timestamp
				}
			}
		}
		if err != nil {
			_ = flush()
			if errors.Is(err, io.EOF) {
				return errors.New("connection closed")
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			return err
		}
		if ctx.Err() != nil {
			_ = flush()
			return ctx.Err()
		}
	}
}

type hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]chan []byte
}

func newHub() *hub {
	return &hub{clients: make(map[*websocket.Conn]chan []byte)}
}

func (h *hub) broadcast(payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.clients {
		select {
		case ch <- payload:
		default:
			// Drop if a slow client falls behind.
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin:       func(*http.Request) bool { return true },
	EnableCompression: true,
}

func (h *hub) handleFeed(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, http.Header{
		"Arbitrum-Feed-Server-Version": []string{"2"},
	})
	if err != nil {
		return
	}
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[conn] = ch
	h.mu.Unlock()
	fmt.Fprintf(os.Stderr, "[nitro] client connected: %s (%d total)\n", conn.RemoteAddr(), h.clientCount())

	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		_ = conn.Close()
		fmt.Fprintf(os.Stderr, "[nitro] client disconnected: %s (%d total)\n", conn.RemoteAddr(), h.clientCount())
	}()

	conn.SetReadLimit(1 << 20)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-readDone:
			return
		case payload, ok := <-ch:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}
}

func (h *hub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func indexOfRHF2(data []byte, start int) int {
	for i := start; i+4 <= len(data); i++ {
		if data[i] == 'R' && data[i+1] == 'H' && data[i+2] == 'F' && data[i+3] == '2' {
			return i
		}
	}
	return -1
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}
