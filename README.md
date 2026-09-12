<div align="center">
  <h1>Robinhood Feed Benchmark</h1>
  <p><em>First-arrival latency benchmarking for Robinhood Chain Feeds</em></p>
  <p>
    <a href="https://github.com/0xfnzero/robinhood-feed-benchmark/releases/latest"><img src="https://img.shields.io/github/v/release/0xfnzero/robinhood-feed-benchmark" alt="Release"></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go" alt="Go 1.22+"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
    <img src="https://img.shields.io/badge/platform-Linux%20%7C%20macOS-555" alt="Linux and macOS">
  </p>
  <p>
    English |
    <a href="README_CN.md">中文</a> |
    <a href="https://fnzero.dev/">Website</a> |
    <a href="https://t.me/fnzero_group">Telegram</a> |
    <a href="https://discord.gg/vuazbGkqQE">Discord</a>
  </p>
</div>

A focused Go CLI for comparing Robinhood Chain Nitro-compatible WebSocket
Feeds. The official mainnet Feed is endpoint 1 by default. Add custom Feeds and
the tool matches identical `sequenceNumber + blockHash` events to report
first-arrival win rate, coverage, and P50/P95/P99 relative latency.

It only benchmarks Feed delivery. It does not use RPC, wallets, signing, or
transaction submission.

**Keywords:** Robinhood Chain, Robinhood Feed, Nitro Feed, WebSocket latency,
blockchain feed benchmark, first-arrival latency, trading infrastructure,
Golang benchmark.

## Features

- Uses the official Robinhood mainnet Feed as endpoint 1 by default:
  `wss://feed.mainnet.chain.robinhood.com`.
- Compares any number of custom Nitro-compatible Feeds concurrently.
- Matches identical events by `sequenceNumber + blockHash`.
- Reports coverage, first-arrival win rate, P50/P95/P99 lag, event rate, coarse
  Feed age, connection time, and disconnects.
- Filters startup backlog and reconnects with bounded exponential backoff.
- Supports optional Bearer tokens, table output, and machine-readable JSON.
- Requires `wss://` for public endpoints; plain `ws://` is limited to loopback.

## Install a prebuilt release

Users do not need Go or a source checkout. Prebuilt releases support Intel and
ARM64 on macOS and Linux.

The recommended Linux server workflow is:

```sh
git clone https://github.com/0xfnzero/robinhood-feed-benchmark.git
cd robinhood-feed-benchmark
cp .env.copy .env
# Optionally configure a custom Feed; leave it empty for the official Feed only.
./run-feed-comparison.sh
```

On first use, the script detects the platform, downloads the matching prebuilt
binary from GitHub Releases, verifies its SHA-256 checksum, and starts the
benchmark. The binary is cached in `.runtime/` for later runs.

```sh
curl -fsSL -o install.sh https://github.com/0xfnzero/robinhood-feed-benchmark/releases/latest/download/install.sh
chmod +x install.sh
./install.sh
cd "$HOME/robinhood-feed-benchmark"
./run-feed-comparison.sh
```

The installer detects the platform, downloads the matching binary, and verifies
its SHA-256 checksum. The final command immediately benchmarks the official
Feed with no configuration.

To compare a custom Feed:

```sh
cp .env.copy .env
# Set FEED_NAME_1, FEED_URL_1, and optionally FEED_TOKEN_1.
./run-feed-comparison.sh
```

## Build from source

This section is for developers. Go 1.22 or newer is required.

```sh
make build
./bin/feed-benchmark --duration 30s
```

Compare the official Feed with a custom Feed:

```sh
./bin/feed-benchmark \
  --feed MyFeed=wss://your-feed.example.com \
  --duration 1m
```

Repeat `--feed NAME=URL` to add more endpoints. Use `--official=false` to run
only custom endpoints.

## Environment configuration

```sh
cp .env.copy .env
# Edit .env, then:
./run-feed-comparison.sh
```

Indexed environment variables are supported:

```text
FEED_NAME_1=MyFeed
FEED_URL_1=wss://your-feed.example.com
FEED_TOKEN_1=your-token
```

Keep tokens in the untracked `.env` file or process environment rather than in
URLs or command-line arguments.

Add more Feeds by incrementing the suffix:

```text
FEED_NAME_2=AnotherFeed
FEED_URL_2=wss://another-feed.example.com
FEED_TOKEN_2=
```

The official Feed remains endpoint 1 unless `--official=false` is supplied.

## Build release packages

Maintainers can cross-compile all four supported platforms in one command:

```sh
VERSION=v0.1.1 ./compile.sh
```

`release/` will contain `install.sh`, four platform archives, and
`SHA256SUMS`. Each archive contains the binary, run script, configuration
template, and documentation. Upload those files as GitHub Release assets to
enable the installation command above. No GitHub Actions workflow is required.

## Measurement model

The receive timestamp is captured immediately after a complete WebSocket frame
is read and before JSON decoding. Relative latency is calculated inside one
process with Go's monotonic clock, so custom Feeds should be compared from the
same machine. `FEED AGE` uses the Feed's whole-second timestamp and is only a
coarse backlog signal.

The tool structurally validates the Nitro envelope, version, timestamp, and
block hash. It intentionally does not decode transactions or verify
`signatureV2`, keeping the measurement focused on transport arrival. Custom
Feeds must use the same envelope format as the official Robinhood Feed.

Run `./bin/feed-benchmark --help` for all options. Table and JSON output are
available with `--format`.

## Result fields

```text
RANK  FEED      STATUS  EVENTS  RATE    COVERAGE  MATCHED  WIN RATE  P50 LAG  P95 LAG  P99 LAG  FEED AGE  DISCONNECTS
1     Official  live    1000    33.3/s  100.00%   980      55.10%    0s       2.1ms    5.4ms    420ms     0
```

- `COVERAGE` is the endpoint's share of the union of observed events.
- `MATCHED` counts events received by every configured endpoint; latency uses
  only these common samples.
- `WIN RATE` counts arrivals within `--tie-tolerance` of the fastest endpoint.
- `P50/P95/P99 LAG` measures relative delay from the first endpoint to receive
  the same event.
- `FEED AGE` uses the whole-second timestamp carried by the Feed and is only a
  coarse backlog indicator.

For valid relative results, connect every Feed from the same Linux server and
process. Go's monotonic clock then avoids cross-machine clock synchronization
error. With only the official Feed, throughput and Feed age remain useful but
relative lag is always zero.

## Common options

```text
--duration 30s          Benchmark duration
--feed NAME=URL         Add a custom Feed; repeatable
--format table|json     Output format
--tie-tolerance 1ms     First-arrival tie threshold
--max-age 5s            Discard stale startup messages
--status-interval 5s    Live progress interval
--official=false        Exclude the official Feed
```

## Verify

```sh
go test ./...
go vet ./...
```

## Community and license

- Website: [fnzero.dev](https://fnzero.dev/)
- Telegram: [t.me/fnzero_group](https://t.me/fnzero_group)
- Discord: [discord.gg/vuazbGkqQE](https://discord.gg/vuazbGkqQE)
- License: [MIT](LICENSE)
