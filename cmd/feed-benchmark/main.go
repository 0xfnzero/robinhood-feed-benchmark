package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/0xfnzero/robinhood-feed-benchmark/internal/benchmark"
	"github.com/0xfnzero/robinhood-feed-benchmark/internal/feed"
)

const usageText = `Robinhood Feed benchmark

Usage:
  feed-benchmark [options]

The official Robinhood mainnet Feed is endpoint 1 by default. Add custom feeds
with repeated --feed NAME=URL flags or FEED_URL_N environment variables.

Examples:
  feed-benchmark --duration 30s
  feed-benchmark --feed MyFeed=wss://feed.example.com --duration 1m
  feed-benchmark --feed A=wss://a.example.com --feed B=wss://b.example.com
`

var version = "dev"

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type options struct {
	duration       time.Duration
	statusInterval time.Duration
	tieTolerance   time.Duration
	maxAge         time.Duration
	maxTracked     int
	format         string
	official       bool
	officialName   string
	officialURL    string
	perEvent       bool
	feedValues     stringList
	tokenValues    stringList
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Println("feed-benchmark", version)
		return nil
	}
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	endpoints, err := buildEndpoints(options)
	if err != nil {
		return err
	}

	baseContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(baseContext, options.duration)
	defer cancel()

	startedAt := time.Now()
	names := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		names = append(names, endpoint.Name)
	}
	runner := benchmark.New(benchmark.Config{EndpointNames: names, TieTolerance: options.tieTolerance, MaxTracked: options.maxTracked}, startedAt)
	updates := make(chan feed.Update, 4096)
	for _, endpoint := range endpoints {
		go feed.Run(ctx, endpoint, options.maxAge, updates)
	}

	if options.format != "json" {
		fmt.Fprintf(os.Stderr, "开始对比多个 Feed 服务性能...\n")
		fmt.Fprintf(os.Stderr, "测试持续时间: %s\n", options.duration)
		fmt.Fprintf(os.Stderr, "测试端点: %s (tie tolerance %s)\n", strings.Join(names, ", "), options.tieTolerance)
	}
	ticker := time.NewTicker(options.statusInterval)
	defer ticker.Stop()
	for {
		select {
		case update := <-updates:
			match := runner.Add(update)
			if options.format != "json" {
				writeStatus(os.Stderr, update)
				if options.perEvent && match != nil {
					benchmark.WriteMatchEvent(os.Stdout, match, runner.NameWidth())
					_ = os.Stdout.Sync()
				}
			}
		case now := <-ticker.C:
			if options.format != "json" {
				writeProgress(os.Stderr, runner.Snapshot(now), options.duration, startedAt)
			}
		case <-ctx.Done():
			for {
				select {
				case update := <-updates:
					match := runner.Add(update)
					if options.format != "json" && options.perEvent && match != nil {
						benchmark.WriteMatchEvent(os.Stdout, match, runner.NameWidth())
					}
				default:
					_ = os.Stdout.Sync()
					return benchmark.WriteReport(os.Stdout, runner.Snapshot(time.Now()), options.format)
				}
			}
		}
	}
}

func parseOptions(args []string) (options, error) {
	var value options
	flags := flag.NewFlagSet("feed-benchmark", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), usageText)
		flags.PrintDefaults()
	}
	flags.DurationVar(&value.duration, "duration", 30*time.Second, "benchmark duration")
	flags.DurationVar(&value.statusInterval, "status-interval", 5*time.Second, "progress output interval")
	flags.DurationVar(&value.tieTolerance, "tie-tolerance", 0, "soft-tie window; 0 = exclusive first (grpc-benchmark style)")
	flags.DurationVar(&value.maxAge, "max-age", 5*time.Second, "discard startup backlog older than this")
	flags.IntVar(&value.maxTracked, "max-tracked", 100_000, "maximum event IDs retained for matching and deduplication")
	flags.StringVar(&value.format, "format", "table", "output format: table or json")
	flags.BoolVar(&value.perEvent, "per-event", true, "print each matched event in grpc-benchmark style")
	flags.BoolVar(&value.official, "official", true, "include the official Feed as endpoint 1")
	flags.StringVar(&value.officialName, "official-name", "Official", "official Feed display name")
	flags.StringVar(&value.officialURL, "official-url", feed.OfficialMainnetURL, "official Feed WebSocket URL")
	flags.Var(&value.feedValues, "feed", "custom feed as NAME=URL; repeatable")
	flags.Var(&value.tokenValues, "feed-token", "custom feed token as NAME=TOKEN; prefer FEED_TOKEN_N")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if value.duration <= 0 || value.statusInterval <= 0 || value.maxAge <= 0 || value.tieTolerance < 0 || value.maxTracked < 100 {
		return options{}, errors.New("duration, status-interval, and max-age must be positive; tie-tolerance must be non-negative; max-tracked must be at least 100")
	}
	if value.format != "table" && value.format != "json" {
		return options{}, fmt.Errorf("unsupported format %q", value.format)
	}
	return value, nil
}

func buildEndpoints(options options) ([]feed.Endpoint, error) {
	tokens, err := parseAssignments(options.tokenValues, "feed-token")
	if err != nil {
		return nil, err
	}
	endpoints := make([]feed.Endpoint, 0, 1+len(options.feedValues))
	if options.official {
		endpoints = append(endpoints, feed.Endpoint{Name: strings.TrimSpace(options.officialName), URL: strings.TrimSpace(options.officialURL)})
	}
	for _, raw := range options.feedValues {
		name, address, err := assignment(raw, "feed")
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, feed.Endpoint{Name: name, URL: address, Token: tokens[name]})
	}
	environment, err := environmentEndpoints()
	if err != nil {
		return nil, err
	}
	endpoints = append(endpoints, environment...)
	if len(endpoints) == 0 {
		return nil, errors.New("at least one feed is required")
	}
	seen := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if err := feed.ValidateEndpoint(endpoint); err != nil {
			return nil, err
		}
		if _, exists := seen[endpoint.Name]; exists {
			return nil, fmt.Errorf("duplicate feed name %q", endpoint.Name)
		}
		seen[endpoint.Name] = struct{}{}
	}
	return endpoints, nil
}

func environmentEndpoints() ([]feed.Endpoint, error) {
	var endpoints []feed.Endpoint
	for index := 1; index <= 64; index++ {
		suffix := strconv.Itoa(index)
		address := strings.TrimSpace(os.Getenv("FEED_URL_" + suffix))
		if address == "" {
			continue
		}
		name := strings.TrimSpace(os.Getenv("FEED_NAME_" + suffix))
		if name == "" {
			name = "Feed_" + suffix
		}
		endpoints = append(endpoints, feed.Endpoint{Name: name, URL: address, Token: os.Getenv("FEED_TOKEN_" + suffix)})
	}
	return endpoints, nil
}

func parseAssignments(values []string, kind string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, raw := range values {
		name, value, err := assignment(raw, kind)
		if err != nil {
			return nil, err
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate %s name %q", kind, name)
		}
		result[name] = value
	}
	return result, nil
}

func assignment(raw, kind string) (string, string, error) {
	name, value, found := strings.Cut(raw, "=")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !found || name == "" || value == "" {
		return "", "", fmt.Errorf("%s must use NAME=VALUE", kind)
	}
	return name, value, nil
}

func writeStatus(writer *os.File, update feed.Update) {
	switch update.Kind {
	case feed.UpdateConnected:
		fmt.Fprintf(writer, "[%s] %-16s connected in %s\n", update.At.Format("15:04:05.000"), update.Endpoint, update.ConnectTime.Round(time.Millisecond))
	case feed.UpdateDisconnected:
		fmt.Fprintf(writer, "[%s] %-16s disconnected: %v\n", update.At.Format("15:04:05.000"), update.Endpoint, update.Err)
	}
}

func writeProgress(writer *os.File, report benchmark.Report, duration time.Duration, startedAt time.Time) {
	elapsed := time.Since(startedAt)
	remaining := duration - elapsed
	if remaining < 0 {
		remaining = 0
	}
	pct := 0.0
	if duration > 0 {
		pct = elapsed.Seconds() * 100 / duration.Seconds()
		if pct > 100 {
			pct = 100
		}
	}
	fmt.Fprintf(writer,
		"===== 测试进度: %.0f%% [%.0f/%.0f秒] - 剩余时间: %.0f秒 - 已对比 seq: %d =====\n",
		pct, elapsed.Seconds(), duration.Seconds(), remaining.Seconds(), report.CommonEvents)
}
