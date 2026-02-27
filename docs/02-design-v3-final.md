# House of Cards — 可落地设计方案

> AI Agent 协作框架：模拟人类政府分工协作的「AI 团队操作系统」
>
> 基于 Gas Town 深度分析后的重新设计
> 版本：v3（2026-02-28）

---

## 0. 设计立场：从 Gas Town 学到什么，改掉什么

### 继承（Gas Town 证明了这些是对的）

| 原则 | Gas Town 的实现 | 我们继承什么 |
|------|----------------|-------------|
| Agent 需要持久身份 | Polecat 三层模型 | ✅ Agent 有 Hansard（议事录），工作记录跨会话积累 |
| 工作状态必须 crash-safe | Git worktree + Dolt | ✅ Git worktree 隔离沙箱（丢掉 Dolt） |
| 系统需要推进力 | GUPP（钩子有活必须干） | ✅ Whip 机制：心跳 + 超时 + 自动重派 |
| 所有操作可归因 | BD_ACTOR 标识 | ✅ 每个 commit/artifact 标注来源 Minister |
| 合并冲突需要专门处理 | Refinery 角色 | ✅ Privy Council（枢密院）仲裁 |
| 生动隐喻 = 压缩的行为规范 | Mayor/Polecat/Hook/Convoy | ✅ 政府隐喻：Speaker/Minister/Whip/Gazette |

### 改掉（Gas Town 踩过的坑）

| Gas Town 的问题 | 根因 | 我们的方案 |
|----------------|------|-----------|
| Dolt 依赖太重 | 用版本化数据库做 issue tracking | **SQLite + Git**（零额外依赖） |
| 强绑 tmux | 用 tmux pane 做 session 管理 | **进程管理抽象层**（tmux 只是一种 backend） |
| 固定星形拓扑 | Mayor→Polecat 单一模式 | **可配置协作拓扑**（线性/并行/流水线/网状） |
| 没有信息凝练 | Agent 间传完整 mail body | **Gazette 协议**（传摘要不传原文） |
| beads CLI 紧耦合 | gt 和 bd 互相依赖 | **单一 CLI（`hoc`），自包含** |

---

## 1. 隐喻体系：为什么用政府隐喻

### 设计原则

> **隐喻不是装饰，隐喻是压缩的行为规范。**

当你告诉 AI「你是 Whip（党鞭）」，它不需要读 500 行 spec 就知道：催人到场、确保执行、惩罚怠工。这个词本身就是 prompt engineering。

Gas Town 用「小镇」隐喻（Mayor/Polecat/Hook）已经证明了这一点。我们用「政府」隐喻，并且解决了一个关键问题：**议会只管立法不管执行，完整政府才有行政能力。**

### 为什么选政府隐喻

| 系统需要的能力 | 政府对应机构 | AI 读到时自动理解的行为 |
|--------------|-------------|---------------------|
| 拆解任务、决策全局 | 议长（Speaker） | 主持会议不亲自干活、协调全局 |
| 实际执行各领域工作 | 部长（Minister） | 各管一摊、术业有专攻 |
| 催促执行、检测摸鱼 | 党鞭（Whip） | 确保到场、确保投票、施压 |
| 合并冲突最终裁决 | 枢密院（Privy Council） | 跨部门争议的最高仲裁 |
| 质量审查 | 委员会（Committee） | 质询、审计、出报告 |
| 凝练信息传递 | 公报（Gazette） | 官方凝练公告，不是逐字记录 |
| 完整审计记录 | 议事录（Hansard） | 完整记录，可追溯，是简历 |
| 一批相关工作 | 会期（Session） | 一届会期处理一批议案 |
| 单个工作项 | 议案（Bill） | 有明确的生命周期和表决流程 |
| 替换掉线 Agent | 补选（By-election） | 自然语义，无需额外解释 |

---

## 2. 核心概念（9 个角色 / 实体）

```
House of Cards
│
│  ══════════════════════════════
│  立法层（决策 / 规划）
│  ══════════════════════════════
│
├── Speaker（议长）
│   决策者。接收人类需求，拆解为 Bill，选择协作拓扑。
│   可以有多个候选（v2 竞标制）。v1 只有一个。
│
├── Bill（议案）
│   一个可追踪的原子工作项。
│   生命周期：Draft → Reading → Committee → Enacted → Royal Assent
│
├── Session（会期）
│   一批相关 Bill 的集合 + 协作拓扑定义。
│   "本届会期我们要完成认证系统。"
│
│  ══════════════════════════════
│  行政层（执行）
│  ══════════════════════════════
│
├── Cabinet（内阁 / 专家池）
│   所有 Minister 的注册表。
│
├── Minister（部长）
│   实际干活的 AI Agent，各管一个领域。
│   持久身份 + 短暂会话。Hansard 是它的简历。
│
├── Whip（党鞭）
│   系统推进力。纯代码层（v1），可选 AI 升级（v2）。
│   心跳检测 + stuck 检测 + 自动重派 + 补选触发。
│
│  ══════════════════════════════
│  司法层（审核 / 仲裁）
│  ══════════════════════════════
│
├── Committee（委员会）
│   代码审查 / 质量审核角色。
│   可以是专门的 reviewer Minister，也可以是 Speaker 自审。
│
├── Privy Council（枢密院）
│   合并冲突的最终仲裁。Dispatcher 内置的合并调度逻辑。
│
│  ══════════════════════════════
│  信息层（沟通 / 记录）
│  ══════════════════════════════
│
├── Gazette（公报）
│   Agent 间传递的凝练信息。核心差异化特性。
│   "后端部长完成了 API，公报已发布，前端部长请查收。"
│
└── Hansard（议事录）
    完整审计记录 + Agent 工作简历。
    永久积累，不可篡改。
```

### 概念对照表

| House of Cards | Gas Town | 行业通用术语 |
|---------------|----------|-------------|
| Speaker | Mayor | Orchestrator |
| Minister | Polecat / Crew | Worker Agent |
| Bill | Bead / Issue | Task / Work Item |
| Session | Convoy | Batch / Sprint |
| Cabinet | Expert Pool | Agent Registry |
| Whip | GUPP + Witness + Boot | Watchdog / Scheduler |
| Committee | Witness (review) | Reviewer |
| Privy Council | Refinery | Merge Coordinator |
| Gazette | Mail (but no condensation) | Brief / Summary |
| Hansard | CV chain | Audit Log / Work History |

---

## 3. 架构总览

```
┌───────────────────────────────────────────────────────────┐
│                     House of Cards                         │
│                                                           │
│  ┌──────────┐     ┌──────────────┐     ┌──────────────┐  │
│  │ Speaker  │────▶│    Whip      │────▶│   Cabinet    │  │
│  │ (AI)     │     │ (Go daemon)  │     │              │  │
│  │          │◀────│              │◀────│ [Minister 1] │  │
│  │ 拆 Bill  │     │ • 拓扑引擎   │     │ [Minister 2] │  │
│  │ 选拓扑   │     │ • 心跳监测   │     │ [Minister 3] │  │
│  │ 读 Gazette│     │ • 派发调度   │     │ [Minister N] │  │
│  └──────────┘     │ • Gazette 路由│     └──────────────┘  │
│                   │ • 合并仲裁   │                        │
│                   │  (Privy C.)  │                        │
│                   └──────┬───────┘                        │
│                          │                                │
│                   ┌──────┴──────┐                         │
│                   │ State Layer  │                        │
│                   │ SQLite + Git │                        │
│                   └─────────────┘                         │
└───────────────────────────────────────────────────────────┘
```

### 三权分立映射

| 层 | 政府对应 | 职责 | 实现方式 |
|----|---------|------|---------|
| **立法层** | 议会 | 拆任务、选拓扑、读 Gazette、做判断 | AI Agent（Speaker） |
| **行政层** | 内阁 + 党鞭 | 执行任务、推进进度、健康检查 | Ministers（AI）+ Whip（Go daemon） |
| **司法层** | 委员会 + 枢密院 | 质量审核、合并仲裁 | Committee（AI）+ Privy Council（Go 合并逻辑） |

---

## 4. 状态持久化方案

### 4.1 原则：SQLite + Git，零额外进程

```
操作状态（高频读写，不需要版本历史）
    → SQLite 单文件数据库
    → Bill 状态、Minister 心跳、队列、Gazette 收件箱

工作产物（需要版本控制、可回溯）
    → Git 仓库本身
    → 代码变更、Gazette 存档、Hansard 记录
```

### 4.2 SQLite Schema

```sql
-- Minister 注册表（内阁花名册）
CREATE TABLE ministers (
    id          TEXT PRIMARY KEY,         -- "backend-claude"
    title       TEXT,                     -- "后端部长" / "Minister of Backend"
    runtime     TEXT NOT NULL,            -- "claude-code" | "codex" | "cursor"
    skills      TEXT,                     -- JSON: ["go", "python", "api-design"]
    status      TEXT DEFAULT 'offline',   -- offline | idle | working | stuck
    pid         INTEGER,
    worktree    TEXT,
    heartbeat   DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Bill 表（议案）
CREATE TABLE bills (
    id          TEXT PRIMARY KEY,         -- "bill-a1b2c3"
    session_id  TEXT,                     -- 所属 Session
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT DEFAULT 'draft',     -- draft | reading | committee | enacted | royal_assent | failed
    assignee    TEXT REFERENCES ministers(id),
    depends_on  TEXT,                     -- JSON: 依赖的 bill id 列表
    branch      TEXT,                     -- git 分支名
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME
);

-- Session 表（会期）
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,         -- "session-x7k2m"
    title       TEXT NOT NULL,
    topology    TEXT NOT NULL,            -- "parallel" | "pipeline" | "tree" | "mesh"
    config      TEXT,                     -- JSON/TOML: 拓扑 DAG 定义
    status      TEXT DEFAULT 'active',    -- active | completed | dissolved
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Gazette 表（公报）
CREATE TABLE gazettes (
    id          TEXT PRIMARY KEY,
    from_minister TEXT,
    to_minister   TEXT,                   -- NULL = 全体公报
    bill_id     TEXT,
    type        TEXT,                     -- "completion" | "handoff" | "help" | "review" | "conflict"
    summary     TEXT NOT NULL,
    artifacts   TEXT,                     -- JSON: 相关文件路径
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    read_at     DATETIME
);

-- Hansard 表（议事录 / Minister 工作简历）
CREATE TABLE hansard (
    id          TEXT PRIMARY KEY,
    minister_id TEXT NOT NULL,
    bill_id     TEXT NOT NULL,
    outcome     TEXT,                     -- "enacted" | "failed" | "partial"
    duration_s  INTEGER,
    skills_used TEXT,                     -- JSON
    quality     REAL,                     -- 0.0-1.0
    notes       TEXT,                     -- Committee 审查意见
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 4.3 目录结构

```
~/house/                            工作区根目录
├── .hoc/
│   ├── state.db                    SQLite（所有操作状态）
│   ├── config.toml                 全局配置
│   └── speaker/
│       └── context.md              Speaker 持久记忆
├── projects/
│   └── <project>/
│       ├── main/                   主分支（clone）
│       ├── chambers/               Minister 工作沙箱
│       │   ├── backend-claude/     ← git worktree
│       │   ├── frontend-cursor/    ← git worktree
│       │   └── reviewer-gemini/    ← git worktree
│       └── gazettes/               公报存档（git tracked）
│           ├── bill-a1b2c3.md
│           └── bill-d4e5f6.md
└── hansard/                        议事录存档
    ├── backend-claude.md           Minister 工作简历
    └── frontend-cursor.md
```

注意 worktree 目录叫 **chambers（议事厅）**——每个 Minister 在自己的议事厅里工作。

### 4.4 崩溃恢复（补选机制）

```
Minister 崩溃（心跳超时）
    │
    ├─ Whip 检测到异常
    │
    ├─ 检查 chamber（worktree）状态
    │   ├─ 有未提交代码 → git stash 保存
    │   ├─ 有已提交未推送 → 记录分支名
    │   └─ 干净 → 直接标记 Bill 可重派
    │
    ├─ 标记该 Minister 为 offline
    ├─ 标记 Bill 回退为 reading（待重新分配）
    │
    ├─ 自动生成 Handoff Gazette：
    │   "前任部长做到了 X，崩溃前代码在分支 Y，请接手。"
    │
    └─ Whip 触发「补选」（By-election）：
        ├─ 有空闲 Minister 且技能匹配 → 自动分配（带 Gazette）
        ├─ 无空闲 Minister → 等待，或通知 Speaker 决策
        └─ 同一 Bill 失败超过 max_retries → 升级给 Speaker
```

---

## 5. Bill 生命周期（议案从提出到通过）

```
Draft（草案）
    │  Speaker 提出议案，描述需求
    ▼
First Reading（一读）
    │  Whip 将 Bill 分配给匹配的 Minister
    │  Minister 收到 Bill + 上游 Gazette
    ▼
In Progress（审议中）
    │  Minister 在自己的 Chamber 中工作
    │  Whip 持续监测心跳
    ▼
Committee（委员会审查）
    │  Minister 完成后发布 Gazette
    │  Committee（reviewer）审查质量
    │  ├─ 通过 → 继续
    │  └─ 退回 → 回到 In Progress，附 Review Gazette
    ▼
Enacted（立法通过）
    │  代码合并到主分支（Privy Council 处理冲突）
    ▼
Royal Assent（御准）
    │  Session 层面确认，记入 Hansard
    └─ Bill 完成
```

这套生命周期比 Gas Town 的 `open → in_progress → closed` 更丰富，而且**每个阶段的名字就暗示了该做什么**——AI 读到「Committee」就知道这是审查阶段。

---

## 6. Gazette 协议（公报——信息凝练层）

### 6.1 核心思想

> **人类政府用公报传达决议，不会把整个辩论记录发给每个部门。**
> **Minister 之间也不应该传 diff，而是传凝练的公报。**

### 6.2 Gazette 标准格式

```markdown
# Gazette: [Bill Title]
> From: Minister of Backend | Bill: bill-a1b2c3 | Date: 2026-02-28

## 决议（3 句话以内）
实现了 JWT 认证的 3 个 API endpoint，支持注册、登录、刷新 token。

## 变更清单
- `api/auth/register.go` — 新增，用户注册
- `api/auth/login.go` — 新增，JWT 签发
- `api/auth/middleware.go` — 新增，认证中间件

## 接口契约（下游部门需要知道的）
- POST /api/auth/register → { token: string }
- POST /api/auth/login → { token: string, refresh: string }
- 中间件从 Authorization header 解析 JWT

## 假设与风险
- 假设 PostgreSQL（MySQL 需改 migration）
- 未实现 rate limiting

## 状态
✅ Enacted | 测试通过 | 分支: feat/auth-backend
```

### 6.3 Gazette 类型

| 类型 | 政治隐喻 | 触发场景 | 内容 |
|------|---------|---------|------|
| **Completion Gazette** | 立法公报 | Bill 完成 | 做了什么、改了什么、接口契约 |
| **Handoff Gazette** | 交接备忘录 | Minister 会话即将结束/崩溃 | 做到哪了、下一步做什么 |
| **Help Gazette** | 质询请求 | Minister 遇到困难 | 什么问题、试过什么、需要什么帮助 |
| **Review Gazette** | 委员会报告 | 审查完成 | 通过/退回、需修改项、质量评分 |
| **Conflict Gazette** | 仲裁公告 | 合并冲突 | 冲突文件、各方摘要、建议方案 |

---

## 7. Whip 设计（党鞭——系统推进力）

### 7.1 为什么 Whip 是关键

Gas Town 的教训：**多 agent 系统最大的敌人不是 agent 出错，而是 agent 不动。**

Gas Town 用了三层机制解决（Daemon → Boot → Deacon），我们用一个统一的 Whip 角色。

### 7.2 Whip 的职责

```
Whip（Go daemon 进程）

每 10 秒：
    ├─ 三线鞭令（Three-Line Whip）—— 最高优先级检查
    │   └─ 所有 working Minister 心跳是否正常
    │       ├─ 正常 → 继续
    │       ├─ 超时 30s → 标记 stuck
    │       └─ 超时 5min → 触发补选（By-election）
    │
    ├─ 议程推进（Order Paper）
    │   └─ 检查 Session DAG 中新就绪的 Bill
    │       └─ 有就绪 Bill + 有 idle Minister → 自动分配
    │
    └─ 公报投递（Gazette Dispatch）
        └─ 有未投递的 Gazette → 路由到目标 Minister

每 60 秒：
    ├─ Speaker 巡视（Speaker's Patrol）
    │   └─ 生成全局状态摘要给 Speaker
    │   └─ Speaker 决策：调整拓扑 / 重派 / 升级
    │
    └─ 议事录更新（Hansard Update）
        └─ 持久化运行统计到 hansard 表
```

### 7.3 v1 纯代码，v2 可选 AI

| 场景 | v1（纯代码） | v2（AI 增强） |
|------|------------|--------------|
| 心跳超时 | 机械判断：超时 = stuck | AI 判断：看 tmux 输出，区分"在思考"和"真卡住" |
| 任务分配 | 技能匹配 + 负载均衡 | AI 分析历史 Hansard，选最适合的 Minister |
| 冲突处理 | 标记冲突，生成 Conflict Gazette | AI 尝试理解冲突语义，给出合并建议 |

---

## 8. Minister 生命周期

### 8.1 注册声明

```toml
# ministers/backend-claude.toml

[minister]
name = "backend-claude"
title = "Minister of Backend"       # 有个头衔，AI 读到时更有角色感
runtime = "claude-code"
command = "claude"
args = ["--model", "sonnet"]

[portfolio]                          # portfolio = 部长管辖范围
skills = ["go", "python", "sql", "api-design"]
max_concurrent = 1
context_limit = "200k"

[discipline]                         # 党鞭需要的纪律参数
heartbeat_interval = "10s"
stuck_threshold = "5m"
max_retries = 2
```

### 8.2 Minister 状态机

```
              ┌───────────┐
              │  offline  │◀─── 已注册未启动 / 被罢免 / 心跳超时
              └─────┬─────┘
                    │ hoc minister summon
                    ▼
              ┌───────────┐
      ┌───── │   idle    │◀─── 等待 Bill 分配（"在野"）
      │      └─────┬─────┘
      │            │ Whip 分配 Bill（"入阁"）
      │            ▼
      │      ┌───────────┐
      │      │  working  │──── 在 Chamber 中执行 Bill
      │      └──┬─────┬──┘
      │         │     │
      │    Bill enacted  stuck/超时
      │         │     │
      │         ▼     ▼
      │     ┌──────┐ ┌───────┐
      │     │ idle │ │ stuck │── Whip 介入
      │     └──────┘ └───┬───┘
      │                  │ 补选 / 重启
      └──────────────────┘
```

### 8.3 Runtime 抽象

```go
// Runtime 接口：任何 AI CLI 都可以接入
type Runtime interface {
    // 在 chamber（worktree）中启动 minister session
    Summon(chamber string, bill Bill, gazettes []Gazette) (Session, error)

    // 检查 session 是否在运行
    IsSeated(session Session) bool

    // 优雅结束
    Dismiss(session Session) error

    // 向 minister 发送消息（nudge / gazette）
    Dispatch(session Session, message string) error
}
```

注意方法名也用了议会隐喻：`Summon`（传召）、`IsSeated`（是否就座）、`Dismiss`（休会）。

---

## 9. 协作拓扑引擎

### 9.1 Session 定义 DSL

```toml
# sessions/build-auth.toml — 定义一届会期

[session]
title = "Build Authentication System"
topology = "tree"

[[bills]]
id = "auth-api"
portfolio = "go"                    # 需要 Go 技能的 Minister
motion = "Build JWT auth API endpoints"

[[bills]]
id = "auth-ui"
portfolio = "react"
motion = "Build login/signup UI components"

[[bills]]
id = "auth-db"
portfolio = "sql"
motion = "Design user table schema and migrations"

# 汇合 Bill（依赖前面的 Bill 全部 enacted）
[[bills]]
id = "integration"
portfolio = "fullstack"
motion = "Integration testing and conflict resolution"
depends_on = ["auth-api", "auth-ui", "auth-db"]

# 终审
[[bills]]
id = "final-review"
portfolio = "reviewer"
motion = "Final code review and quality gate"
depends_on = ["integration"]
```

### 9.2 四种拓扑

```
线性 Debate（辩论式）        并行 Division（分组表决）
A ──▶ B ──▶ C              ┌──▶ A
                           ├──▶ B
适合：bug 修复              ├──▶ C
                           └──▶ merge
                           适合：前后端并行

流水线 Reading（逐读式）     网状 Caucus（党团协商）
A ──▶ B ──▶ C ──▶ D        A ◀──▶ B
每步输出是下步输入              ▲      ▲
                               └──▶ C ◀┘
适合：数据管道                 适合：架构设计讨论
```

---

## 10. Privy Council（枢密院——合并仲裁）

并行 Bill 完成后必须合并代码。Privy Council 是 Whip 内置的合并调度逻辑：

```
并行 Bill 全部 enacted
    │
    ├─ Whip 收集所有 Chamber 的分支名
    │
    ├─ 尝试自动合并到 integration 分支
    │   ├─ 无冲突 → Royal Assent，Bill 合并成功
    │   └─ 有冲突 → 发布 Conflict Gazette
    │
    └─ Conflict Gazette 分配给指定 Minister 解决
        └─ 解决后重新提交 → Privy Council 再次尝试
```

---

## 11. CLI 设计

```bash
# ══════════ 工作区 ══════════
hoc init ~/my-house                     # 建立新政府
hoc project add myapp <repo-url>        # 添加管辖项目

# ══════════ 内阁管理 ══════════
hoc minister appoint backend-claude \   # 任命部长
    --runtime claude-code \
    --portfolio go,python \
    --title "Minister of Backend"
hoc minister summon backend-claude      # 传召部长（启动 session）
hoc minister dismiss backend-claude     # 休会（停止 session）
hoc cabinet list                        # 查看内阁花名册
hoc cabinet reshuffle                   # 内阁改组（重新分配 Minister）

# ══════════ 会期与议案 ══════════
hoc session open build-auth.toml        # 开启会期
hoc session status                      # 会期进度
hoc bill list                           # 所有议案
hoc bill show <bill-id>                 # 议案详情

# ══════════ Speaker ══════════
hoc speaker summon                      # 传召议长（交互模式）
hoc speaker auto                        # 议长自动编排模式

# ══════════ 监控 ══════════
hoc floor                               # 议会大厅——全局状态 TUI
hoc gazette list                        # 公报流转
hoc hansard <minister>                  # 查看部长履历

# ══════════ 运维 ══════════
hoc whip report                         # 党鞭状态报告
hoc doctor                              # 健康检查
hoc dissolve <session-id>               # 解散会期（清理资源）
```

### 命令语义一览

| 命令 | 政治含义 | 实际操作 |
|------|---------|---------|
| `hoc minister appoint` | 任命部长 | 注册 Agent 到内阁 |
| `hoc minister summon` | 传召部长 | 启动 Agent session |
| `hoc minister dismiss` | 免职/休会 | 停止 Agent session |
| `hoc session open` | 开启新会期 | 创建 Campaign + 拓扑 |
| `hoc session dissolve` | 解散会期 | 清理 worktree + 归档 |
| `hoc cabinet reshuffle` | 内阁改组 | 重新分配 Minister |
| `hoc floor` | 进入议会大厅 | TUI 实时监控 |
| `hoc whip report` | 党鞭汇报 | 健康状态 + 进度 |

---

## 12. Speaker 设计

### 12.1 单 Speaker 模式（v1）

```toml
# .hoc/config.toml

[speaker]
runtime = "claude-code"
model = "opus"
context_file = ".hoc/speaker/context.md"
```

Speaker 的持久记忆（`context.md`）每次启动注入：

```markdown
# Speaker Context（议长备忘录）

## 政府现状
- 项目：MyApp — Go 后端 + React 前端
- 活跃会期：build-auth（3/5 Bills enacted）
- 内阁：3 位部长在任，1 位离线

## 内阁档案
- backend-claude：Go/Python 强，12 项议案通过，成功率 91%
- frontend-cursor：React/TS 强，8 项通过，成功率 88%
- reviewer-gemini：审查准确，但处理速度慢

## 近期决策
- 14:30：auth-api 补选，指派 backend-claude 接手（前任崩溃）
- 15:10：auth-ui enacted，质量评分 0.85，公报已路由
```

### 12.2 多 Speaker 竞标制（v2）

```
新 Session 到来
    │
    ├─ 候选 Speaker 各提一份施政纲领（拆解方案）
    │   ├─ Speaker A：3 个 Bill 并行
    │   ├─ Speaker B：5 步流水线
    │   └─ Speaker C：2 个大 Bill
    │
    ├─ 信任投票（评估函数 / 人类选择）
    │
    └─ 胜选者获得该 Session 执行权
```

---

## 13. 端到端示例：完整的一届会期

```
人类："我要做一个用户认证系统"

══ 立法 ══

Speaker（议长）开启新 Session（会期），起草三个 Bill（议案）：
  - Bill #1: auth-api  — 后端 JWT API
  - Bill #2: auth-ui   — 前端登录页
  - Bill #3: auth-test — 集成测试

Speaker 选择 Division（并行表决）拓扑：
  #1 和 #2 并行，#3 依赖前两者。

══ 行政 ══

Whip（党鞭）执行 First Reading（一读）：
  - Bill #1 → Minister of Backend（backend-claude）
  - Bill #2 → Minister of Frontend（frontend-cursor）

两位 Minister 各自进入 Chamber（议事厅 / git worktree）工作。
Whip 每 10 秒检查心跳。

══ 信息流转 ══

backend-claude 完成 Bill #1，发布 Gazette（公报）：
  "JWT 三个端点已完成。POST /api/auth/login → { token, refresh }。"

frontend-cursor 收到 Gazette，了解后端接口，继续完成 Bill #2。

══ 司法 ══

两个 Bill 都完成 → Privy Council（枢密院）合并代码：
  - 无冲突 → 自动合并
  - 有冲突 → Conflict Gazette，指派 Minister 解决

Committee（委员会 / reviewer-gemini）对合并后的代码审查：
  - 通过 → 发布 Review Gazette（质量评分 0.9）
  - 退回 → 退回 Gazette，注明需修改项

Bill #3（集成测试）就绪 → Whip 分配给 testing Minister。

══ 结束 ══

所有 Bill enacted → Session 收到 Royal Assent → 会期结束。
各 Minister 的工作记入 Hansard（议事录）。

人类看到：认证系统已完成，所有代码已合并到 main 分支。
```

---

## 14. MVP 路线图

### Phase 0：脚手架（1 周）

```
目标：hoc init + hoc project add + hoc minister appoint 能跑通

- [ ] CLI 骨架（Cobra，命令前缀 hoc）
- [ ] SQLite 初始化
- [ ] config.toml 解析
- [ ] git worktree（chamber）创建/清理
```

### Phase 1：单 Minister 能跑（2 周）

```
目标：Speaker 把一个 Bill 交给一个 Minister，完成后生成 Gazette

- [ ] ClaudeCodeRuntime 实现（Summon/IsSeated/Dismiss）
- [ ] Bill 创建与分配
- [ ] Gazette 模板注入（让 AI 知道要写 Gazette）
- [ ] 完成信号检测
- [ ] Gazette 存储与展示
```

### Phase 2：并行 + Whip（2 周）

```
目标：一个 Session 拆成 2 个并行 Bill，Whip 调度，各自完成后 Gazette 路由

- [ ] Session TOML 解析 + DAG 引擎
- [ ] Whip daemon（心跳检测 + 派发调度）
- [ ] 并行 Bill 派发
- [ ] Gazette 路由到下游
- [ ] Privy Council：自动 merge + conflict 检测
```

### Phase 3：崩溃恢复 + Speaker AI（2 周）

```
目标：Minister 崩溃后补选自动生效；Speaker 作为 AI 自动做决策

- [ ] 心跳超时 → stuck 检测
- [ ] 补选流程（stash + Handoff Gazette + 重派）
- [ ] Speaker AI session
- [ ] Speaker context.md 持久化
- [ ] Hansard 记录
```

### Phase 4：完善体验（2 周）

```
目标：全拓扑支持、多 runtime、TUI 监控

- [ ] Pipeline / Tree / Mesh 拓扑
- [ ] Codex / Cursor runtime 接入
- [ ] hoc floor TUI（议会大厅实时监控）
- [ ] Committee 审查流程
- [ ] Hansard 质量评分与技能匹配
```

---

## 15. 技术栈

| 组件 | 选择 | 理由 |
|------|------|------|
| 主语言 | **Go** | 单二进制分发、并发好、可参考 Gas Town |
| CLI 框架 | **Cobra** | Go 生态标准 |
| 数据库 | **SQLite**（modernc.org/sqlite，CGo-free） | 零部署 |
| 沙箱 | **Git worktree**（Chambers） | Gas Town 验证过 |
| 进程管理 | **os/exec + 可选 tmux** | 不强绑 |
| TUI | **BubbleTea** | Go 生态最好 |
| 配置 | **TOML** | 人类可读 |

---

## 16. 风险与缓解

| 风险 | 缓解 |
|------|------|
| Gazette 质量差（AI 写的摘要不准确） | Gazette 模板 + 必填字段校验 + Committee 交叉审查 |
| Minister 不按规矩来（不写 Gazette、不发完成信号） | 通过 CLAUDE.md 注入行为规范 + Whip 超时兜底 |
| 不同 Runtime 行为差异大 | Runtime 接口约束输入输出，差异通过 adapter 吸收 |
| SQLite 并发不够 | WAL 模式；真不够再换 PostgreSQL |
| 隐喻记不住 | `hoc help` 输出完整隐喻对照表；README 开头放术语表 |

---

## 17. 最终对比

| 维度 | Gas Town | House of Cards |
|------|----------|---------------|
| 隐喻 | 小镇（Mayor/Polecat/Hook） | **政府**（Speaker/Minister/Whip/Gazette） |
| 依赖 | Dolt + beads CLI + tmux | **仅 Git**（SQLite 内嵌） |
| 拓扑 | 固定星形 | **可配置 DAG** |
| 信息凝练 | 无 | **Gazette 协议** |
| 推进力 | GUPP + Witness + Boot（三层） | **Whip**（单一角色，职责清晰） |
| 合并 | Refinery（完整角色） | **Privy Council**（Whip 内置） |
| 审计 | CV chain（beads 存储） | **Hansard**（SQLite + git） |
| Agent 运行时 | 初始绑 Claude，后期扩展 | **Day 1 多 runtime** |
| CLI 前缀 | `gt` | **`hoc`** |

---

*House of Cards：在纸牌搭的房子里，每张牌都知道自己该撑哪面墙。*

*下一步：Phase 0，建立新政府。*
