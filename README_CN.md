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
cp .env.example .env
# 可选：在 .env 中填写自己的 Feed；留空则只测试官方 Feed
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
下载对应二进制并验证 SHA-256。最后一条命令直接连接官方 Feed 测速。

对比自己的 Feed 时只需：

```sh
cp .env.example .env
# 在 .env 中填写 FEED_NAME_1、FEED_URL_1 和可选的 FEED_TOKEN_1
./run-feed-comparison.sh
```

## 功能

- 第一个测速端点默认是 Robinhood 官方主网 Feed：
  `wss://feed.mainnet.chain.robinhood.com`
- 支持任意数量的自定义 Feed 和 Bearer Token。
- 实时显示连接状态、事件数量和公共样本数量。
- 汇总覆盖率、首达胜率、P50/P95/P99 相对延迟、事件速率和断线次数。
- 过滤连接启动时的历史积压，断线后自动指数退避重连。
- 支持表格或 JSON 输出。
- 公网端点必须使用 `wss://`；`ws://` 只允许本机测试。

## 源码构建

这一节仅供开发者使用。需要 Go 1.22 或更高版本：

```sh
make build
```

二进制位于 `bin/feed-benchmark`。

## 快速开始

只测试官方 Feed：

```sh
./bin/feed-benchmark --duration 30s
```

官方 Feed 与一个自定义 Feed 对比：

```sh
./bin/feed-benchmark \
  --feed MyFeed=wss://your-feed.example.com \
  --duration 1m
```

重复 `--feed` 即可增加端点。官方 Feed 始终排在第一个，除非显式传入
`--official=false`。

## 使用 `.env`

创建本地配置：

```sh
cp .env.example .env
```

填写自己的 Feed 后运行：

```sh
./run-feed-comparison.sh
```

环境变量按数字递增：

```text
FEED_NAME_1=MyFeed
FEED_URL_1=wss://your-feed.example.com
FEED_TOKEN_1=your-token

FEED_NAME_2=AnotherFeed
FEED_URL_2=wss://another-feed.example.com
FEED_TOKEN_2=
```

Token 建议只放在未提交的 `.env` 或进程环境中，不要放进 URL 或命令行参数。

## 生成发布包

维护者可以一次生成四个平台的预编译包：

```sh
VERSION=v0.1.0 ./compile.sh
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

压缩包已经包含平台二进制、`run-feed-comparison.sh`、`.env.example` 和说明文档。
将这些文件作为 GitHub Release assets 上传后，普通用户即可使用上面的安装命令。

在发布前可以直接验证本地包：

```sh
cd release
./install.sh
```

安装器默认写入 `$HOME/robinhood-feed-benchmark`，也可以通过 `INSTALL_DIR` 指定目录。
升级已有的受管安装时，旧版本会移动到带时间戳的备份目录，原 `.env` 会复制到新版本。

## 输出说明

```text
RANK  FEED      STATUS  EVENTS  RATE    COVERAGE  MATCHED  WIN RATE  P50 LAG  P95 LAG  P99 LAG  FEED AGE  DISCONNECTS
1     Official  live    1000    33.3/s  100.00%   980      55.10%    0s       2.1ms    5.4ms    420ms     0
```

- `EVENTS`：该端点收到并去重后的实时 sequencer 消息数。
- `COVERAGE`：该端点相对所有端点事件并集的覆盖率。
- `MATCHED`：所有端点都收到的公共事件数；延迟只使用这些样本。
- `WIN RATE`：相对最快端点不超过 `--tie-tolerance` 的样本比例。
- `P50/P95/P99 LAG`：相对同一事件最早到达端点的延迟。
- `FEED AGE`：本机接收时间减去 Feed 内的秒级时间戳，仅适合发现明显积压，不能替代相对延迟。

两路或更多 Feed 必须在同一台机器、同一进程中比较，这样相对延迟使用 Go 的单调时钟，
不受机器时钟同步误差影响。只运行官方 Feed 时，吞吐和 Feed age 仍有效，但相对延迟恒为零。

## 常用参数

```text
--duration 30s          测试时长
--feed NAME=URL         增加自定义 Feed，可重复
--format table|json     输出格式
--tie-tolerance 1ms     计为并列首达的容差
--max-age 5s            丢弃启动积压和过期消息
--status-interval 5s    实时进度间隔
--official=false        不连接官方 Feed
```

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
