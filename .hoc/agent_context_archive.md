# Agent Context Archive

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
