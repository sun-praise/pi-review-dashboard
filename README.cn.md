# pi-review-dashboard

[English](README.md) | [中文](README.cn.md)

[pi-review-agent](https://github.com/sun-praise/pi-review-agent) 的跨仓库统计看板：review 次数、token 消耗
（input / output / cacheRead / cacheWrite）、成本、verdict 分布、缓存命中率，按仓库对比与按日趋势。

Go 后端 + Vite/TypeScript 前端，**一个静态二进制**同时服务 API 和页面（前端 `go:embed` 内嵌），
状态是**一个 SQLite 文件**——部署 = 拷贝二进制，备份 = 拷贝 `data/stats.db`。

![dashboard 预览（演示数据，`-seed 80` 生成）](docs/screenshot.png)

```
repo A (CI, 内网 runner) ─┐
repo B (CI)               ─┼─ POST /api/events ─→ Go + SQLite ─→ Web UI
本地 CLI 运行             ─┘    (幂等去重)                        (跨仓库对比/趋势/明细)
```

## 快速开始

```bash
# 1. 构建前端（产出 web/dist，随后被内嵌进二进制）
cd web && npm ci && npm run build && cd ..

# 2. 构建并运行（需要 Go 1.25+——modernc.org/sqlite v1.58 的要求；
#    Go 1.21+ 的 GOTOOLCHAIN=auto 也会自动下载对应工具链）
go build -o pi-review-dashboard .
./pi-review-dashboard                # http://localhost:8787

# 可选：灌 120 条演示数据先看看效果（写入 data/stats.db）
./pi-review-dashboard -seed 120
```

开发模式：终端 1 跑 `./pi-review-dashboard`，终端 2 在 `web/` 下 `npm run dev`，
Vite（:5173）会把 `/api` 代理到 Go（:8787）。

## 配置

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `PORT` | `8787` | 监听端口 |
| `STATS_DB` | `data/stats.db` | SQLite 路径 |
| `STATS_TOKEN` | 无 | 设置后 ingest 需要 `Authorization: Bearer <token>`；内网可不设 |

## agent 侧接入（每个仓库）

GitHub Actions 用 action inputs：

```yaml
- uses: sun-praise/pi-review-agent@v1
  with:
    team: "quality:1,security:1"
    stats-url: http://<内网地址>:8787/api/events
    stats-token: ${{ secrets.STATS_TOKEN }}   # dashboard 设了 STATS_TOKEN 才需要
```

本地 CLI：

```bash
PI_REVIEW_STATS_URL=http://<内网地址>:8787/api/events npx tsx src/index.ts --pr 12 ...
```

推送是 **fail-open** 的：dashboard 挂了不影响评审本身；事件同时会追加到
`<sessions-root>/stats.jsonl` 作为本地兜底。

## 事件协议

每次完成的评审 POST 一条 JSON（单对象或数组）：

```json
{
  "schema": 1,
  "ts": "2026-09-08T12:34:56.000Z",
  "platform": "github",
  "repository": "owner/repo",
  "pr": 123, "runId": "987654321", "attempt": 1,
  "mode": "team",
  "personas": [
    { "name": "quality", "input": 81000, "output": 1200, "cacheRead": 64000,
      "cacheWrite": 0, "cost": 0.0112, "resumed": true }
  ],
  "verdict": "CAN MERGE",
  "severity": { "decision": "CAN MERGE", "blocking": 0, "warning": 1, "fallback": false },
  "usage": { "input": 250000, "output": 4000, "cacheRead": 190000, "cacheWrite": 0 },
  "costTotal": 0.038, "durationMs": 47000
}
```

- **幂等**：`(platform, repository, runId, attempt)` 唯一——CI 重跑 / HTTP 重试只记一次；
- **字段只增不改**（`schema` 版本化），第三方可以放心解析；
- 缓存命中率 = `cacheRead / (input + cacheRead)`，衡量 session resume 实际省下的钱。
- 事件字段（repository、verdict 等）是不可信输入：前端渲染统一走 `esc()` 输出转义（`web/src/format.ts`）。ingest 未设 `STATS_TOKEN` 时这是唯一防线——新增任何 innerHTML 插值点必须经过它。

## Docker

```bash
docker build -t pi-review-dashboard .
docker run -d -p 8787:8787 -v /srv/pi-review:/data pi-review-dashboard
```

或 systemd：

```ini
[Unit]
Description=pi-review dashboard
After=network.target

[Service]
ExecStart=/opt/pi-review-dashboard/pi-review-dashboard
Environment=STATS_DB=/opt/pi-review-dashboard/data/stats.db
# Environment=STATS_TOKEN=change-me
WorkingDirectory=/opt/pi-review-dashboard
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## 开发

```bash
go test ./...     # 存储与 API 测试（含幂等/鉴权/聚合）
cd web && npm run typecheck
```

结构：`main.go`（入口/embed/SPA fallback）· `internal/store`（SQLite + 聚合查询 + seed）
· `internal/api`（ingest + dashboard API）· `web/`（Vite + TS + Chart.js 前端）。
