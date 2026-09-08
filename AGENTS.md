# 开发约定

与 [pi-review-agent](https://github.com/sun-praise/pi-review-agent) 的 AGENTS.md 保持同一套纪律，按本项目（Go 后端 + Vite/TS 前端）语境落地。

## 分支与 worktree

**IMPORTANT**: 不要在 `main` 分支上直接开发。所有改动（功能、修复、文档、CI）都在 `.worktrees/<branch>/` 下做，基于 `origin/main` 建分支，改完开 PR 合并。

```bash
git fetch origin --quiet
git worktree add .worktrees/<branch> origin/main -b <branch>
# ... 改代码、commit ...
git push -u origin <branch>
gh pr create ...
```

`.worktrees/` 已在 `.gitignore` 里。不要 commit 进去（会被 git 当 submodule，污染历史）。

## 发版与 tag

版本号格式 `vMAJOR.MINOR.PATCH`（semver）。每次发版：

1. commit `release: vX.Y.Z`，推 main
2. 打 annotated tag：`git tag -a vX.Y.Z -m "vX.Y.Z"` + `git push origin vX.Y.Z`
3. `.github/workflows/release.yml` 自动交叉编译三平台产物并发布 GitHub Release
4. **前移 major moving tag**（`v0` / `v1` ...）指向新 tag 同一个 commit：

```bash
git tag --force v0 v0.Y.Z
git push origin v0 --force
```

**Moving tag 规则**：
- `v0` 跟 v0.x.x 最新（v1 发布前用 v0 表示 pre-stable）。
- 发 v1.0.0 起 `v1` 跟 v1.x.x 最新；breaking（事件 schema 破坏性变更、API 不兼容）时**不动旧 major**，新建下一个 major。
- 用户引用 `@v1` = 自动跟最新 v1.x，引用 `@vX.Y.Z` = 精确锁定。

## Release notes

- **永远用 `--notes-file`**，不要 `--notes "inline"`。inline 模式下 heredoc / shell 会让 `` ` `` 和 `${{ }}` 产生转义残留。
- 改已发版本的 notes 用 `gh release edit vX.Y.Z --notes-file <file>`。

## 验证（CI 之外的本地验证）

改完代码、push 前：

1. `gofmt -l .` —— 输出必须为空
2. `go vet ./...` && `go test ./...`
3. 改了前端（`web/`）：`cd web && npm run build`，**并把 `web/dist/` 一起 commit**
   —— `go:embed` 依赖 dist；dist 不提交 = 裸克隆/CI 构建出旧前端或直接失败
4. 改了后端行为，本地过一发：
   ```bash
   cd web && npm run build && cd ..
   go run . -seed 20   # 灌演示数据
   go run .            # http://localhost:8787
   ```

## 项目结构

- `main.go` — 入口：env 配置（PORT/STATS_DB/STATS_TOKEN）、`-seed` 演示数据、go:embed 前端、SPA fallback、请求日志
- `internal/store/` — SQLite（modernc.org/sqlite，**纯 Go 无 CGO**）：schema、幂等 ingest、聚合查询、seed
- `internal/api/` — HTTP handlers：`POST /api/events`（Bearer 鉴权、单对象/数组）、`GET /api/dashboard`
- `web/` — Vite + TypeScript + Chart.js 前端；构建产物 `web/dist/` **提交入库**（embed 需要，见验证第 3 条）
- `.github/workflows/ci.yml` — 前端构建 → gofmt/vet/test → 内嵌二进制构建（顺序不可换，见 .learnings）
- `.github/workflows/release.yml` — `v*` tag → linux/amd64、linux/arm64、windows/amd64 → GitHub Release
- `Dockerfile` — 多阶段：node 构建前端 → golang 构建 → distroless/static 运行

## 数据与事件契约

- 事件 schema 与 pi-review-agent 的 `src/stats.ts` **一一对应**，`schema` 字段版本化：**字段只增不改**；需要改语义 = 破坏性变更 = 升 major
- 幂等键 `(platform, repository, run_id, attempt)`；`run_id` 缺失存 NULL（SQLite UNIQUE 视 NULL 互异，不误去重）
- ingest 对坏条目宽容：跳过并在响应里报告，不拒绝整批（`Invalid` 数组）
- 缓存命中率 = `cacheRead / (input + cacheRead)`，是核心指标，任何聚合改动不得丢失它

## 代码规则

Go：
- gofmt 标准；错误一律 `%w` 包装并带上下文（`fmt.Errorf("open store: %w", err)`）
- 测试用标准 `testing` + httptest；store 测试跑临时目录真 SQLite，不 mock 数据库
- 依赖上限：net/http 标准库 + modernc.org/sqlite。引新依赖前先问能不能用标准库做

web/（与 pi-review-agent 的 TS 规则一致）：
- 不用 `: any` / `as any`（用 `unknown` + 守卫）
- 顶层 `import type` 用于类型-only 依赖
- 静态 import，不用 `await import("literal")`
- 小静态字符串键查表用 `Record<K, V>`，不用 `Set`

## 经验沉淀（`.learnings/`）

**IMPORTANT**: 开发中遇到根因不显而易见、未来预计还会犯的坑，**必须记到 `.learnings/LEARNINGS.md`**。条目格式见现有条目（`[LRN-YYYYMMDD-NNN]` + Summary/Details/Suggested Action/Metadata）。

### 经验索引（按类别，排查时先 grep 关键词）

- **`go mod tidy` 静默顶高 go 指令** — modernc.org/sqlite v1.58 要求 Go ≥ 1.25，本机 GOTOOLCHAIN=auto 自动下载新工具链照样编过，掩盖了 Dockerfile/README 版本不一致。→ LRN-20260909-001
- **go:embed 需要 web/dist 先存在** — `go test ./...` 编译 main 包即触发 embed；CI 里前端构建必须排在 Go 步骤前，且 dist 提交入库兜底。→ LRN-20260909-002
