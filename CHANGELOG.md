# Changelog

## [v0.3.0] — 2026-04-29

> **质量强化版**：从能用到可靠。不加新用户功能，专注四个硬性 Gate。

### Gate 1 · 错误处理
- **cmd 层静默错误清零**：30 处 `_ = db.*()` 全量替换为 `warnIfErr(...)`，审计/公报事件不再静默丢失
- **Whip 三级恢复梯度**：Minister 异常不再直接 by-election（补选），改为 grace-period → scope-reduce → by-election 渐进恢复 (`internal/whip/liveness.go`)
- **配置启动校验**：`LoadConfig()` 启动时验证 duration/数值/必填字段 (`internal/config/config.go`)

### Gate 2 · 测试
- **Whip 核心路径**：覆盖率 12.9% → 63.9%，含 liveness/poller/report/scheduler/dispatch 全链路 (`internal/whip/*_test.go`)
- **Store 并发测试**：覆盖率 52.6% → 85.1%，含事务边界、并发写入、错误路径 (`internal/store/*_test.go`)
- **Runtime 命令构造**：覆盖率 16.5% → 90.6%，含 tmux 模拟 (`internal/runtime/summon_test.go`)
- **Chamber worktree**：覆盖率 28.1% → 93.8%，含创建/销毁/孤儿清理 (`internal/chamber/git_test.go`)
- **Util 纯函数**：覆盖率 30.9% → 94.1%，含 DAG/confirm/chart 边界输入 (`internal/util/*_test.go`)
- **总覆盖率**：29.8% → **60.3%**

### Gate 3 · Lint
- **gosec 安全扫描**：加入 CI 作为 blocking check，排除 G104/G204/G306/G202/G703
- **Linter 升级**：golangci-lint v1 → v2.11.4，CI 使用 `golangci-lint-action@v7`，Go 1.25 兼容
- **新增 linter**：gocritic、exhaustive、gochecknoinits、godot 全部通过

### Gate 4 · 日志
- **内部包 fmt.Print 清零**：`internal/speaker.Summon` 重构为返回 `SummonResult`，用户反馈迁回 `cmd/`
- **关键操作结构化日志**：bill 状态变更、minister 传召/解职、错误恢复全量覆盖
- **日志 level 可控**：`internal/logger/logger.go` ParseLevel 支持 debug/info/warn/error

### 其他
- **Minister 上下文健康监控**：Gazette 协议扩展 `context_health` 字段，Whip tick 时检测 token 使用率 (`internal/whip/context_health.go`)
- **Autoscale 全链路**：提取 `internal/minister/summon.go` 复用函数，whip autoscale 触发全链路 chamber 创建+分配+runtime 启动
- **Destructive 命令确认**：`session dissolve` / `minister dismiss` 强制确认
- **OTLP stub 标注**：doctor 检查中标注 `otlp` 为 stub 状态

---

## [v0.2.0] — 2026-03-15

### Phase 1–4 · 核心功能骨架

- **CLI 骨架**：完整 Cobra 命令集（bill/minister/session 管理）
- **Whip 守护进程**：tick 驱动的 Minister 心跳监控、自动补选
- **DAG 调度引擎**：拓扑排序、依赖解析、并行/流水线/树形拓扑
- **Gazette + ACK**：Agent 间异步消息协议，含 thread_id/reply_to
- **Committee 审查**：Bill 审查流程（committee → enacted）
- **Formula 扩展**：复杂度预测、健康检查
- **OpenTelemetry**：tracer/meter stub，nop exporter
- **Doctor 健康检查**：DB/git/tmux/claude/stuck-ministers/gazette-backlog/worktree-orhpan/config 10 项
- **Hansard 回放**：按时间线查看议事录
- **Webhook API**：GitHub push/pull_request 事件，HMAC-SHA256 验证
- **Graceful Shutdown**：whip + serve 信号处理

---

## [v0.1.0] — Initial

- Phase 0：CLI skeleton，项目初始化
