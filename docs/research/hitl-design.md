# HITL 技术设计：House of Cards 人类介入机制

> 版本：v0.2（2026-04-08）
> 状态：设计草案，供迭代参考
> 变更：v0.2 新增第 8 节多界面延展设计（TUI/GUI）

---

## 1. 三个层级

| 层级 | 定义 | HoC 映射 | 适用场景 |
|------|------|---------|---------|
| **Human-in-the-loop** | 每步审批，Agent 无自主权 | Speaker 每次拆 Bill 都需人工确认后才派发 | 初次使用、高风险项目、生产环境操作 |
| **Human-on-the-loop** | 关键节点介入，日常自动 | Whip 自动派发，但敏感操作和最终输出需人工审核 | **默认模式**，多数开发场景 |
| **Human-out-of-loop** | 全自动，异常才通知 | Shadow 模式 + 异常告警触发介入 | 成熟流水线、低风险批量任务、CI/CD 集成 |

**结论**：默认使用 Human-on-the-loop。层级通过 Session 配置选择，不在运行时切换。

```toml
# session 配置示例
[hitl]
mode = "on-the-loop"   # "in-the-loop" | "on-the-loop" | "out-of-the-loop"
```

---

## 2. 三类介入节点

### 节点 1：计划审核（Plan Review）

**位置**：Speaker 拆解 Bill 后、Whip 执行 Order Paper 派发前。

**为什么这里拦**：纠错成本最低。一旦 Minister 开始执行，回滚代价指数增长。

| 条件 | 行为 |
|------|------|
| `hitl.mode == "in-the-loop"` | **强制阻断**。所有 Bill 进入 `pending_review` 状态，等待人工确认后流转到 `draft` |
| `hitl.mode == "on-the-loop"` + 以下任一触发 | **阻断**：(1) Session 包含 ≥5 个 Bill；(2) Bill 涉及生产分支；(3) 拓扑为 `mesh`（复杂度最高）；(4) Bill 描述匹配敏感关键词（`delete`、`drop`、`migration`、`deploy`） |
| `hitl.mode == "on-the-loop"` + 无触发条件 | **放行**，记录到 Event Ledger |
| `hitl.mode == "out-of-the-loop"` | **放行**，记录到 Event Ledger |

**CLI 交互**：

```
$ hoc session plan

📋 Session "auth-refactor" — Speaker 提出以下 Bill：

  #1 [bill-a1b2] 重写 JWT middleware         → backend-claude
  #2 [bill-c3d4] 更新前端 token 刷新逻辑      → frontend-claude
  #3 [bill-e5f6] 删除旧 session 表 ⚠️ 敏感    → backend-claude

  拓扑：pipeline (#1 → #2, #1 → #3)

  [a]pprove all  [r]eject  [e]dit  [1-3] 逐条审核
  > _
```

### 节点 2：敏感操作授权（Operation Authorization）

**位置**：Minister 执行不可逆操作时，实时拦截。

**为什么这里拦**：有些操作不可逆（删文件、调外部 API、执行任意 shell 命令），必须在执行前拦截，不能事后补救。

| 操作类型 | 风险等级 | 默认策略 |
|---------|---------|---------|
| 读取文件、读取 Git 状态 | 无 | 放行 |
| 写入/修改已有文件 | 低 | on-the-loop 放行，in-the-loop 需确认 |
| 创建新文件 | 低 | 放行 |
| 删除文件 | 高 | **强制确认** |
| `git commit` | 低 | 放行（Chamber 内操作） |
| `git push`、`git push --force` | 高 | **强制确认** |
| 执行 shell 命令（非白名单） | 高 | **强制确认** |
| 调用外部 API（HTTP 请求） | 高 | **强制确认** |
| 数据库 DDL（`ALTER`、`DROP`） | 高 | **强制确认** |
| `git checkout`、`git reset --hard` | 高 | **强制确认** |

**白名单机制**：通过 TOML 配置允许特定命令跳过确认：

```toml
[hitl.allowlist]
shell = ["go test ./...", "go build ./...", "golangci-lint run ./..."]
```

**非白名单命令拦截流程**：

```
Minister backend-claude 请求执行：

  $ rm -rf ./internal/legacy/

  Bill: bill-a1b2 (重写 JWT middleware)
  风险: 高（删除操作）

  [a]pprove  [d]eny  [e]dit command  [s]kip bill
  > _
```

### 节点 3：输出审核（Output Review）

**位置**：Bill 标记 `enacted` 后、进入 `royal_assent` 前。对应现有 Committee 阶段。

**为什么这里拦**：最终 sanity check。Minister 可能完成了工作但结果不符合预期。

| 条件 | 行为 |
|------|------|
| `hitl.mode == "in-the-loop"` | **强制审核**。所有 Bill 必须经过 Committee 才能 `royal_assent` |
| `hitl.mode == "on-the-loop"` + 以下任一触发 | **触发审核**：(1) Bill 修改了 ≥10 个文件；(2) Bill 涉及跨 Minister 依赖（pipeline 下游）；(3) Bill 标记了 `at_risk`（曾被 stuck）；(4) 测试覆盖率下降 |
| `hitl.mode == "on-the-loop"` + 无触发条件 | **自动通过**，但生成 Diff 摘要到 Gazette |
| `hitl.mode == "out-of-the-loop"` | **自动通过**，Shadow 记录 |

**CLI 交互**：

```
$ hoc review bill-a1b2

📦 Bill "重写 JWT middleware" — enacted by backend-claude

  Branch: bill/bill-a1b2
  Changes: 7 files, +342/-128
  Tests: 14 passed, 0 failed
  Duration: 12m34s

  Diff summary:
    M internal/auth/jwt.go        (+89/-45)
    M internal/auth/middleware.go  (+67/-32)
    A internal/auth/refresh.go     (+112)
    ...

  [a]pprove → royal_assent
  [r]eject → reset to draft
  [d]iff   → show full diff
  [c]omment → add review note
  > _
```

---

## 3. 四种交互形态

### 3.1 强制确认（高风险 · 低频）

**场景**：删除操作、force push、DDL 变更、外部 API 调用。

**特征**：阻断执行流，Minister 挂起等待，人工必须主动输入才能继续。

**CLI 实现**：

```
# Whip 在终端输出阻断提示
⛔ APPROVAL REQUIRED

  Minister: backend-claude
  Bill: bill-e5f6 (删除旧 session 表)
  Operation: DROP TABLE old_sessions;

  This operation is irreversible.

  Type "approve" to continue, or "deny" to block:
  > _
```

**实现要点**：
- Whip 轮询发现 approval 请求 → 写入 `approval_queue` 表 → 阻塞 Minister 进程
- Minister 进程通过 `.hoc/bill-<id>.approval` 文件等待响应
- 人工通过 `hoc approve <approval-id>` 或 TUI 交互确认
- **超时策略**：5 分钟无响应 → deny（保守默认）

**适用**：节点 2 中所有高风险操作。

### 3.2 异步审批队列（高风险 · 高频）

**场景**：批量文件写入、多个 Minister 同时请求敏感操作、CI/CD pipeline 中的审批。

**特征**：请求入队不阻断其他 Minister，人工可批量处理。

**CLI 实现**：

```
$ hoc approvals

📋 Pending Approvals (3)

  ID        Minister         Operation              Bill          Age
  apv-001   backend-claude   write 5 files          bill-a1b2     2m
  apv-002   frontend-claude  npm install lodash     bill-c3d4     5m
  apv-003   backend-claude   curl api.stripe.com    bill-a1b2     8m ⚠️

  [a]pprove all  [1-3] 逐条处理  [d]eny all  [f]ilter
  > _
```

```
$ hoc approve apv-001 apv-002  # 批量审批
$ hoc deny apv-003 --reason "不允许直接调用生产 API"
```

**实现要点**：
- 审批请求写入 `approval_queue` 表，状态 `pending`
- Minister 继续执行其他不需要审批的操作（如果有），或挂起等待
- 人工通过 `hoc approvals` 查看队列，支持批量操作
- **超时策略**：可配置。默认 15 分钟，超时后：
  - 高风险 → deny（保守）
  - 中风险 → 降级为 dry-run 模式（执行但不写入）

**适用**：多个 Minister 并发工作时的敏感操作。

### 3.3 Inline 预览编辑（低风险 · 低频）

**场景**：计划审核（节点 1）、单文件修改确认、配置变更。

**特征**：展示 Diff，低摩擦接受。回车即通过，只在需要修改时才需额外输入。

**CLI 实现**：

```
$ hoc session plan --preview

📋 Bill #3: 删除旧 session 表

  Speaker 建议的执行计划：
  1. 备份 old_sessions 表到 old_sessions_backup
  2. 验证新表数据完整性
  3. DROP TABLE old_sessions

  [Enter] approve  [e]dit plan  [s]kip
  > _
```

**实现要点**：
- 不需要队列，同步处理
- 默认操作是 approve（Enter 即可）
- 编辑后的内容直接回写到 Bill 描述
- 适合 TUI（BubbleTea）后期实现为内联编辑器

**适用**：节点 1（计划审核）、节点 3（输出审核）中低风险项。

### 3.4 Shadow + 异常告警（低风险 · 高频）

**场景**：`out-of-the-loop` 模式下的全自动执行、成熟流水线中的文件写入。

**特征**：自动执行，全量记录，异常时分级触发介入。

**告警分级**：

| 级别 | 触发条件 | 行为 |
|------|---------|------|
| **INFO** | 正常操作完成 | 仅记录到 Event Ledger |
| **WARN** | 单 Bill 执行时间超过预期 2 倍；测试失败率 >10% | 终端通知 + Gazette |
| **ALERT** | Minister stuck 且自动恢复失败；合并冲突；数据丢失风险 | **升级为强制确认**，阻断后续操作 |

**CLI 实现**：

```
# 后台运行，异常时推送
$ hoc session run --shadow

[12:03:14] ✅ bill-a1b2 enacted (backend-claude, 8m22s)
[12:05:01] ✅ bill-c3d4 enacted (frontend-claude, 6m11s)
[12:07:33] ⚠️  bill-e5f6 执行时间异常 (已 15m, 预期 8m)
[12:09:00] 🚨 bill-e5f6 ALERT: Minister stuck, 自动恢复失败

  ⛔ 自动模式已暂停。需要人工介入。
  Type "hoc resume" to continue or "hoc abort" to stop.
```

**实现要点**：
- 所有操作写入 Event Ledger（`events` 表），topic 为 `shadow.*`
- Whip tick 循环中增加异常检测逻辑
- ALERT 级别触发时，session 标记为 `paused`，等待人工恢复
- 告警可通过 webhook 推送到外部系统（Slack、邮件等），v2 再做

**适用**：`out-of-the-loop` 模式下所有操作。

---

## 4. 数据结构

### 4.1 审批队列（approval_queue 表）

```sql
CREATE TABLE approval_queue (
    id          TEXT PRIMARY KEY,           -- "apv-xxxx"
    bill_id     TEXT NOT NULL,
    minister_id TEXT NOT NULL,
    session_id  TEXT,
    type        TEXT NOT NULL,              -- "plan_review" | "op_auth" | "output_review"
    operation   TEXT NOT NULL,              -- 人类可读的操作描述
    risk_level  TEXT NOT NULL,              -- "low" | "medium" | "high" | "critical"
    context     TEXT,                       -- JSON: 操作上下文（命令、文件路径、diff 等）
    status      TEXT DEFAULT 'pending',     -- "pending" | "approved" | "denied" | "timeout" | "escalated"
    decided_by  TEXT,                       -- 审批人标识（预留多人场景）
    reason      TEXT,                       -- deny 时的原因
    timeout_at  DATETIME,                   -- 超时时间
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    decided_at  DATETIME
);

CREATE INDEX idx_approval_status ON approval_queue(status);
CREATE INDEX idx_approval_bill ON approval_queue(bill_id);
```

### 4.2 执行日志（复用 events 表）

现有 `events` 表已有 `topic`、`bill_id`、`minister_id`、`payload` 字段，直接复用。

**新增 topic 规范**：

| topic | 含义 | payload 示例 |
|-------|------|-------------|
| `hitl.plan_review.requested` | 计划审核请求 | `{"bills": ["bill-a1b2"], "trigger": "bill_count>=5"}` |
| `hitl.plan_review.approved` | 计划审核通过 | `{"bills": ["bill-a1b2"], "modified": false}` |
| `hitl.plan_review.denied` | 计划审核拒绝 | `{"bills": ["bill-a1b2"], "reason": "..."}` |
| `hitl.op_auth.requested` | 操作授权请求 | `{"operation": "rm -rf ./legacy", "risk": "high"}` |
| `hitl.op_auth.approved` | 操作授权通过 | `{"approval_id": "apv-001"}` |
| `hitl.op_auth.denied` | 操作授权拒绝 | `{"approval_id": "apv-001", "reason": "..."}` |
| `hitl.op_auth.timeout` | 操作授权超时 | `{"approval_id": "apv-001", "fallback": "deny"}` |
| `hitl.output_review.auto_pass` | 输出自动通过 | `{"bill_id": "bill-a1b2", "trigger": "no_risk_factors"}` |
| `hitl.shadow.warn` | Shadow 告警 | `{"level": "warn", "message": "..."}` |
| `hitl.shadow.alert` | Shadow 升级 | `{"level": "alert", "action": "pause_session"}` |

### 4.3 降级策略配置

```toml
[hitl.timeout]
# 强制确认超时（秒）
forced_confirm = 300          # 5 分钟，超时 deny
# 异步审批超时（秒）
async_approval = 900          # 15 分钟
# 超时后的降级行为
fallback = "deny"             # "deny" | "dry-run" | "escalate"

[hitl.escalation]
# 升级路径：超时 → 降级 → 再超时 → 最终行为
max_escalations = 2
final_fallback = "abort_bill" # "abort_bill" | "pause_session" | "continue_shadow"
```

### 4.4 Bill 状态扩展

现有状态机：`draft → reading → committee → enacted → royal_assent`

新增状态：

```
draft → pending_review → reading → ...
                ↑
        （节点 1 拦截时插入）
```

- `pending_review`：等待人工审核计划。仅在 `in-the-loop` 或触发条件命中时使用。
- 未触发时直接 `draft → reading`，无额外状态。

### 4.5 Session 状态扩展

新增 `paused` 状态：

```
active → paused → active    （Shadow ALERT 触发暂停，人工恢复后继续）
active → completed
active → dissolved
```

### 4.6 Minister 文件协议扩展

现有文件协议：`.hoc/bill-<id>.done`、`.hoc/bill-<id>.ack`、`.hoc/bill-<id>.review`

新增：

| 文件 | 方向 | 含义 |
|------|------|------|
| `.hoc/bill-<id>.approval-request` | Minister → Whip | Minister 请求授权执行敏感操作 |
| `.hoc/bill-<id>.approval-response` | Whip → Minister | 授权结果（approve/deny） |

**approval-request 格式**（TOML）：

```toml
operation = "shell"
command = "rm -rf ./internal/legacy/"
risk = "high"
reason = "清理废弃代码，已确认无引用"
```

**approval-response 格式**（TOML）：

```toml
status = "approved"     # "approved" | "denied"
reason = ""
decided_at = "2026-04-07T12:03:14Z"
```

---

## 5. 反模式

### 5.1 审批疲劳（Approval Fatigue）

**症状**：每个操作都弹确认 → 人类无脑点 approve → HITL 形同虚设。

**根因**：风险等级没有分级，或者分级不准，低风险操作也要确认。

**对策**：
- 默认 `on-the-loop`，只拦高风险
- 白名单机制让常见安全操作免确认
- 监控审批通过率：如果 >95% 都是 approve，说明拦截阈值太低，需要调整

### 5.2 同步阻断一切（Synchronous Blocking Hell）

**症状**：所有审批都用强制确认 → 人不在电脑前时整个系统停摆。

**根因**：没有区分"必须立即确认"和"可以排队等"的场景。

**对策**：
- 高风险 · 高频场景用异步队列，不用同步阻断
- 超时有明确降级策略，不会无限等待
- 多个 Minister 的审批请求独立，一个等待不影响其他

### 5.3 不可溯源的自动决策（Untracked Auto-Decisions）

**症状**：`out-of-the-loop` 模式下出了问题，但不知道哪个操作导致的。

**根因**：Shadow 模式只记了"做了什么"，没记"为什么自动放行"。

**对策**：
- 每个自动放行决策都写 Event Ledger，包含触发条件和放行原因
- Shadow 日志必须记录：操作内容 + 风险评估结果 + 放行依据
- `hitl.shadow.*` event topic 覆盖全生命周期

### 5.4 超时策略缺失（No Timeout = Silent Deadlock）

**症状**：审批请求发出后无人响应，Minister 永远挂起，Session 静默卡死。

**根因**：没有超时机制，或者超时后没有明确的降级行为。

**对策**：
- 每个审批请求必须带 `timeout_at` 字段
- 超时后执行配置的 fallback（deny/dry-run/escalate）
- Whip tick 循环中检测超时的审批请求并执行降级

### 5.5 权限膨胀（Permission Creep）

**症状**：白名单越加越多 → 最后几乎所有操作都在白名单里 → 等于没有 HITL。

**根因**：白名单没有审计机制，只加不减。

**对策**：
- 白名单变更记录到 Event Ledger
- 定期审计：列出最近 N 天内哪些白名单条目被命中、哪些从未命中
- 白名单支持 `expires_at` 字段，过期自动失效

### 5.6 "改了再审"倒置（Review-After-The-Fact）

**症状**：Minister 已经执行了操作（写了文件、跑了命令），然后才弹出审核 → 拒绝也没用了。

**根因**：拦截点放在了操作完成后而不是执行前。

**对策**：
- 节点 2 必须在操作执行前拦截，不是执行后
- Minister runtime 层必须支持 pre-execution hook
- approval-request 文件必须在命令执行前写入，Whip 确认后 Minister 才执行

---

## 6. 与现有架构的集成点

| 现有组件 | 集成方式 | 改动量 |
|---------|---------|--------|
| **Whip tick 循环** | 新增 `pollApprovals()` 步骤，检测 approval-request 文件和超时 | 中 |
| **Speaker** | 计划审核在 Speaker 输出后、Whip 派发前插入。Speaker 无需改动 | 无 |
| **Minister runtime** | 需要 pre-execution hook 支持，写 approval-request 文件 | 大 |
| **Store** | 新增 `approval_queue` 表，`events` 表新增 hitl topic | 小 |
| **Bill 状态机** | 新增 `pending_review` 状态（可选） | 小 |
| **Session 状态机** | 新增 `paused` 状态 | 小 |
| **CLI（Cobra）** | 新增 `hoc approvals`、`hoc approve`、`hoc deny`、`hoc review` 命令 | 中 |
| **Chamber** | 文件协议扩展（approval-request/response） | 小 |
| **Gazette** | 审批结果通知可复用现有 Gazette 机制 | 无 |
| **Event Ledger** | 新增 `hitl.*` topic 族 | 小 |

---

## 7. 实施优先级建议

| 优先级 | 内容 | 依赖 |
|-------|------|------|
| **P0** | `approval_queue` 表 + `hoc approvals/approve/deny` CLI | Store |
| **P0** | 节点 2 文件协议（approval-request/response） | Chamber |
| **P1** | Whip `pollApprovals()` + 超时降级 | P0 |
| **P1** | 节点 1 计划审核（`hoc session plan --preview`） | Speaker |
| **P2** | 节点 3 输出审核（增强现有 Committee） | Committee |
| **P2** | Shadow 模式 + 异常告警 | Event Ledger |
| **P3** | TUI 交互（BubbleTea 集成） | P0-P2 |
| **P3** | 外部通知（webhook） | Event Ledger |
| **P4** | GUI Web (HTMX) HITL 面板 | API Layer + P0-P2 |

---

## 8. 多界面延展设计（TUI / GUI）

> 参考：`docs/v0.3/user-interaction-design.md` 定义的三层架构（CLI → TUI → GUI）

### 8.1 核心原则

**HITL 逻辑不在界面层实现。** 界面只是审批队列的消费端。

```
                    ┌──────────┐
                    │ approval │  ← 唯一数据源
                    │ _queue   │
                    └────┬─────┘
           ┌─────────────┼──────────────┐
           ▼             ▼              ▼
      ┌─────────┐  ┌──────────┐  ┌───────────┐
      │  CLI    │  │  TUI     │  │  Web GUI  │
      │ hoc     │  │  floor   │  │  HTMX     │
      │ approve │  │  panel   │  │  dashboard│
      └─────────┘  └──────────┘  └───────────┘
```

**规则**：
- CLI/TUI/GUI 都通过同一套 API（或直接 Store 方法）读写 `approval_queue`
- 不存在"TUI 专属审批状态"或"GUI 专属审批流程"
- 任何界面做出的 approve/deny 决策，其他界面立即可见
- 避免多端同时操作同一审批项：用 `decided_at IS NULL` 做乐观锁

### 8.2 四种交互形态在三种界面中的映射

| 交互形态 | CLI | TUI (BubbleTea) | GUI (HTMX Web) |
|---------|-----|-----------------|-----------------|
| **强制确认** | 阻断式 stdin 输入 | 模态弹窗，焦点锁定 | 浏览器通知 + 模态对话框 |
| **异步审批队列** | `hoc approvals` 列表命令 | 常驻侧边栏面板 | 实时更新的审批看板 |
| **Inline 预览** | Diff 文本 + Enter 确认 | 内嵌 Diff 高亮 + 单键操作 | 语法高亮 Diff + 一键按钮 |
| **Shadow 告警** | 日志流输出 | 状态栏 badge + 告警面板 | 浏览器推送通知 + 时间线 |

### 8.3 TUI 层 HITL 设计（BubbleTea）

#### 8.3.1 审批面板（Approval Panel）

集成到现有 `hoc floor` TUI 中，作为新 Tab 或常驻侧边栏。

```
┌─────────────────────────────────────────────────────────────────┐
│ [1] Overview  [2] Bills  [3] Ministers  [4] Approvals (3🔴)     │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ⛔ PENDING APPROVALS                          sorted: urgency  │
│  ───────────────────────────────────────────────────────────────│
│  ▶ apv-003  backend-claude   curl api.stripe.com    ⏱ 8m  🔴  │
│    apv-002  frontend-claude  npm install lodash      ⏱ 5m  🟡  │
│    apv-001  backend-claude   write 5 files           ⏱ 2m  🟢  │
│                                                                 │
│  ─── Detail ────────────────────────────────────────────────── │
│  ID:        apv-003                                             │
│  Minister:  backend-claude                                      │
│  Bill:      bill-a1b2 (重写 JWT middleware)                     │
│  Operation: curl -X POST https://api.stripe.com/v1/charges     │
│  Risk:      high                                                │
│  Reason:    "测试支付接口连通性"                                  │
│  Timeout:   7m remaining                                        │
│                                                                 │
│  [a] approve  [d] deny  [e] edit  [s] skip  [A] approve all   │
└─────────────────────────────────────────────────────────────────┘
```

**关键交互**：
- `j/k` 或 `↑/↓`：在审批项间导航
- `a`：approve 当前项
- `d`：deny 当前项（弹出 reason 输入）
- `A`：批量 approve 所有 pending 项（二次确认）
- `Tab`：切换到其他面板，审批面板 badge 持续更新

#### 8.3.2 强制确认模态（Modal Overlay）

高风险操作触发时，TUI 渲染全屏模态覆盖层，阻断其他操作。

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                 │
│                                                                 │
│         ┌─────────────────────────────────────────┐             │
│         │  ⛔ APPROVAL REQUIRED                    │             │
│         │                                         │             │
│         │  Minister: backend-claude               │             │
│         │  Operation: DROP TABLE old_sessions     │             │
│         │                                         │             │
│         │  ⚠️ This operation is irreversible.      │             │
│         │  Timeout: 4m32s remaining               │             │
│         │                                         │             │
│         │  [a] approve   [d] deny   [Esc] later  │             │
│         └─────────────────────────────────────────┘             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**行为**：
- 模态出现时播放终端 bell（`\a`）
- `Esc` 不是 deny，是"稍后处理"——回到审批队列，该项保持 pending
- 超时倒计时实时更新
- 模态可在任何 Tab 下弹出，不需要切到 Approvals Tab

#### 8.3.3 Inline Diff 预览

节点 1（计划审核）和节点 3（输出审核）中的低风险项，直接在当前面板内嵌展示。

```
┌─────────────────────────────────────────────────────────────────┐
│ Bill #bill-a1b2: 重写 JWT middleware         [enacted → review] │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Changes: 7 files, +342/-128         Tests: 14✓ 0✗             │
│                                                                 │
│  internal/auth/jwt.go:                                          │
│  ┃  42 │-func validateToken(t string) bool {                   │
│  ┃  42 │+func validateToken(t string) (Claims, error) {        │
│  ┃  43 │+    claims, err := parseJWT(t)                        │
│  ┃  44 │+    if err != nil {                                   │
│  ┃  ...│     (12 more lines)                                   │
│                                                                 │
│  [Enter] approve  [f] full diff  [r] reject  [c] comment      │
└─────────────────────────────────────────────────────────────────┘
```

**行为**：
- 默认只展示前 20 行变更，`f` 进入全屏 Diff 浏览（scrollable）
- `Enter` 即 approve，最低摩擦
- lipgloss 渲染红绿 Diff 高亮

#### 8.3.4 Shadow 状态栏

`out-of-the-loop` 模式下，告警信息渲染在 TUI 底部状态栏。

```
┌─────────────────────────────────────────────────────────────────┐
│  ... (normal TUI content) ...                                   │
├─────────────────────────────────────────────────────────────────┤
│ 🟢 Shadow OK │ 3 enacted │ 1 reading │ ⚠️ bill-e5f6 slow (15m) │
└─────────────────────────────────────────────────────────────────┘
```

当 ALERT 触发时，状态栏变色 + 自动弹出模态：

```
├─────────────────────────────────────────────────────────────────┤
│ 🔴 PAUSED │ bill-e5f6: Minister stuck, 自动恢复失败            │
└─────────────────────────────────────────────────────────────────┘
```

#### 8.3.5 BubbleTea 实现要点

```go
// 新增 Model 组件
type ApprovalPanelModel struct {
    approvals []store.Approval   // 从 approval_queue 表加载
    cursor    int
    detail    *store.Approval    // 当前选中项
    modal     *ModalModel        // 强制确认模态（nil = 无模态）
}

// 融入现有 floor Model
type FloorModel struct {
    tabs       []TabModel
    approvals  ApprovalPanelModel  // 新增 Tab
    statusBar  StatusBarModel      // Shadow 状态栏
    // ...existing fields
}
```

**数据刷新**：与 Whip tick 同步，每 10 秒轮询 `approval_queue` 表。不用 WebSocket，轮询够了。

### 8.4 GUI 层 HITL 设计（HTMX Web）

#### 8.4.1 架构

```
浏览器
  │
  ├── GET  /api/v1/approvals          → 审批队列列表
  ├── POST /api/v1/approvals/:id      → approve/deny
  ├── GET  /api/v1/approvals/:id/diff → Diff 内容
  ├── GET  /api/v1/shadow/alerts      → Shadow 告警
  └── SSE  /api/v1/events/stream      → 实时事件推送
```

**关键决策**：用 SSE（Server-Sent Events）而非 WebSocket。原因：
- 单向推送够用（服务端 → 浏览器）
- HTMX 原生支持 SSE（`hx-ext="sse"`）
- Go 标准库直接支持，不需要第三方库
- 审批操作走普通 POST，不需要双向通道

#### 8.4.2 审批看板（Approval Board）

```html
<!-- HTMX 实时审批看板 -->
<div id="approval-board" hx-ext="sse"
     sse-connect="/api/v1/events/stream?topic=hitl.*"
     sse-swap="approval-update">

  <!-- 待审批列 -->
  <div class="column pending">
    <h3>Pending <span class="badge">3</span></h3>

    <div class="card high-risk" id="apv-003">
      <div class="risk-indicator red"></div>
      <div class="minister">backend-claude</div>
      <div class="operation">curl api.stripe.com</div>
      <div class="timer" data-timeout="2026-04-08T12:17:00Z">7:00</div>
      <div class="actions">
        <button hx-post="/api/v1/approvals/apv-003"
                hx-vals='{"action":"approve"}'
                hx-swap="outerHTML"
                hx-target="#apv-003">
          Approve
        </button>
        <button hx-post="/api/v1/approvals/apv-003"
                hx-vals='{"action":"deny"}'
                hx-swap="outerHTML"
                hx-target="#apv-003"
                hx-prompt="Reason for denial?">
          Deny
        </button>
      </div>
    </div>
    <!-- ...more cards -->
  </div>

  <!-- 已处理列 -->
  <div class="column decided">
    <h3>Decided</h3>
    <!-- approved/denied items move here via SSE swap -->
  </div>
</div>
```

**交互特点**：
- 看板式布局（Kanban），pending → decided 实时移动
- 超时倒计时用 JS `setInterval` 客户端渲染，不轮询服务器
- 批量操作：顶部 "Approve All" 按钮，`hx-post` 批量端点
- 浏览器通知：高风险项到达时触发 `Notification.requestPermission()`

#### 8.4.3 计划审核视图（Plan Review）

```
┌──────────────────────────────────────────────────────┐
│  Session: auth-refactor                              │
│  Topology: pipeline                                  │
│                                                      │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐       │
│  │ bill-a1b2│───▶│ bill-c3d4│    │ bill-e5f6│       │
│  │ JWT重写  │    │ Token刷新│    │ 删旧表⚠️ │       │
│  │→backend  │───▶│→frontend │    │→backend  │       │
│  └──────────┘    └──────────┘    └──────────┘       │
│       │                               ▲              │
│       └───────────────────────────────┘              │
│                                                      │
│  [Approve All]  [Edit Plan]  [Reject]               │
└──────────────────────────────────────────────────────┘
```

**GUI 独有能力**：
- DAG 可视化（用 D3.js 或 Mermaid 渲染 Bill 依赖图）
- 拖拽调整 Bill 分配（拖动 Bill 到不同 Minister）
- 点击 Bill 展开详情和编辑

CLI/TUI 做不到图形化 DAG，这是 GUI 的核心差异化价值。

#### 8.4.4 输出审核视图（Diff Review）

```
┌──────────────────────────────────────────────────────┐
│  Bill: bill-a1b2  "重写 JWT middleware"               │
│  Status: enacted → pending review                    │
│                                                      │
│  ┌─ Files Changed (7) ────────────────────────────┐  │
│  │ ▼ internal/auth/jwt.go         +89 -45         │  │
│  │   internal/auth/middleware.go   +67 -32         │  │
│  │   internal/auth/refresh.go     +112 (new)      │  │
│  │   ...                                          │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  ┌─ Diff ─────────────────────────────────────────┐  │
│  │  42 │ -func validateToken(t string) bool {     │  │
│  │  42 │ +func validateToken(t string) (Claims, e │  │
│  │  43 │ +    claims, err := parseJWT(t)          │  │
│  │     │  ...                                     │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  ┌─ Review ───────────────────────────────────────┐  │
│  │  Comment: [                                  ] │  │
│  │  [Approve ✓]  [Reject ✗]  [Request Changes]   │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

**实现**：服务端渲染 Diff HTML（`go-diff` 库），HTMX 按文件懒加载（避免大 Diff 阻塞页面）。

#### 8.4.5 Shadow 仪表盘（Shadow Dashboard）

`out-of-the-loop` 模式下的实时监控页面。

```
┌──────────────────────────────────────────────────────┐
│  Shadow Mode: auth-refactor         🟢 Running       │
├──────────────────────────────────────────────────────┤
│                                                      │
│  Timeline                                            │
│  ─────────                                           │
│  12:03 ✅ bill-a1b2 enacted      backend-claude 8m   │
│  12:05 ✅ bill-c3d4 enacted      frontend-claude 6m  │
│  12:07 ⚠️ bill-e5f6 slow         15m (expect 8m)     │
│  12:09 🔴 bill-e5f6 ALERT        stuck, recovery ✗   │
│                                                      │
│  ┌─ Alerts ────────────────────────────────────────┐ │
│  │ 🔴 Session PAUSED — manual intervention needed  │ │
│  │    [Resume]  [Abort]  [Inspect bill-e5f6]       │ │
│  └─────────────────────────────────────────────────┘ │
│                                                      │
│  Stats: 2/3 bills done │ Avg: 7m │ Quality: 0.82    │
└──────────────────────────────────────────────────────┘
```

**实时推送**：SSE `topic=shadow.*` 事件驱动时间线自动追加。ALERT 时浏览器弹窗通知。

#### 8.4.6 API 端点规划

| 端点 | 方法 | 描述 | 优先级 |
|------|------|------|--------|
| `/api/v1/approvals` | GET | 审批队列（支持 `?status=pending` 过滤） | P0 |
| `/api/v1/approvals/:id` | POST | approve/deny（body: `{action, reason}`） | P0 |
| `/api/v1/approvals/batch` | POST | 批量 approve/deny | P1 |
| `/api/v1/approvals/:id/diff` | GET | 获取关联 Bill 的 Diff | P1 |
| `/api/v1/shadow/status` | GET | Shadow 模式当前状态 | P1 |
| `/api/v1/shadow/alerts` | GET | 告警列表 | P1 |
| `/api/v1/shadow/resume` | POST | 恢复暂停的 Session | P1 |
| `/api/v1/events/stream` | GET (SSE) | 实时事件流（`?topic=hitl.*`） | P0 |
| `/api/v1/sessions/:id/plan` | GET | 获取 Session 计划（含 DAG） | P2 |
| `/api/v1/sessions/:id/plan` | POST | approve/reject/edit 计划 | P2 |

### 8.5 跨界面一致性保障

#### 8.5.1 状态同步

```
场景：用户在 TUI 中 approve 了 apv-001，同时 Web 浏览器开着。

1. TUI approve → Store: UPDATE approval_queue SET status='approved'
2. Store 触发 Event: hitl.op_auth.approved
3. SSE 推送 event 到 Web → HTMX swap apv-001 到 "decided" 列
4. 下次 TUI 刷新（10s）→ apv-001 从 pending 列表消失

无冲突：先写入的 decided_at 生效，后到的操作发现 decided_at IS NOT NULL → 提示"已被处理"。
```

#### 8.5.2 冲突处理

乐观锁策略，不需要分布式锁：

```sql
-- approve 操作的 SQL
UPDATE approval_queue
SET status = 'approved', decided_by = ?, decided_at = CURRENT_TIMESTAMP
WHERE id = ? AND decided_at IS NULL;

-- affected rows = 0 → 已被其他界面处理，返回 409 Conflict
```

#### 8.5.3 通知优先级

不同界面的通知能力不同，设定降级策略：

| 场景 | 优先通知渠道 | 降级 |
|------|------------|------|
| 用户正在使用 TUI | TUI 模态/bell | — |
| 用户开着 Web 浏览器 | 浏览器 Notification | SSE 内联 |
| 用户只开了终端（无 TUI） | `hoc approvals` 命令提示 | 文件标记 |
| 用户离线 | webhook（Slack/邮件） | 超时降级 |

**判断方式**：
- TUI 活跃：Whip 检测 `hoc floor` 进程存在
- Web 活跃：SSE 连接存在
- 都不活跃：触发 webhook（如果配置了）

### 8.6 各阶段 HITL 界面实施路径

```
v0.x (当前)
  └─ CLI-only: hoc approve / hoc approvals / hoc review
     └─ 文件协议: approval-request / approval-response
     └─ approval_queue 表 + events 表

v0.4 (TUI 增强)
  └─ hoc floor 新增 Approvals Tab
     └─ 强制确认模态覆盖层
     └─ Shadow 状态栏
     └─ Inline Diff 预览（lipgloss 高亮）

v1.0 (GUI Web)
  └─ HTMX 审批看板
     └─ SSE 实时推送
     └─ DAG 可视化计划审核
     └─ 语法高亮 Diff Review
     └─ Shadow 仪表盘 + 浏览器通知
     └─ 多人审批（decided_by 字段生效）

v1.x (远期)
  └─ 移动端通知（PWA / 原生推送）
  └─ 审批策略引擎（规则化自动审批）
  └─ 审计报表（审批通过率、平均响应时间、超时率）
```

### 8.7 GUI 独有能力（CLI/TUI 做不到的）

| 能力 | 为什么需要 GUI | 价值 |
|------|--------------|------|
| **DAG 可视化** | Bill 依赖关系需要图形渲染 | 计划审核时一眼看清拓扑结构 |
| **拖拽重分配** | 鼠标交互比键盘直觉 | 编辑计划时调整 Bill → Minister 映射 |
| **多人协作** | 多人同时审批同一看板 | 团队场景下分工审批 |
| **审计可视化** | 趋势图、热力图 | 审批通过率、平均响应时间等指标可视化 |
| **移动端触达** | PWA 推送通知 | 人不在电脑前时仍能及时响应高风险审批 |

### 8.8 延展反模式（TUI/GUI 特有）

#### 8.8.1 通知轰炸（Notification Storm）

**症状**：Web 浏览器 + TUI + Slack webhook 同时推送同一条审批 → 用户被同一件事通知三次。

**对策**：通知去重。Event Ledger 记录 `hitl.notification.sent`，带 `channel` 字段。已通知的渠道不重复推送。或者更简单：用户配置优先通知渠道，只走一个。

#### 8.8.2 GUI 依赖导致的可用性降级

**症状**：团队依赖 Web 看板审批，Web 服务挂了 → 审批流程瘫痪。

**对策**：CLI 是底线。任何时候 `hoc approve <id>` 都能工作，不依赖 serve.go。GUI/TUI 是锦上添花，不是必要条件。

#### 8.8.3 实时幻觉（Stale UI）

**症状**：Web 页面长时间没刷新，SSE 断连后重连但丢了中间事件 → 页面状态和实际不一致 → 用户 approve 了一个已超时 deny 的审批。

**对策**：
- SSE 重连时发送 `Last-Event-ID`，服务端从 events 表回放缺失事件
- 每 60 秒全量刷新一次审批列表（HTMX `hx-trigger="every 60s"`）
- approve/deny 响应中返回最新状态，前端以响应为准而非本地状态
