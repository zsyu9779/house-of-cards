# Agent Context Archive

## 2026-04-06 — v0.3 Phase 1 PR-2 完成记录

### PR-2: E-1.1 Whip 错误治理 + 恢复梯度
- Store 层：Minister struct 新增 `RecoveryAttempts`，migrate 新增 ALTER TABLE
- 所有 7 个 minister SELECT/Scan 更新为包含 `COALESCE(recovery_attempts,0)`
- 新增 `IncrementRecoveryAttempts()` 和 `ResetRecoveryAttempts()` 方法
- liveness.go：三级恢复梯度（checkpoint → at-risk → by-election）
- poller.go：16 处错误修复（关键路径 return，辅助路径 slog.Warn，文件清理 best-effort）
- scheduler.go：14 处错误修复（UpdateSessionStatus → critical+return）
- dispatch.go：best-effort 注释
- 验证：golangci-lint 零 error，go build/vet/test -race 全通过
- `grep '_ = w.db.' internal/whip/` = 0 匹配

---

## 2026-03-01 — 深度评估 + 巡检修复 + v0.2 草案

### 深度评估 + 文档归档
- 对 v0.1 做全量深度分析（设计文档、所有 Phase 历史、核心代码、测试覆盖率）
- 将 `docs/` 下所有 v0.1 相关文档归档至 `docs/v0.1/`（含 research-reports/）
- 新建 `docs/v0.2-draft.md`：现状评估矩阵、三个方向（A 骨架补完/B 信息质量/C 可靠性）

### 巡检修复（6 任务全部 PASS）
- privy 纯函数测试 + git 集成测试（覆盖率 12.4% → 56.5%）
- runtime factory + 实现类测试（覆盖率 6.3% → 16.5%）
- config 测试（覆盖率 0% → 41.8%）
- whip advanceSession 集成测试（覆盖率 2.6% → 12.9%）
- Makefile CI 检查
- 总覆盖率：31.6% → 40.3%

---

## 2026-03-01 — Phase 5 实施完成

### Phase 5A — hoc session stats 命令
- `internal/store/store.go`：新增 `SessionStats`, `MinisterLoad` 结构体，`GetSessionStats()`, `GetAllSessionStats()` 方法
- `cmd/session.go`：新增 `sessionStatsCmd`（`hoc session stats [id] [--all]`），`printSessionStats()`, `showSessionStats()`, `showAllSessionStats()`
- `internal/store/store_test.go`：新增 `TestGetSessionStats_Empty`, `TestGetSessionStats_WithData`

### Phase 5B — E2E 集成测试
- `cmd/integration_test.go`：新增 4 个测试：`TestFullSessionLifecycle_Pipeline`, `TestByElectionRecovery`, `TestHansardQualityScoring`, `TestSessionStatsIntegration`
- `internal/whip/whip_test.go`：新增 2 个测试：`TestBillIsReady_ChainDependencies`, `TestBillIsReady_RoyalAssentCountsAsDone`

### Phase 5C — Bill 复杂度预测
- `internal/util/complexity.go`：新建，`EstimateBillComplexity()`, `ComplexityIcon()`, 关键词启发式规则
- `internal/util/complexity_test.go`：新建，关键词测试 + 置信度边界测试
- `cmd/bill.go`：`hoc bill show` 追加「复杂度预测」行，引入 `util` 包

### Phase 5D — README.md
- `README.md`：新建，含术语速查、快速上手、CLI 命令参考、ASCII 架构图、质量评分说明

测试结果：
```
go test ./... — 全部 PASS ✅  (含 Phase 5 新增测试)
go vet ./...  ✅
go build ./... ✅
```

---

## 2026-03-01 — Phase 4 实施完成

### Phase 4A — Hansard 质量评分
- `internal/store/store.go`: `ComputeBillQuality()`, `UpdateHansardQuality()`, `GetMinisterAvgQuality()`, `GetMinisterAvgQualityForSkill()`
- `EnactBillFromDone()` 现在自动计算并存储 quality 分数
- `pollReviewFile()` 委员会审查结果写入 quality

### Phase 4B — 质量加权部长选择
- `internal/store/store.go`: `FindBestMinisterForSkill()` — 按 quality × (1/load+1) 排名
- `internal/whip/whip.go`: `advanceSession()` 改用 `FindBestMinisterForSkill`

### Phase 4C — Pipeline Gazette 注入
- `internal/whip/whip.go`: `buildUpstreamGazetteSection()` — 收集上游议案完成公报
- `autoAssign()` 调用后追加 "上游议案公报" 到交接摘要（pipeline 拓扑语义）

### Phase 4D — hoc hansard score 命令
- `cmd/hansard.go`: `hansardScoreCmd` + `showHansardScore()` + `buildQualityBar()`
- 按质量排名展示所有部长，含成功率 + 质量分 + 说明

### 测试
- `TestComputeBillQuality`, `TestGetMinisterAvgQuality`, `TestFindBestMinisterForSkill` — 全 PASS
- `go test ./...` `go vet ./...` 全通过

---

## 2026-03-01 — Phase 3C/3D 实施完成

### Phase 3C — OpenTelemetry 可观测性
- `internal/otel/tracer.go`: Provider/Tracer/Span 抽象，支持 stdout/otlp/nop 三种模式
- `internal/otel/metrics.go`: Counter/Histogram/MetricRegistry，NewRegistryForTest 供测试使用
- `internal/otel/exporter.go`: InitFromConfig/InitFromConfigWithWriter
- `internal/otel/otel_test.go`: 6 个测试全部 PASS
- `internal/config/config.go`: 新增 ObservabilityConfig（Exporter/OTLPEndpoint/ServiceName）
- `cmd/root.go`: initLogging 里初始化全局 OTEL provider
- **关键路径埋点**:
  - `whip.go`: tick() → "whip.tick" span，byElection() → "whip.by_election" span + hoc_by_election_total counter
  - `privy.go`: MergeSession() → "privy.merge" span + hoc_conflicts_total counter
  - `claudecode.go`: Summon() → "minister.summon" span + hoc_ministers_active_total counter

### Phase 3D — Formula 扩展系统
- `internal/formula/types.go`: Formula/Step/Action/RunResult/Registry 类型
- `internal/formula/parser.go`: LoadFromFile/LoadDirectory/LoadRegistryFromDirs（TOML 解析）
- `internal/formula/executor.go`: Execute/runStep/runAction（支持 git/shell/hoc/notify 四种 action type，模板变量插值，dry-run 模式，并行步骤）
- `internal/formula/builtins.go`: 5 个内置 Formula（cleanup-chambers/auto-merge/sync-main/health-check/archive-session）
- `internal/formula/formula_test.go`: 7 个测试全部 PASS
- `cmd/formula.go`: hoc formula list/apply/status 三个子命令
- `cmd/root.go`: 注册 formulaCmd

---

## 2026-03-01 — Phase 3A/3B/3E 实施完成

### Phase 3A — 智能冲突解决
- `privy.go`: ConflictInfo 结构体、策略链（merge → -X theirs → -X ours）、AnalyzeBranch、FormatConflictGazette
- `cmd/privy.go`: `hoc privy analyze <branch>` 干跑预测命令

### Phase 3B — Hook 队列与空闲池化
- `store.go`: ministers.hook 列、PushHook/PopHook/PeekHook、FindLeastLoadedMinister
- `whip.go`: pollIdleMinisterReassign（idle 自动接单）、orderPaper 改为负载均衡
- `cmd/ministers.go`: `hoc minister hook push/list/pop`

### Phase 3E — 体验优化
- `cmd/root.go`: 默认日志 INFO + --quiet flag
- `cmd/cabinet.go`: `--max-per-minister N` + findBestMinisterWithLimit（负载均衡）
- `cmd/session.go`: `hoc session migrate` 迁移旧会期 project 字段

### 测试：全 PASS（含 TestHookQueuePushPopPeek、TestFindLeastLoadedMinister）

---

## 2026-03-01 — Phase 10 平台化扩展（全部完成）

### 改动摘要
- **10-1** `internal/store/store.go`：Session 新增 Projects（JSON 数组），Bill 新增 Project；`cmd/session.go`：--projects flag（逗号分隔），billSpec 新增 project 字段
- **10-2** `cmd/serve.go`：新建 API Server（HTTP），端点 /api/v1/sessions, /api/v1/ministers, /api/v1/bills, /api/v1/gazettes, /api/v1/webhooks
- **10-3** `internal/whip/whip.go`：新增 autoscale() 函数（pending bills > idle*2 → 扩容，idle > pending+2 → 缩容）
- **10-4** `internal/config/config.go`：新增 ConfigWatcher + HotReloadableParams；新增 fsnotify 依赖；`cmd/config_cmd.go`：config show/reload 子命令

### 验收
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（全通过）

---

## 2026-03-01 — Phase 9 Speaker AI 增强（全部完成）

### 改动摘要
- **9-1** `internal/store/store.go`：新增 `ListByElectionHansard(limit)` 方法；`internal/speaker/speaker.go`：`GenerateContext()` 增强，新增资源利用率、拓扑推荐、近期补选记录、推荐行动章节
- **9-2** `internal/speaker/speaker.go`：新增 `ParseDecision()` 解析 `[DIRECTIVE]` 指令，`RunPatrol()` 非交互式运行；`cmd/speaker.go`：新增 `patrol` 子命令（--interval/--once flag），`execDecision()` 执行 assign/by-election/escalate 指令
- **9-3** `internal/speaker/speaker.go`：新增 `SelectTopology(bills)` 函数——无依赖→parallel，线性链→pipeline，多依赖/汇合→tree
- **9-4** `internal/speaker/speaker.go`：新增 `Decision` 结构体；`cmd/speaker.go`：新增 `council` 子命令（实验性），输出竞标结果到 `.hoc/council-<timestamp>.md`

### 验收
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（全通过）

---

## 2026-03-01 — Phase 8 自动化闭环（全部完成）

### 改动摘要
- **8-1** `internal/store/store.go`：新增 `EnactBillFromDone(billID, ministerID, summary string) error`（enacted + hansard + gazette）；`internal/whip/whip.go`：新增 `pollDoneFiles()`（检测 `<chamber>/.hoc/bill-<id>.done`，删除后自动 enacted）
- **8-2** `internal/store/store.go`：新增 `ListBillsForCommittee()`、`UnassignBill(billID string)`；`internal/whip/whip.go`：新增 `committeeAutomation()`（识别 committee bills → 派给 reviewer）、`pollReviewFile(bill)`（检测 `.review` 文件，PASS → enacted，FAIL → draft 重派）
- **8-3** `internal/whip/whip.go`：增强 `gazetteDispatch()` + 新增 `deliverGazette(g)`：tmux send-keys 优先，fallback 写 `<chamber>/.hoc/inbox/<gazette-id>.md`
- **8-4** `cmd/ministers.go`：新增 `ministerAutoCmd`（`hoc minister auto`，--session/--max-concurrent/--project/--no-tmux flag），新增 `autoIterate()`（监控已分配未启动议案）、`doSummon()`（提取公共传召逻辑）；`buildBillBrief` 追加第5步 done 文件写入约定

### 验收
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（全通过）

---

## 2026-03-01 — Phase 7 可视化与监控增强（全部完成）

### 改动摘要
- 新建 `internal/util/dag.go`：DAGItem/DAGNode 结构体，BuildDAG/RenderDAG 函数，ParseDepsJSON 工具
- 新建 `internal/util/chart.go`：BarItem 结构体，RenderBarChart ASCII 条形图渲染
- `cmd/session.go`：session status 支持可选 session-id 参数，附带 ASCII DAG 渲染；提取 encodeSessionsJSON/showSessionDetail/splitLines 函数
- `cmd/floor.go`：完整重写；增加 --interval flag，viewMode 切换（main/gazette/session），g/s/esc 快捷键，公报流显示最近 5 条（含已读），session 详情视图显示各 bill 状态颜色编码
- `cmd/hansard.go`：新增 trend 子命令，调用 RenderBarChart 展示各部长成功率
- `internal/store/store.go`：新增 WhipStats 结构体、GetWhipStats()、ListRecentHansard(N) 方法
- `internal/whip/whip.go`：Report() 函数签名改为 Report(db, showHistory bool)，增加历史统计/--history 事件日志，新增 fmtSeconds 辅助函数
- `cmd/whip.go`：新增 --history flag，透传给 whip.Report
- `cmd/format.go`：补充 repeat() helper（被 doctor.go 使用，floor.go 重写时丢失）
- `cmd/integration_test.go`：更新 whip.Report 调用为 2 参数签名

## 2026-02-28 — Phase 6B 代码工程化（全部完成）

### 改动摘要

- **6B-1** `internal/util/format.go`：提取 `Truncate/OrDash`；`cmd/format.go` 提供包内薄包装
- **6B-2** `internal/store/store.go`：`migrate()` 追加 5 个 `CREATE INDEX IF NOT EXISTS`
- **6B-3** `cmd/integration_test.go`：6 个集成测试（单 minister 流程、生命周期、Whip report、--json 输出）
- **6B-4** `cmd/root.go`：`--verbose/-v` flag + slog 初始化（WARN→DEBUG）；`internal/whip/whip.go` 迁移至 slog；所有 cmd 文件 `fmt.Fprintf(os.Stderr)` 迁移至 `slog.Warn`
- **6B-5** `bill list --json`、`minister list --json`、`session status --json`：使用 `encoding/json` 输出标准 JSON

### 验收
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（全通过）

---

## 2026-02-28 — Phase 2 路线图 + Gas Town 对比分析

### 改动摘要
1. **`docs/03-phase2-roadmap.md`** — 新建：完整 Phase 2 开发路线图，含 5 个子阶段（6B/7/8/9/10）
2. **`docs/04-gas-town-comparison.md`** — 新建：Gas Town 对比分析与优化建议

### 文档内容
- Phase 6B：代码工程化（util/format 提取、DB 索引、集成测试、slog、--json）
- Phase 7：可视化增强（DAG 渲染、TUI Gazette 流、hansard trend、whip 历史统计）
- Phase 8：自动化闭环（done 文件检测、committee 自动化、Gazette 投递、minister auto）
- Phase 9：Speaker AI 增强（context 优化、patrol、拓扑选择、多 Speaker 竞标原型）
- Phase 10：平台化扩展（多项目 session、API Server、动态扩缩容、配置热更新）

### Gas Town 对比分析要点
- 已有优势：政府隐喻、Gazette 协议、SQLite 零依赖、可选 tmux
- 可借鉴机制：Hook 队列、done 文件自动检测、OpenTelemetry（长期）
- 需改进：Minister 连续承接、冲突解决智能化

### 验证
- Markdown 格式正确，无语法错误 ✅

---

## 2026-02-28 — Phase 5 自动化闭环 + TUI 升级

### 改动摘要
1. **`internal/store/store.go`** — Session 新增 `project` 字段；idempotent migration；新增 `UpdateSessionProject`、`ListBillsWithBranchBySession`；所有 Session 查询包含 project 列
2. **`cmd/session.go`** — `sessionMeta` 新增 `project` 字段；`session open` 新增 `--project` flag；创建 session 时传入 project；`session status` 展示项目名
3. **`internal/whip/whip.go`** — 导入 `internal/privy`；`advanceSession()` 改为调用 `privyAutoMerge()`；新增 `privyAutoMerge()`：并行议案全部完成后自动合并 → 成功时 royal_assent + 完成 Gazette，冲突时 Conflict Gazette
4. **`cmd/doctor.go`** — 新建：健康检查命令；检查 DB 连接、git/tmux/claude CLI 可用性、卡住部长、未读公报积压
5. **`cmd/root.go`** — 注册 doctorCmd
6. **`cmd/floor.go`** — 完全重写为 BubbleTea TUI：Model/Init/Update/View；lipgloss 样式；q/r 键绑定；3s 自动 tick；alt-screen 模式

### 依赖新增
- `github.com/charmbracelet/bubbletea v1.3.10`
- `github.com/charmbracelet/lipgloss v1.1.0`

### 验证结果
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（全部通过）



## 2026-02-28 — Phase 4 完善体验

### 改动摘要
1. **`cmd/cabinet.go`** — 完整实现 `cabinet list`（花名册+成功率）, `cabinet reshuffle`（预演/执行按技能匹配派发）
2. **`cmd/bill.go`** — 新增 `bill committee`（reading→committee）, `bill review --pass/--fail`（committee→enacted/reading + Hansard + Review Gazette）
3. **`internal/privy/privy.go`** — 新建：`MergeSession`(git merge 并行分支)、`MainRepoFromWorktree`、`MainRepoPath`
4. **`cmd/privy.go`** — 新建：`hoc privy merge <session-id> --project [--base] [--dry-run]`
5. **`cmd/floor.go`** — 完整实现：清屏刷新实时仪表板，展示内阁/会期进度条/未读公报
6. **`cmd/root.go`** — 注册 `privyCmd`

### 验证结果
- `go build ./...` ✅
- `hoc cabinet --help` / `hoc privy --help` / `hoc bill committee --help` ✅
- `hoc floor --help` ✅

## 2026-02-28 — Phase 3 崩溃恢复 + Speaker AI + Hansard

### 改动摘要
1. **`internal/store/store.go`** — 新增 `ListHansard`, `ListHansardByMinister`, `GetBillsByAssignee`, `ClearBillAssignment`, `HansardSuccessRate`, `ListMinistersWithStatus`
2. **`internal/speaker/speaker.go`** — 新建 Speaker 包：`GenerateContext`（5 节内容：政府现状/会期进度/内阁档案/近期公报）, `WriteContext`, `Summon`（tmux/前台）
3. **`internal/whip/whip.go`** — 重构：新增 `hocDir` 字段，`byElection()`（git stash+Handoff Gazette+Hansard+ClearBillAssignment+offline），`hansardUpdate` 每 60s 刷新 context.md
4. **`cmd/hansard.go`** — 完整实现：`hansard list`（全量）+ `hansard [minister-id]`（含成功率）
5. **`cmd/bill.go`** — 新增 `bill enacted`：UpdateBillStatus+CreateHansard+CreateGazette(completion)
6. **`cmd/ministers.go`** — 新增 `minister by-election`：git stash+Handoff Gazette+Hansard+ClearBillAssignment+offline
7. **`cmd/speaker.go`** — 新建：`speaker summon`（刷新 context.md+启动 Claude）, `speaker context [--refresh]`
8. **`cmd/root.go`** — 注册 speakerCmd

### 验证结果
- `hoc bill enacted` → Hansard 写入 + completion Gazette ✅
- `hoc hansard go-minister` → 成功率、履历展示 ✅
- `hoc minister by-election` → 完整补选流程 ✅
- `hoc speaker context --refresh` → context.md 生成 ✅

## 2026-02-28 — Phase 2 并行 + Whip

### 改动摘要
1. **`internal/store/store.go`** — 新增 `portfolio` 列（幂等 ALTER TABLE）+ 5 个新方法：`ListWorkingMinisters`, `ListIdleMinistersForSkill`, `ListActiveSessions`, `ListUnreadGazettes`, `MarkGazetteRead`
2. **`internal/whip/whip.go`** — 新建 Whip 包：`Whip` struct, `Run(ctx)` 主循环, `threeLineWhip`(进程/tmux 存活检测+stuck标记), `orderPaper`(DAG引擎+自动派单), `gazetteDispatch`(公报路由), `Report`(全局状态)
3. **`cmd/session.go`** — 两阶段 bill 创建：先计算 sessionHex-id 映射，再 resolve depends_on 为完整 ID + 存储 portfolio
4. **`cmd/whip.go`** — 完整实现：`whip start`(PID 文件+信号处理), `whip stop`(SIGTERM), `whip report`

### 验证结果
- DAG 引擎：并行 bills 无依赖直接派发 ✅
- DAG 依赖：上游 enacted 后下游自动派发 ✅
- 心跳检测：进程消失 → stuck（5分钟阈值）✅
- whip report：全局状态展示正确 ✅

## 2026-02-28 — Phase 1 单 Minister 能跑

### 改动摘要
1. **`internal/store/store.go`** — 新增：`NullString()`、`UpdateMinisterWorktree`、`UpdateMinisterPID`、`UpdateBillBranch`、`UpdateSessionStatus`、`ListBillsBySession`、`ListGazettes`、`ListGazettesForBill`
2. **`internal/runtime/runtime.go`** — 新建：Runtime 接口 + SummonOpts + AgentSession
3. **`internal/runtime/claudecode.go`** — 新建：ClaudeCodeRuntime（tmux 或前台模式）
4. **`cmd/session.go`** — 完整实现：`session open`（解析 TOML，创建 session+bills），`session status`，`session dissolve`
5. **`cmd/bill.go`** — 完整实现：`bill list/show/draft/assign`；assign 自动创建 handoff gazette
6. **`cmd/ministers.go`** — `minister summon` 增强：`--bill --project --no-tmux` 参数；创建 Chamber，写 bill brief，调起 ClaudeCodeRuntime
7. **`cmd/gazette.go`** — 完整实现：`gazette list`（支持 `--minister/--bill` 过滤），`gazette show`

### 验证结果
- `hoc session open <toml>` → 创建 session + bills，输出 ID ✅
- `hoc bill assign` → 分配+创建 handoff gazette ✅
- `hoc bill show` → 含公报列表 ✅
- `hoc minister summon` → 无议案=idle，有议案验证项目存在 ✅
- `hoc gazette list` → 公报流转展示 ✅

## 2026-02-28 — Phase 0 初始搭建

- Go module 初始化
- 依赖添加 (Cobra, SQLite, TOML)
- CLI 命令骨架创建 (root + 9 子命令)
- internal/store 存储层实现 (5 表 CRUD)
- internal/config 配置管理
- internal/chamber git worktree 管理
- cmd/ministers.go minister appoint/summon/dismiss/list
- 初始 config 结构与 TOML 输出不一致（已在后续修复）
