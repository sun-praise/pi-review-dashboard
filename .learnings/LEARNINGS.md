# Learnings

坑位记录：根因不显而易见、未来还会再犯的问题。格式 `[LRN-YYYYMMDD-NNN]`。

## [LRN-20260909-001] pitfall
**Logged**: 2026-09-09T00:00:00+08:00
**Priority**: medium
**Status**: active
**Area**: build

### Summary
`go mod tidy` 会静默把 `go.mod` 的 `go` 指令顶到依赖要求的最低版本，本机构建照样成功，掩盖版本不一致。

### Details
modernc.org/sqlite v1.58 要求 Go ≥ 1.25。本机工具链是 go1.22.5，但 Go 1.21+ 默认 `GOTOOLCHAIN=auto` 会自动下载 1.25 工具链，`go build` 完全正常——问题直到检查发现 Dockerfile 写着 `golang:1.22-bookworm`、README 写着 "Go 1.22+" 而 `go.mod` 已是 `go 1.25.0` 才暴露（不同环境首编要额外下载工具链，或直接失败）。

### Suggested Action
动过依赖后 diff `go.mod` 的 `go` 指令；CI 用 `go-version-file: go.mod` 跟随；Dockerfile 的 golang 镜像版本必须与 `go.mod` 对齐。

### Metadata
- Source: session_analysis
- Related Files: go.mod, Dockerfile, .github/workflows/ci.yml
- Tags: go, toolchain, docker

## [LRN-20260909-002] pitfall
**Logged**: 2026-09-09T00:00:00+08:00
**Priority**: high
**Status**: active
**Area**: build

### Summary
`main.go` 的 `go:embed all:web/dist` 要求 dist 目录存在且非空，否则 `go build` / `go test ./...` 直接失败。

### Details
`go test ./...` 会编译 main 包，embed 在编译期解析——CI 若把前端构建（npm run build 产出 dist）排在 Go 步骤之后，test 一步就挂。另外 dist 若被 .gitignore 忽略，裸克隆后构建同样失败。当前做法双保险：dist 提交入库 + CI 前端构建在前。

### Suggested Action
CI 步骤顺序固定：前端构建 → gofmt/vet/test/build；改动 .gitignore 时确认 `web/dist` 未被忽略。

### Metadata
- Source: session_analysis
- Related Files: main.go, .github/workflows/ci.yml, .gitignore
- Tags: go, embed, ci
