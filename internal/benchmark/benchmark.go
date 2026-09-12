package benchmark

import (
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
	lags            []time.Duration
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
	P50LagMS        float64 `json:"p50_lag_ms"`
	P95LagMS        float64 `json:"p95_lag_ms"`
	P99LagMS        float64 `json:"p99_lag_ms"`
	MaxLagMS        float64 `json:"max_lag_ms"`
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
	for _, name := range config.EndpointNames {
		states[name] = &endpointState{name: name, seen: newDeduper(config.MaxTracked)}
	}
	return &Runner{
		config: config, startedAt: startedAt, states: states,
		pending: make(map[eventKey]*pendingEvent), globalSeen: newDeduper(config.MaxTracked),
	}
}

func (r *Runner) Add(update feed.Update) {
	state := r.states[update.Endpoint]
	if state == nil {
		return
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
		r.addObservation(state, update)
	}
}

func (r *Runner) addObservation(state *endpointState, update feed.Update) {
	key := eventKey{sequence: update.Sequence, blockHash: update.BlockHash}
	if !state.seen.Add(key) {
		return
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
		event = &pendingEvent{arrivals: make(map[string]time.Time, len(r.states))}
		r.pending[key] = event
		r.pendingKeys = append(r.pendingKeys, key)
	}
	event.arrivals[state.name] = update.At
	if len(event.arrivals) == len(r.states) {
		r.finalize(event)
		delete(r.pending, key)
	}
	r.prune()
}

func (r *Runner) finalize(event *pendingEvent) {
	earliest := time.Time{}
	for _, arrival := range event.arrivals {
		if earliest.IsZero() || arrival.Before(earliest) {
			earliest = arrival
		}
	}
	for name, arrival := range event.arrivals {
		state := r.states[name]
		lag := arrival.Sub(earliest)
		state.matched++
		state.lags = append(state.lags, lag)
		if lag <= r.config.TieTolerance {
			state.wins++
		}
	}
	r.common++
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
		StartedAt:   r.startedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt:  now.UTC().Format(time.RFC3339Nano),
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
			item.MeanLagMS = mean(state.lags) / float64(time.Millisecond)
			item.P50LagMS = percentile(state.lags, 0.50) / float64(time.Millisecond)
			item.P95LagMS = percentile(state.lags, 0.95) / float64(time.Millisecond)
			item.P99LagMS = percentile(state.lags, 0.99) / float64(time.Millisecond)
			item.MaxLagMS = percentile(state.lags, 1) / float64(time.Millisecond)
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
		if left.Matched != right.Matched {
			return left.Matched > right.Matched
		}
		if left.P50LagMS != right.P50LagMS {
			return left.P50LagMS < right.P50LagMS
		}
		if left.P95LagMS != right.P95LagMS {
			return left.P95LagMS < right.P95LagMS
		}
		return left.Name < right.Name
	})
	for index := range report.Endpoints {
		report.Endpoints[index].Rank = index + 1
	}
	return report
}

func mean(values []time.Duration) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += float64(value)
	}
	return total / float64(len(values))
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
