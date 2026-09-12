package benchmark

import (
	"testing"
	"time"

	"github.com/0xfnzero/robinhood-feed-benchmark/internal/feed"
)

func TestRunnerMatchesAndRanksFeeds(t *testing.T) {
	start := time.Unix(100, 0)
	runner := New(Config{EndpointNames: []string{"Official", "Custom"}, TieTolerance: time.Millisecond, MaxTracked: 100}, start)
	for sequence := uint64(1); sequence <= 3; sequence++ {
		base := start.Add(time.Duration(sequence) * time.Second)
		runner.Add(observation("Official", sequence, base, base))
		runner.Add(observation("Custom", sequence, base.Add(2*time.Millisecond), base))
	}
	report := runner.Snapshot(start.Add(5 * time.Second))
	if report.UniqueEvents != 3 || report.CommonEvents != 3 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if report.Endpoints[0].Name != "Official" || report.Endpoints[0].Wins != 3 || report.Endpoints[0].P50LagMS != 0 {
		t.Fatalf("unexpected first endpoint: %+v", report.Endpoints[0])
	}
	if report.Endpoints[1].Name != "Custom" || report.Endpoints[1].Wins != 0 || report.Endpoints[1].P50LagMS != 2 {
		t.Fatalf("unexpected second endpoint: %+v", report.Endpoints[1])
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

func observation(name string, sequence uint64, received, feedTime time.Time) feed.Update {
	return feed.Update{
		Kind: feed.UpdateObservation, Endpoint: name, At: received,
		Sequence: sequence, BlockHash: "0x01", FeedTimestamp: feedTime, FrameBytes: 100,
	}
}
