package benchmark

import (
	"strings"
	"testing"
	"time"

	"github.com/0xfnzero/robinhood-feed-benchmark/internal/feed"
)

func TestRunnerMatchesAndRanksFeeds(t *testing.T) {
	start := time.Unix(100, 0)
	runner := New(Config{EndpointNames: []string{"Official", "Custom"}, TieTolerance: 0, MaxTracked: 100}, start)
	for sequence := uint64(1); sequence <= 3; sequence++ {
		base := start.Add(time.Duration(sequence) * time.Second)
		runner.Add(observation("Official", sequence, base, base))
		runner.Add(observation("Custom", sequence, base.Add(2*time.Millisecond), base))
	}
	report := runner.Snapshot(start.Add(5 * time.Second))
	if report.UniqueEvents != 3 || report.CommonEvents != 3 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if report.Endpoints[0].Name != "Official" || report.Endpoints[0].Wins != 3 || report.Endpoints[0].WinRatePct != 100 {
		t.Fatalf("unexpected first endpoint: %+v", report.Endpoints[0])
	}
	if report.Endpoints[0].BestLeadMS != 2 || report.Endpoints[0].P50LeadMS != 2 {
		t.Fatalf("unexpected lead stats: %+v", report.Endpoints[0])
	}
	if report.Endpoints[1].Name != "Custom" || report.Endpoints[1].Wins != 0 || report.Endpoints[1].P50LagMS != 2 {
		t.Fatalf("unexpected second endpoint: %+v", report.Endpoints[1])
	}
	if report.Endpoints[1].P90LagMS != 2 || report.Endpoints[1].MinLagMS != 2 {
		t.Fatalf("unexpected lag percentiles: %+v", report.Endpoints[1])
	}
	// Exclusive wins must sum to 100%.
	sumWins := report.Endpoints[0].Wins + report.Endpoints[1].Wins
	if sumWins != report.CommonEvents {
		t.Fatalf("wins should be exclusive: %d+%d != %d", report.Endpoints[0].Wins, report.Endpoints[1].Wins, report.CommonEvents)
	}
}

func TestRunnerDeduplicatesAndRequiresCommonEvents(t *testing.T) {
	start := time.Unix(100, 0)
	runner := New(Config{EndpointNames: []string{"A", "B"}, MaxTracked: 100}, start)
	update := observation("A", 1, start, start)
	runner.Add(update)
	runner.Add(update)
	report := runner.Snapshot(start.Add(time.Second))
	if report.UniqueEvents != 1 || report.CommonEvents != 0 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	for _, endpoint := range report.Endpoints {
		if endpoint.Name == "A" && endpoint.Observed != 1 {
			t.Fatalf("duplicate counted: %+v", endpoint)
		}
	}
}

func TestWriteMatchEventGrpcStyle(t *testing.T) {
	start := time.Unix(100, 0)
	match := &MatchEvent{
		Sequence: 42,
		Winner:   "Ours",
		Arrivals: []Arrival{
			{Name: "Ours", At: start, Lag: 0, First: true, Winner: "Ours"},
			{Name: "Peer", At: start.Add(12 * time.Millisecond), Lag: 12 * time.Millisecond, First: false, Winner: "Ours"},
			{Name: "Tiny", At: start.Add(5 * time.Microsecond), Lag: 5 * time.Microsecond, First: false, Winner: "Ours"},
		},
	}
	var buf strings.Builder
	WriteMatchEvent(&buf, match, 8)
	out := buf.String()
	if !strings.Contains(out, "Ours     接收 seq 42 tx 0: 首次接收") {
		t.Fatalf("missing first line: %q", out)
	}
	if !strings.Contains(out, "Peer     接收 seq 42 tx 0: 延迟  12.00ms (相对于 Ours)") {
		t.Fatalf("missing lag line: %q", out)
	}
	if strings.Contains(out, "Tiny") {
		t.Fatalf("sub-0.01ms lag should be omitted: %q", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
}

func observation(name string, sequence uint64, received, feedTime time.Time) feed.Update {
	return feed.Update{
		Kind: feed.UpdateObservation, Endpoint: name, At: received,
		Sequence: sequence, BlockHash: "0x01", FeedTimestamp: feedTime, FrameBytes: 100,
	}
}

func TestRunnerMatchesPerTxIndex(t *testing.T) {
	start := time.Unix(100, 0)
	runner := New(Config{EndpointNames: []string{"A", "B"}, MaxTracked: 100}, start)
	// Same seq, two tx indexes — must be independent race keys.
	runner.Add(observationTx("A", 10, 0, start, start))
	runner.Add(observationTx("B", 10, 0, start.Add(time.Millisecond), start))
	runner.Add(observationTx("B", 10, 1, start, start))
	runner.Add(observationTx("A", 10, 1, start.Add(2*time.Millisecond), start))
	report := runner.Snapshot(start.Add(time.Second))
	if report.UniqueEvents != 2 || report.CommonEvents != 2 {
		t.Fatalf("want 2 unique/common (seq,tx) keys, got %+v", report)
	}
	var a, b *EndpointReport
	for i := range report.Endpoints {
		if report.Endpoints[i].Name == "A" {
			a = &report.Endpoints[i]
		}
		if report.Endpoints[i].Name == "B" {
			b = &report.Endpoints[i]
		}
	}
	if a == nil || b == nil || a.Wins != 1 || b.Wins != 1 {
		t.Fatalf("want split wins, A=%+v B=%+v", a, b)
	}
}

func observationTx(name string, sequence uint64, tx uint32, received, feedTime time.Time) feed.Update {
	return feed.Update{
		Kind: feed.UpdateObservation, Endpoint: name, At: received,
		Sequence: sequence, TxIndex: tx, BlockHash: "0x01", FeedTimestamp: feedTime, FrameBytes: 100,
	}
}
