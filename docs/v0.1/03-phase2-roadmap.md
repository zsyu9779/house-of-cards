# House of Cards — Phase 2 开发路线图

> 文档创建：2026-02-28
> 适用版本：Phase 6B 起

---

## 一、第一阶段完成总结

### Phase 0 — CLI 骨架（完成）

- Go module + Cobra CLI 初始化
- 9 个子命令骨架：`minister`, `bill`, `session`, `cabinet`, `whip`, `floor`, `gazette`, `hansard`, `speaker`
- `internal/store`：5 表 SQLite 存储层（ministers, bills, sessions, gazettes, hansard）
- `internal/chamber`：Git worktree 沙箱管理
- CLI 前缀 `hoc` 全面统一

### Phase 1 — 单 Minister 能跑（完成）

- `session open` 解析 TOML，创建 session + bills
- `bill assign` 分配议案 + 自动创建 Handoff Gazette
- `minister summon` 创建 Chamber worktree，写 bill brief，调起 ClaudeCodeRuntime
- `gazette list/show` 公报流转展示
- `internal/runtime`：Runtime 接口 + ClaudeCodeRuntime（tmux/前台双模式）

### Phase 2 — 并行 + Whip（完成）

- `internal/whip`：DAG 引擎 + 三线党鞭心跳 + 自动派单
- `session open` 支持 `depends_on` 依赖关系解析
- `whip start/stop/report`：PID 文件 + 信号处理 + 全局状态报告
- `cabinet reshuffle`：按技能匹配批量派单

### Phase 3 — 崩溃恢复 + Speaker AI + Hansard（完成）

- `internal/speaker`：GenerateContext（5 节内容）+ Summon
- `internal/whip`：`byElection()` 补选流程（git stash + Gazette + Hansard + 重置）
- `minister by-election`：手动触发补选
- `hansard list/show`：议事录 + 成功率统计

### Phase 4 — 完善体验（完成）

- `cabinet list`：花名册 + 成功率
- `bill committee`：阅读→委员会阶段
- `bill review --pass/--fail`：委员会审查结果
- `internal/privy`：`MergeSession` git merge 并行分支
- `hoc privy merge`：手动触发枢密院合并

### Phase 5 — 自动化闭环 + TUI 升级（完成）

- Whip `privyAutoMerge()`：并行议案完成后自动合并
- `hoc doctor`：健康检查（DB/git/tmux/claude/卡住 minister）
- `cmd/floor.go`：完整 BubbleTea TUI（lipgloss 样式，实时 3s 刷新，alt-screen）
- Session `--project` 字段：指定目标仓库路径

### Phase 6A — Runtime 扩展（完成）

- `internal/runtime/codex.go`：CodexRuntime（tmux shell 替换 + 前台直传）
- `internal/runtime/cursor.go`：CursorRuntime（`cursor agent --force -p`）
- `internal/runtime/factory.go`：`runtime.New(runtimeType, useTmux)` 工厂函数
- `cmd/ministers.go`：`summon/dismiss` 改为工厂函数，支持多 runtime

---

## 二、第二阶段规划

第二阶段目标：**从可运行的 MVP 到可靠的生产级工具**。重点在工程化、可观测性、智能自动化和平台化。

---

## Phase 6B — 代码工程化（估时：1 周）

### 目标

消除技术债，提升代码质量和可维护性，为后续扩展打下基础。

### 任务列表

#### 6B-1：提取公共格式化工具

- 新建 `internal/util/format.go`
- 提取跨包重复的 `truncate(s string, n int) string`
- 提取跨包重复的 `orDash(s string) string`
- 更新所有引用方（`cmd/bill.go`, `cmd/ministers.go`, `cmd/cabinet.go` 等）

**改动文件**：
```
internal/util/format.go          ← 新建
cmd/bill.go                      ← 替换本地函数为 util 调用
cmd/ministers.go                 ← 替换
cmd/cabinet.go                   ← 替换
cmd/gazette.go                   ← 替换（如有）
```

#### 6B-2：补充 DB 索引

在 `internal/store/store.go` 的 `ensureSchema()` 中添加：

```sql
CREATE INDEX IF NOT EXISTS idx_ministers_status ON ministers(status);
CREATE INDEX IF NOT EXISTS idx_bills_session_id ON bills(session_id);
CREATE INDEX IF NOT EXISTS idx_bills_status ON bills(status);
CREATE INDEX IF NOT EXISTS idx_gazettes_read_at ON gazettes(read_at);
CREATE INDEX IF NOT EXISTS idx_gazettes_minister_id ON gazettes(minister_id);
```

**改动文件**：
```
internal/store/store.go          ← ensureSchema() 追加索引
```

#### 6B-3：cmd 层集成测试

- 新建 `cmd/integration_test.go`
- 测试场景 1：`session open → bill list → bill assign → gazette list`（完整单 minister 流程）
- 测试场景 2：`minister summon → minister dismiss`（minister 生命周期）
- 测试场景 3：`session open → whip report`（空 session 状态正确）
- 使用 `os.TempDir()` 隔离测试 DB，测后清理

**改动文件**：
```
cmd/integration_test.go          ← 新建
```

#### 6B-4：Structured Logging（slog）

- 将所有 `fmt.Fprintf(os.Stderr, ...)` 调试输出迁移到 `log/slog`
- 默认级别 `WARN`，`--verbose` flag 开启 `DEBUG`
- `internal/whip/whip.go` 内部日志改为 slog
- `internal/speaker/speaker.go` 内部日志改为 slog

**改动文件**：
```
cmd/root.go                      ← 初始化 slog handler，注册 --verbose flag
internal/whip/whip.go            ← fmt.Fprintf → slog
internal/speaker/speaker.go      ← fmt.Fprintf → slog
internal/privy/privy.go          ← fmt.Fprintf → slog
```

#### 6B-5：--json 输出标志

- `hoc bill list --json`：输出 JSON 数组，每项包含 id/title/status/assignee/branch
- `hoc minister list --json`：输出 JSON 数组，每项包含 id/name/status/portfolio/runtime
- `hoc session status --json`：输出 JSON，包含 session 信息 + bills 数组
- 使用 `encoding/json` 标准库，不引入额外依赖

**改动文件**：
```
cmd/bill.go                      ← --json flag
cmd/ministers.go                 ← --json flag
cmd/session.go                   ← --json flag
```

### 验收标准

- [ ] `go build ./...` 无编译错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./...` 全部通过，含新集成测试
- [ ] `hoc bill list --json | jq '.[0].id'` 输出正确
- [ ] `truncate`/`orDash` 不再在 cmd 层重复定义
- [ ] 生产运行时无 DEBUG 输出泄漏到 stderr

---

## Phase 7 — 可视化与监控增强（估时：1 周）

### 目标

提升系统可观测性，让运行状态一目了然。

### 任务列表

#### 7-1：session status ASCII DAG

- `hoc session status <id>` 追加 ASCII 依赖树
- 实现 `renderDAG(bills []Bill) string`，纯 ASCII，不依赖外部库
- 示例输出：
  ```
  [enacted] design-api (b001)
      └─► [working] implement-handler (b002)
              └─► [reading] write-tests (b003)
  ```
- 支持并行分支：同级节点并排显示

**改动文件**：
```
cmd/session.go                   ← 追加 DAG 渲染
internal/util/dag.go             ← 新建 DAG ASCII 渲染器
```

#### 7-2：hoc floor TUI 增强

- 实时自动刷新间隔从 3s 降至可配置（`--interval` flag，默认 3s）
- Gazette 流实时展示：底部新增"最新公报"区域，滚动显示最近 5 条
- 快捷键增强：`g` 键打开 gazette 列表视图，`s` 键切换 session 视图
- 颜色编码：working=绿，stuck=红，idle=灰，committee=黄

**改动文件**：
```
cmd/floor.go                     ← 增强 BubbleTea model
```

#### 7-3：hoc hansard 趋势图

- `hoc hansard trend [--last N]`：显示最近 N 条 Hansard 的成功率趋势
- ASCII 条形图：每个 minister 一行，`█` 表示成功，`░` 表示失败
- 示例：
  ```
  go-minister   [██████░░░░] 60% (6/10)
  ts-minister   [████████░░] 80% (8/10)
  ```

**改动文件**：
```
cmd/hansard.go                   ← 新增 trend 子命令
internal/util/chart.go           ← 新建 ASCII 条形图渲染器
```

#### 7-4：hoc whip report 扩展

- `hoc whip report` 增加历史统计：
  - 总 by-election（补选）次数
  - 平均议案完成时长
  - 当前 stuck minister 详情（卡住多久）
- `hoc whip report --history`：展示最近 10 次 Whip 循环事件日志

**改动文件**：
```
internal/store/store.go          ← 新增 WhipStats 查询
cmd/whip.go                      ← report 扩展
```

### 验收标准

- [ ] `hoc session status <id>` 显示 ASCII DAG，依赖关系正确
- [ ] `hoc floor` 实时展示最新 5 条 Gazette
- [ ] `hoc hansard trend` 输出条形图，百分比正确
- [ ] `hoc whip report` 显示补选次数和平均完成时长

---

## Phase 8 — 自动化闭环（估时：2 周）

### 目标

最小化人工干预，实现从 bill 创建到 royal assent 的全自动流水线。

### 任务列表

#### 8-1：Minister 完成信号检测

- 约定：Minister 完成工作后在 Chamber 根目录写入 `.hoc/bill-<id>.done`
- Whip 主循环新增 `pollDoneFiles()` 函数
- 检测到 done 文件后：自动调用 `UpdateBillStatus(id, "enacted")` + 创建 Hansard + 创建 completion Gazette
- Done 文件内容可包含摘要，写入 Gazette content

**设计决策**：
- Done 文件路径：`<chamber_path>/.hoc/bill-<bill-id>.done`
- 文件内容格式：纯文本摘要（可选）
- 检测频率：与主循环同频（30s）

**改动文件**：
```
internal/whip/whip.go            ← pollDoneFiles() 新函数
internal/store/store.go          ← CreateHansardFromDone() 便捷方法
```

#### 8-2：Committee 自动化

- Whip 新增 `committeeAutomation()` 函数
- 识别 `status=committee` 的议案，查找 `portfolio="reviewer"` 的空闲 Minister
- 自动调用 `bill assign` 逻辑，将 committee 阶段议案分配给 reviewer
- Reviewer Minister 完成后写入 `.hoc/bill-<id>.review` 文件（pass/fail + 意见）
- Whip 检测 review 文件，自动执行 `bill review --pass/--fail`

**改动文件**：
```
internal/whip/whip.go            ← committeeAutomation() 新函数
internal/store/store.go          ← ListBillsForCommittee() 查询
```

#### 8-3：Gazette 自动投递

- Gazette 创建后，Whip 的 `gazetteDispatch()` 增强：
  - 如目标 Minister 有活跃 tmux 窗口：发送 Gazette 内容到 tmux（`tmux send-keys`）
  - 如 Minister 无 tmux（前台模式）：写入 `<chamber>/.hoc/inbox/` 目录
  - Minister 可轮询 inbox 目录获取新指令

**改动文件**：
```
internal/whip/whip.go            ← gazetteDispatch() 增强
internal/chamber/chamber.go      ← InboxPath() 工具方法
```

#### 8-4：hoc minister auto — 全自动模式

- `hoc minister auto [--session <id>]`：进入全自动执行模式
- 行为：持续循环，等待 Whip 分配议案，自动 summon，监控完成，自动 dismiss
- 配合 `hoc whip start` 形成完整无人值守流水线
- 支持 `--max-concurrent N`：最多同时运行 N 个 Minister

**改动文件**：
```
cmd/ministers.go                 ← auto 子命令
```

### 验收标准

- [ ] Minister 写入 `.hoc/bill-<id>.done` 后，5 分钟内自动 enacted
- [ ] `portfolio=reviewer` 的 Minister 自动接收 committee 阶段议案
- [ ] Gazette 自动投递到 tmux 窗口（如存在）
- [ ] `hoc minister auto` 启动后，全程无需人工干预完成一个简单 session

---

## Phase 9 — Speaker AI 增强（估时：2 周）

### 目标

提升 Speaker 的决策质量和自主性，探索多 Speaker 架构原型。

### 任务列表

#### 9-1：Speaker context.md 格式优化

- 在 `internal/speaker/speaker.go` 的 `GenerateContext()` 中增加：
  - 当前拓扑类型（parallel/pipeline/tree）及推荐理由
  - 最近 3 次 by-election 原因摘要
  - 当前系统资源利用率（活跃 Minister 数 / 总 Cabinet 容量）
  - 推荐下一步行动（优先级排序的待办列表）

**改动文件**：
```
internal/speaker/speaker.go      ← GenerateContext() 扩展
```

#### 9-2：Speaker Patrol 自动推进

- `hoc speaker patrol` 命令：持续运行，每 60s 刷新 context.md 并触发 Speaker AI 决策
- 检测 Speaker 输出（从 Gazette 流读取）并自动执行指令
- 支持指令集：`assign <bill> <minister>`, `by-election <minister>`, `escalate <bill>`
- Patrol 模式下，Speaker 指令优先级高于 Whip 自动派单

**改动文件**：
```
cmd/speaker.go                   ← patrol 子命令
internal/speaker/speaker.go      ← ParseDecision() 指令解析
```

#### 9-3：Speaker 拓扑自动选择

- Speaker 在 `summon` 时，基于当前 bills 依赖图自动选择最优拓扑：
  - 无依赖关系 → parallel
  - 线性链式依赖 → pipeline
  - 树状依赖 → tree（当前不支持 mesh）
- 拓扑选择结果写入 context.md 供人工审查
- `hoc session status` 展示当前拓扑类型

**改动文件**：
```
internal/speaker/speaker.go      ← SelectTopology() 函数
cmd/session.go                   ← 展示拓扑类型
```

#### 9-4：多 Speaker 竞标制（原型）

> 本项为概念验证，不进入生产。

- 设计 `internal/speaker/council.go`：多 Speaker 并行生成决策
- 实现简单多数投票：3 个 Speaker 决策取共识
- 输出竞标结果到 `.hoc/council-<timestamp>.md`
- 仅作为研究原型，不集成到主流程

**改动文件**：
```
internal/speaker/council.go      ← 新建（原型）
cmd/speaker.go                   ← council 子命令（实验性）
```

### 验收标准

- [ ] `context.md` 包含拓扑推荐和最近 by-election 摘要
- [ ] `hoc speaker patrol` 启动后每 60s 自动决策循环
- [ ] Speaker 指令 `assign` 被自动执行
- [ ] 3 个 bills 无依赖时，Speaker 自动选择 parallel 拓扑
- [ ] `hoc speaker council`（实验）输出多 Speaker 竞标结果

---

## Phase 10 — 平台化扩展（估时：3 周）

### 目标

从单仓库工具扩展为多项目、可编程的 AI 协作平台。

### 任务列表

#### 10-1：多项目会期

- Session 支持多仓库：`project` 字段改为数组（`projects []string`）
- 每个 bill 可指定所属仓库（`bill.project`）
- Chamber 创建时选择对应仓库的 worktree
- 跨仓库合并：Privy Council 分仓库串行合并，冲突单独报告

**改动文件**：
```
internal/store/store.go          ← project 字段支持多值（JSON 序列化）
internal/chamber/chamber.go      ← 多仓库 worktree 创建
internal/privy/privy.go          ← 多仓库合并逻辑
cmd/session.go                   ← --projects flag（逗号分隔）
```

#### 10-2：API Server 模式

- `hoc serve [--port 8080]`：启动 HTTP REST API 服务
- 端点设计：
  ```
  GET    /api/v1/sessions           → session list
  POST   /api/v1/sessions           → session open（JSON body）
  GET    /api/v1/sessions/:id       → session status
  GET    /api/v1/ministers          → minister list
  POST   /api/v1/ministers/:id/summon → minister summon
  POST   /api/v1/bills/:id/assign   → bill assign
  GET    /api/v1/gazettes           → gazette list
  POST   /api/v1/webhooks           → webhook 触发（GitHub Actions 集成）
  ```
- 使用标准库 `net/http`，不引入 Web 框架

**改动文件**：
```
cmd/serve.go                     ← 新建
internal/api/handlers.go         ← 新建
internal/api/router.go           ← 新建
cmd/root.go                      ← 注册 serveCmd
```

#### 10-3：Minister 动态扩缩容

- Whip 新增 `autoscale()` 函数
- 基于队列深度自动 summon 新 Minister：
  - `pending bills > idle ministers * 2` → summon 一个新 Minister
  - `idle ministers > pending bills + 2` → dismiss 一个 idle Minister
- 受 `--max-ministers N` 上限约束
- 扩缩容事件写入 Gazette

**改动文件**：
```
internal/whip/whip.go            ← autoscale() 新函数
```

#### 10-4：配置热更新

- Whip 监听 `~/.hoc/config.toml` 文件变更（`fsnotify`）
- 支持热更新的配置项：`whip_interval`, `stuck_threshold`, `max_ministers`
- 不支持热更新的配置项重启时生效，并在日志中提示
- `hoc config reload`：手动触发配置重新加载

**改动文件**：
```
internal/config/config.go        ← 新建（或重构）
internal/whip/whip.go            ← 监听配置变更
cmd/root.go                      ← 全局 config 加载
go.mod                           ← 添加 fsnotify 依赖
```

### 验收标准

- [ ] `hoc session open` 支持多仓库项目
- [ ] `hoc serve` 启动后，`curl /api/v1/sessions` 返回正确 JSON
- [ ] Whip 在队列积压时自动 summon 新 Minister
- [ ] 修改 `config.toml` 的 `whip_interval` 后，Whip 在 5s 内应用新值（无需重启）

---

## 三、技术设计要点

### 3.1 完成信号约定

```
# Minister 完成工作后写入：
<chamber_path>/.hoc/bill-<bill-id>.done

# 文件内容（可选摘要）：
Implemented the user authentication module.
Added JWT token generation and validation.
Tests: 47 passing.

# Review 结果文件：
<chamber_path>/.hoc/bill-<bill-id>.review
# 文件内容：
PASS
The implementation meets all requirements.
```

### 3.2 DAG ASCII 渲染算法

```go
// internal/util/dag.go
type DAGNode struct {
    Bill     Bill
    Children []*DAGNode
    Parents  []*DAGNode
}

func BuildDAG(bills []Bill) []*DAGNode  // 构建拓扑
func RenderDAG(roots []*DAGNode) string  // ASCII 渲染

// 渲染规则：
// - roots 并排显示（同一行前缀 ├─►）
// - 最后一个 root 用 └─►
// - 缩进 4 空格每层
// - 状态颜色：通过 lipgloss（TUI）或括号标注（CLI）
```

### 3.3 API Server 认证

- Phase 10 初版不做认证（仅本地使用）
- 后续版本：Bearer Token（存储在 `~/.hoc/api-token`）
- Webhook 签名：HMAC-SHA256

### 3.4 多 Speaker 竞标协议

```
1. Council 创建 3 个独立的 Speaker context.md 副本（注入不同随机种子）
2. 并发调用 3 个 Claude Code 实例
3. 等待所有实例完成（超时 120s）
4. 解析各自输出，按指令类型统计投票
5. 执行得票 ≥ 2 的指令
6. 弃权/超时的实例不计票
```

### 3.5 关键接口变更

**Phase 6B**：`internal/util/format.go` 新 API
```go
package util

func Truncate(s string, n int) string
func OrDash(s string) string
```

**Phase 8**：Done 文件轮询接口
```go
// internal/whip/whip.go
func (w *Whip) pollDoneFiles(ctx context.Context) error

// internal/store/store.go
func (s *Store) EnactBillFromDone(billID, summary string) error
```

**Phase 10**：API 路由接口
```go
// internal/api/router.go
func NewRouter(store *store.Store) http.Handler
```

---

## 四、风险与依赖

### 风险

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|---------|
| Minister 不写 done 文件 | Phase 8 自动化失效 | 中 | 保留手动 `bill enacted` 命令；文档约定写入 done 文件 |
| fsnotify 跨平台兼容性 | Phase 10 配置热更新失效 | 低 | 退化到 SIGHUP 信号触发；仅 macOS/Linux 支持 |
| 多仓库 git 操作冲突 | Phase 10 多项目 session 数据错乱 | 中 | 单测覆盖多仓库场景；先支持 2 个仓库上限 |
| Claude API 并发限制 | Phase 9 多 Speaker 竞标被限速 | 高 | 竞标制作为实验功能，不进入默认流程；添加退避重试 |

### 外部依赖

| 依赖 | 版本 | 用途 | 引入阶段 |
|------|------|------|---------|
| `github.com/fsnotify/fsnotify` | v1.7+ | 配置文件监听 | Phase 10 |
| `net/http`（标准库） | — | API Server | Phase 10 |
| `log/slog`（标准库） | Go 1.21+ | 结构化日志 | Phase 6B |

### 内部依赖顺序

```
Phase 6B（工程化）
    └─► Phase 7（可视化）
            └─► Phase 8（自动化）
                    ├─► Phase 9（AI 增强）
                    └─► Phase 10（平台化）
```

Phase 6B 是后续所有阶段的基础，必须优先完成。Phase 9 和 Phase 10 可并行推进，互不阻塞。

---

## 五、里程碑检查点

| 检查点 | 条件 | 目标日期 |
|--------|------|---------|
| 6B 完成 | 集成测试全通过，`--json` 可用 | Week 1 |
| 7 完成 | `hoc floor` 展示 Gazette 流，DAG 可见 | Week 2 |
| 8 完成 | 全自动 session 无人值守跑通 | Week 4 |
| 9 完成 | Speaker Patrol 稳定运行 60+ 分钟 | Week 6 |
| 10 完成 | API Server 可接收 GitHub webhook | Week 9 |

---

*文档维护：每个阶段完成后更新对应章节状态。*
