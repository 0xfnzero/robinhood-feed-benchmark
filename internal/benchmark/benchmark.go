package benchmark

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/0xfnzero/robinhood-feed-benchmark/internal/feed"
)

type Config struct {
	EndpointNames []string
	TieTolerance  time.Duration
	MaxTracked    int
}

type eventKey struct {
	sequence  uint64
	blockHash string
}

type pendingEvent struct {
	key      eventKey
	arrivals map[string]time.Time
}

type endpointState struct {
	name            string
	connected       bool
	connects        int
	disconnects     int
	connectTimes    []time.Duration
	observed        int
	frames          int
	bytes           int64
	matched         int
	wins            int
	lags            []time.Duration // lag vs earliest (includes 0 for winners)
	behindLags      []time.Duration // lag only when strictly behind
	winLeads        []time.Duration // lead margin when this endpoint is sole earliest
	totalLag        time.Duration
	behindLagTotal  time.Duration
	winLeadTotal    time.Duration
	feedAges        []time.Duration
	seen            *deduper
	lastError       string
	lastObservation time.Time
}

type Runner struct {
	config      Config
	startedAt   time.Time
	states      map[string]*endpointState
	pending     map[eventKey]*pendingEvent
	pendingKeys []eventKey
	globalSeen  *deduper
	unique      int
	common      int
	nameWidth   int
}

// Arrival is one endpoint's arrival for a matched common event.
type Arrival struct {
	Name    string
	At      time.Time
	Lag     time.Duration
	First   bool
	Winner  string // earliest endpoint name (may tie)
}

// MatchEvent is emitted when every endpoint has seen the same sequencer message.
type MatchEvent struct {
	Sequence  uint64
	BlockHash string
	Winner    string
	Arrivals  []Arrival
}

type EndpointReport struct {
	Rank            int     `json:"rank"`
	Name            string  `json:"name"`
	Connected       bool    `json:"connected"`
	Connections     int     `json:"connections"`
	Disconnects     int     `json:"disconnects"`
	Observed        int     `json:"observed"`
	EventsPerSec    float64 `json:"events_per_sec"`
	Frames          int     `json:"frames"`
	Bytes           int64   `json:"bytes"`
	Matched         int     `json:"matched"`
	CoveragePct     float64 `json:"coverage_pct"`
	Wins            int     `json:"wins"`
	WinRatePct      float64 `json:"win_rate_pct"`
	MeanLagMS       float64 `json:"mean_lag_ms"`
	BehindMeanLagMS float64 `json:"behind_mean_lag_ms"`
	P1LagMS         float64 `json:"p1_lag_ms"`
	P5LagMS         float64 `json:"p5_lag_ms"`
	P10LagMS        float64 `json:"p10_lag_ms"`
	P25LagMS        float64 `json:"p25_lag_ms"`
	P50LagMS        float64 `json:"p50_lag_ms"`
	P75LagMS        float64 `json:"p75_lag_ms"`
	P90LagMS        float64 `json:"p90_lag_ms"`
	P95LagMS        float64 `json:"p95_lag_ms"`
	P99LagMS        float64 `json:"p99_lag_ms"`
	MinLagMS        float64 `json:"min_lag_ms"`
	MaxLagMS        float64 `json:"max_lag_ms"`
	// Lead margins when this endpoint is the exclusive earliest vs #2.
	MeanLeadMS float64 `json:"mean_lead_ms"`
	P1LeadMS   float64 `json:"p1_lead_ms"`
	P5LeadMS   float64 `json:"p5_lead_ms"`
	P10LeadMS  float64 `json:"p10_lead_ms"`
	P25LeadMS  float64 `json:"p25_lead_ms"`
	P50LeadMS  float64 `json:"p50_lead_ms"`
	P75LeadMS  float64 `json:"p75_lead_ms"`
	P90LeadMS  float64 `json:"p90_lead_ms"`
	P95LeadMS  float64 `json:"p95_lead_ms"`
	P99LeadMS  float64 `json:"p99_lead_ms"`
	BestLeadMS float64 `json:"best_lead_ms"`
	MinLeadMS  float64 `json:"min_lead_ms"`
	MedianFeedAgeMS float64 `json:"median_feed_age_ms"`
	MedianConnectMS float64 `json:"median_connect_ms"`
	LastError       string  `json:"last_error,omitempty"`
	LastObservation string  `json:"last_observation,omitempty"`
}

type Report struct {
	StartedAt    string           `json:"started_at"`
	FinishedAt   string           `json:"finished_at"`
	DurationSec  float64          `json:"duration_sec"`
	UniqueEvents int              `json:"unique_events"`
	CommonEvents int              `json:"common_events"`
	Endpoints    []EndpointReport `json:"endpoints"`
}

func New(config Config, startedAt time.Time) *Runner {
	if config.MaxTracked < 1 {
		config.MaxTracked = 100_000
	}
	states := make(map[string]*endpointState, len(config.EndpointNames))
	nameWidth := 0
	for _, name := range config.EndpointNames {
		states[name] = &endpointState{name: name, seen: newDeduper(config.MaxTracked)}
		if len(name) > nameWidth {
			nameWidth = len(name)
		}
	}
	return &Runner{
		config: config, startedAt: startedAt, states: states,
		pending: make(map[eventKey]*pendingEvent), globalSeen: newDeduper(config.MaxTracked),
		nameWidth: nameWidth,
	}
}

func (r *Runner) NameWidth() int { return r.nameWidth }

// Add processes an update. When a common event is completed, returns the match details.
func (r *Runner) Add(update feed.Update) *MatchEvent {
	state := r.states[update.Endpoint]
	if state == nil {
		return nil
	}
	switch update.Kind {
	case feed.UpdateConnected:
		state.connected = true
		state.connects++
		state.connectTimes = append(state.connectTimes, update.ConnectTime)
	case feed.UpdateDisconnected:
		state.connected = false
		state.disconnects++
		if update.Err != nil {
			state.lastError = update.Err.Error()
		}
	case feed.UpdateObservation:
		return r.addObservation(state, update)
	}
	return nil
}

func (r *Runner) addObservation(state *endpointState, update feed.Update) *MatchEvent {
	key := eventKey{sequence: update.Sequence, blockHash: update.BlockHash}
	if !state.seen.Add(key) {
		return nil
	}
	state.observed++
	state.lastObservation = update.At
	state.feedAges = append(state.feedAges, update.At.Sub(update.FeedTimestamp))
	if update.FrameBytes > 0 {
		state.frames++
		state.bytes += int64(update.FrameBytes)
	}
	if r.globalSeen.Add(key) {
		r.unique++
	}

	event := r.pending[key]
	if event == nil {
		event = &pendingEvent{key: key, arrivals: make(map[string]time.Time, len(r.states))}
		r.pending[key] = event
		r.pendingKeys = append(r.pendingKeys, key)
	}
	event.arrivals[state.name] = update.At
	var match *MatchEvent
	if len(event.arrivals) == len(r.states) {
		match = r.finalize(event)
		delete(r.pending, key)
	}
	r.prune()
	return match
}

func (r *Runner) finalize(event *pendingEvent) *MatchEvent {
	// grpc-benchmark style: sort by arrival time, then name; exactly one first-receiver.
	type namedArrival struct {
		name string
		at   time.Time
	}
	ordered := make([]namedArrival, 0, len(event.arrivals))
	for name, at := range event.arrivals {
		ordered = append(ordered, namedArrival{name: name, at: at})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].at.Equal(ordered[j].at) {
			return ordered[i].at.Before(ordered[j].at)
		}
		return ordered[i].name < ordered[j].name
	})
	earliest := ordered[0].at
	winner := ordered[0].name
	var second time.Time
	if len(ordered) > 1 {
		second = ordered[1].at
	}

	arrivals := make([]Arrival, 0, len(ordered))
	for _, item := range ordered {
		state := r.states[item.name]
		lag := item.at.Sub(earliest)
		state.matched++
		state.lags = append(state.lags, lag)
		state.totalLag += lag

		// Default (tie-tolerance=0): exclusive first, matching grpc-benchmark.
		// Optional soft ties only when caller sets a positive tolerance.
		first := item.name == winner || (r.config.TieTolerance > 0 && lag <= r.config.TieTolerance)
		if first {
			state.wins++
			if item.name == winner && !second.IsZero() && second.After(earliest) {
				lead := second.Sub(earliest)
				if lead > 0 {
					state.winLeads = append(state.winLeads, lead)
					state.winLeadTotal += lead
				}
			}
		} else {
			state.behindLags = append(state.behindLags, lag)
			state.behindLagTotal += lag
		}
		arrivals = append(arrivals, Arrival{
			Name: item.name, At: item.at, Lag: lag, First: first, Winner: winner,
		})
	}
	r.common++
	return &MatchEvent{
		Sequence: event.key.sequence, BlockHash: event.key.blockHash,
		Winner: winner, Arrivals: arrivals,
	}
}

func (r *Runner) prune() {
	for len(r.pending) > r.config.MaxTracked && len(r.pendingKeys) > 0 {
		key := r.pendingKeys[0]
		r.pendingKeys = r.pendingKeys[1:]
		delete(r.pending, key)
	}
	if len(r.pendingKeys) <= r.config.MaxTracked*2 {
		return
	}
	compacted := make([]eventKey, 0, len(r.pending))
	for _, key := range r.pendingKeys {
		if _, exists := r.pending[key]; exists {
			compacted = append(compacted, key)
		}
	}
	r.pendingKeys = compacted
}

func (r *Runner) Snapshot(now time.Time) Report {
	report := Report{
		StartedAt: r.startedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt: now.UTC().Format(time.RFC3339Nano),
		DurationSec: now.Sub(r.startedAt).Seconds(), UniqueEvents: r.unique, CommonEvents: r.common,
	}
	for _, state := range r.states {
		item := EndpointReport{
			Name: state.name, Connected: state.connected, Connections: state.connects,
			Disconnects: state.disconnects, Observed: state.observed, Frames: state.frames,
			Bytes: state.bytes, Matched: state.matched, Wins: state.wins, LastError: state.lastError,
		}
		if report.DurationSec > 0 {
			item.EventsPerSec = float64(state.observed) / report.DurationSec
		}
		if r.unique > 0 {
			item.CoveragePct = float64(state.observed) * 100 / float64(r.unique)
		}
		if state.matched > 0 {
			item.WinRatePct = float64(state.wins) * 100 / float64(state.matched)
			item.MeanLagMS = float64(state.totalLag) / float64(state.matched) / float64(time.Millisecond)
		}
		// Lag percentiles mirror grpc-benchmark: only samples where this endpoint was behind.
		if len(state.behindLags) > 0 {
			item.BehindMeanLagMS = float64(state.behindLagTotal) / float64(len(state.behindLags)) / float64(time.Millisecond)
			item.MinLagMS = percentile(state.behindLags, 0) / float64(time.Millisecond)
			item.P1LagMS = percentile(state.behindLags, 0.01) / float64(time.Millisecond)
			item.P5LagMS = percentile(state.behindLags, 0.05) / float64(time.Millisecond)
			item.P10LagMS = percentile(state.behindLags, 0.10) / float64(time.Millisecond)
			item.P25LagMS = percentile(state.behindLags, 0.25) / float64(time.Millisecond)
			item.P50LagMS = percentile(state.behindLags, 0.50) / float64(time.Millisecond)
			item.P75LagMS = percentile(state.behindLags, 0.75) / float64(time.Millisecond)
			item.P90LagMS = percentile(state.behindLags, 0.90) / float64(time.Millisecond)
			item.P95LagMS = percentile(state.behindLags, 0.95) / float64(time.Millisecond)
			item.P99LagMS = percentile(state.behindLags, 0.99) / float64(time.Millisecond)
			item.MaxLagMS = percentile(state.behindLags, 1) / float64(time.Millisecond)
		}
		if len(state.winLeads) > 0 {
			item.MeanLeadMS = float64(state.winLeadTotal) / float64(len(state.winLeads)) / float64(time.Millisecond)
			item.MinLeadMS = percentile(state.winLeads, 0) / float64(time.Millisecond)
			item.P1LeadMS = percentile(state.winLeads, 0.01) / float64(time.Millisecond)
			item.P5LeadMS = percentile(state.winLeads, 0.05) / float64(time.Millisecond)
			item.P10LeadMS = percentile(state.winLeads, 0.10) / float64(time.Millisecond)
			item.P25LeadMS = percentile(state.winLeads, 0.25) / float64(time.Millisecond)
			item.P50LeadMS = percentile(state.winLeads, 0.50) / float64(time.Millisecond)
			item.P75LeadMS = percentile(state.winLeads, 0.75) / float64(time.Millisecond)
			item.P90LeadMS = percentile(state.winLeads, 0.90) / float64(time.Millisecond)
			item.P95LeadMS = percentile(state.winLeads, 0.95) / float64(time.Millisecond)
			item.P99LeadMS = percentile(state.winLeads, 0.99) / float64(time.Millisecond)
			item.BestLeadMS = percentile(state.winLeads, 1) / float64(time.Millisecond)
		}
		item.MedianFeedAgeMS = percentile(state.feedAges, 0.50) / float64(time.Millisecond)
		item.MedianConnectMS = percentile(state.connectTimes, 0.50) / float64(time.Millisecond)
		if !state.lastObservation.IsZero() {
			item.LastObservation = state.lastObservation.UTC().Format(time.RFC3339Nano)
		}
		report.Endpoints = append(report.Endpoints, item)
	}
	sort.Slice(report.Endpoints, func(i, j int) bool {
		left, right := report.Endpoints[i], report.Endpoints[j]
		if left.WinRatePct != right.WinRatePct {
			return left.WinRatePct > right.WinRatePct
		}
		if left.P50LagMS != right.P50LagMS {
			return left.P50LagMS < right.P50LagMS
		}
		return left.Name < right.Name
	})
	for index := range report.Endpoints {
		report.Endpoints[index].Rank = index + 1
	}
	return report
}

// WriteMatchEvent prints one common seq in grpc-benchmark style.
// Mirrors grpc_comparison.rs log_info lines:
//   [HH:MM:SS.mmm] Name 接收 seq N: 首次接收
//   [HH:MM:SS.mmm] Name 接收 seq N: 延迟   X.XXms (相对于 Winner)
// Uses one wall-clock timestamp for the whole match group (like grpc-benchmark),
// prints the exclusive first-arrival first, then lagging endpoints (lag >= 0.01ms).
func WriteMatchEvent(w io.Writer, match *MatchEvent, nameWidth int) {
	if match == nil || len(match.Arrivals) == 0 {
		return
	}
	if nameWidth < 8 {
		nameWidth = 8
	}
	ts := time.Now().Format("15:04:05.000")
	for _, arrival := range match.Arrivals {
		name := fmt.Sprintf("%-*s", nameWidth, arrival.Name)
		if arrival.First {
			fmt.Fprintf(w, "[%s] %s 接收 seq %d: 首次接收\n", ts, name, match.Sequence)
			continue
		}
		ms := float64(arrival.Lag) / float64(time.Millisecond)
		if ms < 0.01 {
			continue
		}
		fmt.Fprintf(w, "[%s] %s 接收 seq %d: 延迟 %6.2fms (相对于 %s)\n",
			ts, name, match.Sequence, ms, match.Winner)
	}
	if flusher, ok := w.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
}

func percentile(values []time.Duration, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(float64(len(ordered)-1)*quantile + 0.5)
	return float64(ordered[index])
}

type deduper struct {
	capacity int
	values   map[eventKey]struct{}
	order    []eventKey
	next     int
}

func newDeduper(capacity int) *deduper {
	return &deduper{capacity: capacity, values: make(map[eventKey]struct{}, capacity)}
}

func (d *deduper) Add(key eventKey) bool {
	if _, exists := d.values[key]; exists {
		return false
	}
	if len(d.order) < d.capacity {
		d.order = append(d.order, key)
	} else {
		delete(d.values, d.order[d.next])
		d.order[d.next] = key
		d.next = (d.next + 1) % d.capacity
	}
	d.values[key] = struct{}{}
	return true
}
