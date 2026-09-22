<div align="center">
  <h1>Robinhood Feed Benchmark</h1>
  <p><em>面向 Robinhood Chain 的多 Feed 首达延迟测速工具</em></p>
  <p>
    <a href="https://github.com/0xfnzero/robinhood-feed-benchmark/releases/latest"><img src="https://img.shields.io/github/v/release/0xfnzero/robinhood-feed-benchmark" alt="Release"></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go" alt="Go 1.22+"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
    <img src="https://img.shields.io/badge/platform-Linux%20%7C%20macOS-555" alt="Linux and macOS">
  </p>
  <p>
    <a href="README.md">English</a> |
    中文 |
    <a href="https://fnzero.dev/">Website</a> |
    <a href="https://t.me/fnzero_group">Telegram</a> |
    <a href="https://discord.gg/vuazbGkqQE">Discord</a>
  </p>
</div>

用 Go 实现的 Robinhood Chain Feed 延迟测速工具。它在同一个进程中并发连接多个
Nitro-compatible WebSocket Feed，以相同的 `sequenceNumber + blockHash` 匹配事件，
比较每个 Feed 的首达延迟。

工具只测试 Feed，不连接 RPC、不需要钱包，也不会签名或发送交易。

**关键词：** Robinhood Chain、Robinhood Feed、Nitro Feed、WebSocket 延迟、
区块链 Feed 测速、首达延迟、交易基础设施、Go benchmark。

## 普通用户安装

普通用户不需要安装 Go，也不需要编译源码。发布包支持：

- macOS Intel (`x86_64-apple-darwin`)
- macOS Apple Silicon (`aarch64-apple-darwin`)
- Linux x86-64 (`x86_64-unknown-linux-gnu`)
- Linux ARM64 (`aarch64-unknown-linux-gnu`)

Linux 服务器推荐直接克隆仓库：

```sh
git clone https://github.com/0xfnzero/robinhood-feed-benchmark.git
cd robinhood-feed-benchmark
cp .env.copy .env
# 在 .env 中配置要测的 Feed（厂商 / 自建 / 可选官方）
./run-feed-comparison.sh
```

脚本首次运行时会自动识别系统架构，从 GitHub Release 下载对应的预编译二进制、
验证 SHA-256，然后立即开始测速。二进制缓存在 `.runtime/`，后续执行无需重复下载。

从 GitHub Release 下载并执行安装脚本：

```sh
curl -fsSL -o install.sh https://github.com/0xfnzero/robinhood-feed-benchmark/releases/latest/download/install.sh
chmod +x install.sh
./install.sh
cd "$HOME/robinhood-feed-benchmark"
./run-feed-comparison.sh
```

独立安装方式适合不希望保留源码仓库的用户。安装脚本会自动识别操作系统和 CPU、
下载对应二进制并验证 SHA-256。请先在 `.env` 中配置要测的 Feed，再运行
`./run-feed-comparison.sh`。

```sh
cp .env.copy .env
# 填写 FEED_VENDOR_N（RHF2 / NitroFeed）+ FEED_URL_N
# 需要官方 Feed 时再设 FEED_INCLUDE_OFFICIAL=true
./run-feed-comparison.sh
```

## 功能

- 支持 Nitro JSON WebSocket 与 RHF2 二进制 TCP 流并发对比（自建 / 第三方 / 官方）。
- 官方主网 Feed 为可选：`wss://feed.mainnet.chain.robinhood.com`，通过
  `--official` 或 `FEED_INCLUDE_OFFICIAL=true` 开启。
- 实时显示连接状态、`seq` 数量和公共样本数量。
- 汇总覆盖率、互斥首达胜率（grpc-benchmark 风格）、完整延迟分位
  （P1/P5/P10/P25/P50/P75/P90/P95/P99）、最大领先、事件速率和断线次数。
- 过滤连接启动时的历史积压，断线后自动指数退避重连。
- 支持 Bearer / 自定义鉴权头，以及表格或 JSON 输出。
- 支持 `wss://` / `ws://`（NitroFeed）与 `tcp://`（RHF2）。

## 源码构建

这一节仅供开发者使用。需要 Go 1.22 或更高版本：

```sh
make build
```

二进制位于 `bin/feed-benchmark`。

## 快速开始

对比两个自定义 Feed：

```sh
./bin/feed-benchmark \
  --feed A=wss://a.example.com \
  --feed B=wss://b.example.com \
  --duration 30s
```

需要官方 Feed 时显式加上 `--official`：

```sh
./bin/feed-benchmark \
  --official \
  --feed MyFeed=wss://your-feed.example.com \
  --duration 1m
```

## 使用 `.env`

创建本地配置：

```sh
cp .env.copy .env
```

填写自己的 Feed 后运行：

```sh
./run-feed-comparison.sh
```

环境变量按数字递增。`FEED_VENDOR_N` 填线格式（不是品牌名）：

```text
FEED_VENDOR_1=RHF2
FEED_URL_1=tcp://0.0.0.0:19770

FEED_VENDOR_2=NitroFeed
FEED_NAME_2=Ours
FEED_URL_2=ws://127.0.0.1:9642/feed

# 需要官方 Feed 时再开启
# FEED_INCLUDE_OFFICIAL=true

FEED_COMPARISON_DURATION=45s
```

`RHF2` 监听二进制 TCP 推送（`tcp://host:port`）。
`NitroFeed` 连接 Nitro JSON WebSocket（`ws://` / `wss://`）。
凭证统一填 `FEED_AUTH_TOKEN_N`（兼容别名 `FEED_TOKEN_N`）；工具会根据
`FEED_URL` 的域名自动选择路径拼接、请求头或 Bearer。

把 RHF2 转成本地 Nitro WebSocket：

```text
FEED_VENDOR_1=RHF2
FEED_URL_1=tcp://0.0.0.0:19770
NITRO_FEED_PORT=9642
```

```sh
./run-rhf2-relay.sh
# 客户端: ws://127.0.0.1:9642/feed
```

真实 Token 只写在未提交的 `.env` 里；仓库里的 `.env.copy` / `.env.example` 只用占位符。
`./run-feed-comparison.sh` 会加载 `.env`，并在 `FEED_INCLUDE_OFFICIAL=true` 时自动加上 `--official`。

## 生成发布包

维护者可以一次生成四个平台的预编译包：

```sh
VERSION=v0.1.5 ./compile.sh
```

产物位于 `release/`：

```text
install.sh
robinhood-feed-benchmark-x86_64-apple-darwin.tar.gz
robinhood-feed-benchmark-aarch64-apple-darwin.tar.gz
robinhood-feed-benchmark-x86_64-unknown-linux-gnu.tar.gz
robinhood-feed-benchmark-aarch64-unknown-linux-gnu.tar.gz
SHA256SUMS
```

压缩包已经包含平台二进制、`run-feed-comparison.sh`、`.env.copy`、`.env.example`
和说明文档。
将这些文件作为 GitHub Release assets 上传后，普通用户即可使用上面的安装命令。

在发布前可以直接验证本地包：

```sh
cd release
./install.sh
```

安装器默认写入 `$HOME/robinhood-feed-benchmark`，也可以通过 `INSTALL_DIR` 指定目录。
升级已有的受管安装时，旧版本会移动到带时间戳的备份目录，原 `.env` 会复制到新版本。

## 输出说明

默认输出风格对齐 [grpc-benchmark](https://github.com/0xfnzero/grpc-benchmark)：逐笔首达/延迟 + 结束汇总。

### 逐笔事件（stdout，可用 `--per-event=false` 关闭）

```text
[11:18:30.706] LocalRBH 接收 seq 65325112: 首次接收
[11:18:31.169] Official 接收 seq 65325112: 延迟 462.18ms (相对于 LocalRBH)
[11:18:31.727] LocalRBH 接收 seq 65325122: 首次接收
[11:18:31.735] Official 接收 seq 65325122: 延迟   8.80ms (相对于 LocalRBH)
```

### 结束汇总

```text
📊 LocalRBH 性能分析
总接收数: 447 seqs
首先接收数: 425 (95.08%) seqs
落后接收数: 22 (4.92%) seqs
ℹ 延迟统计 (相对于最快端点):
  平均延迟: 12.13 ms
  P25 延迟: ...
  P50 延迟: ...
  P90 延迟: ...
  P99 延迟: ...
  最大延迟: ...
ℹ 领先幅度 (先到时相对第二名):
  P50 领先: ...
  最大领先: ...

🏆 端点性能对比
LocalRBH    : 首先接收  95.08%, 落后时平均延迟  12.13ms, 总体平均延迟   0.52ms, 最大领先  ...

RANK  FEED      STATUS  SEQS  RATE    COVERAGE  MATCHED  WIN RATE  P25 LAG  P50 LAG  ...  BEST LEAD
1     LocalRBH  live    447     9.9/s   100.00%   447      95.08%    ...
```

- `SEQS` / `MATCHED`：收到并去重后的 / 各端点都收到的 `sequenceNumber` 样本数。
- `COVERAGE`：该端点相对所有端点 seq 并集的覆盖率。
- `WIN RATE` / `首先接收`：每个公共 seq 只计一个最快端点（与 grpc-benchmark 一致；默认 `--tie-tolerance 0`）。
- `P25/P50/P75/P90/P99 LAG`：落后样本相对最快端点的延迟分位。
- `BEST LEAD`：先到时相对第二名的最大领先幅度。
- `FEED AGE`：本机接收时间减去 Feed 内的秒级时间戳，仅适合发现明显积压，不能替代相对延迟。

对齐键为 Feed 的 `sequenceNumber`（报告里缩写为 `seq`）。

两路或更多 Feed 必须在同一台机器、同一进程中比较，这样相对延迟使用 Go 的单调时钟，
不受机器时钟同步误差影响。只运行官方 Feed 时，吞吐和 Feed age 仍有效，但相对延迟恒为零。

## 常用参数

```text
--duration 30s          测试时长
--feed NAME=URL         增加自定义 Feed，可重复
--feed-token NAME=TOKEN 可选 Token
--feed-auth-header NAME=Header  自定义鉴权头（默认 Bearer）
--format table|json     输出格式
--per-event             逐笔打印首达/延迟（默认开启，grpc-benchmark 风格）
--tie-tolerance 0       软并列容差；默认 0=互斥首达（grpc-benchmark）
--max-age 5s            丢弃启动积压和过期消息
--status-interval 5s    实时进度间隔
--official              加入官方 Robinhood 主网 Feed（默认关闭）
```

优先用 `.env` 的线格式配置（`FEED_VENDOR_N=RHF2|NitroFeed` + `FEED_URL_N`）。

运行 `./bin/feed-benchmark --help` 查看全部参数。

## 测量边界

测速时间在完整 WebSocket frame 读取完成后立即记录，之后才解析 JSON。当前实现只校验
Feed envelope 的结构、版本、时间戳和 block hash，不做交易解码或 `signatureV2` 验证，
因此测量的是传输首达速度，而不是完整业务处理耗时。自定义 Feed 必须提供与 Robinhood
官方 Feed 相同的 Nitro envelope 格式。

## 验证

```sh
go test ./...
go vet ./...
```

## 社区与许可

- Website: [fnzero.dev](https://fnzero.dev/)
- Telegram: [t.me/fnzero_group](https://t.me/fnzero_group)
- Discord: [discord.gg/vuazbGkqQE](https://discord.gg/vuazbGkqQE)
- License: [MIT](LICENSE)
