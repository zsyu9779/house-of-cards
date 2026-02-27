# Cyber Parliament v2 — 可落地设计方案

> AI Agent 协作框架：模拟人类社会分工协作的「AI 团队操作系统」
>
> 基于 Gas Town 深度分析后的重新设计
> 版本：v2（2026-02-28）

---

## 0. 设计立场：从 Gas Town 学到什么，改掉什么

### 继承（Gas Town 证明了这些是对的）

| 原则 | Gas Town 的实现 | 我们继承什么 |
|------|----------------|-------------|
| Agent 需要持久身份 | Polecat 三层模型 | ✅ Agent 有 CV，工作记录跨会话积累 |
| 工作状态必须 crash-safe | Git worktree + Dolt | ✅ Git worktree 隔离沙箱（丢掉 Dolt） |
| 系统需要推进力 | GUPP（钩子有活必须干） | ✅ 心跳 + 超时 + 自动重派 |
| 所有操作可归因 | BD_ACTOR 标识 | ✅ 每个 commit/artifact 标注来源 Agent |
| 合并冲突需要专门处理 | Refinery 角色 | ✅ 合并协调器（但更轻量） |

### 改掉（Gas Town 踩过的坑）

| Gas Town 的问题 | 根因 | 我们的方案 |
|----------------|------|-----------|
| Dolt 依赖太重 | 用版本化数据库做 issue tracking | **SQLite + Git**（零额外依赖） |
| 强绑 tmux | 用 tmux pane 做 session 管理 | **进程管理抽象层**（tmux 只是一种 backend） |
| 概念术语过重 | Polecat/Bead/Wisp/Hook/Convoy... | **用行业通用术语**（Worker/Task/Session/Queue） |
| 固定星形拓扑 | Mayor→Polecat 单一模式 | **可配置协作拓扑**（线性/并行/流水线/网状） |
| 没有信息凝练 | Agent 间传完整 mail body | **Condensed Brief 协议**（传摘要不传原文） |
| beads CLI 紧耦合 | gt 和 bd 互相依赖 | **单一 CLI，自包含** |

---

## 1. 核心概念（6 个，不能再多）

```
Parliament（工作区）
    │
    ├── Speaker（编排者）—— 拆任务、选拓扑、协调进度
    │
    ├── Expert Pool（专家池）—— 注册的 Agent 运行时实例
    │   ├── Expert: "backend" (Claude Code)
    │   ├── Expert: "frontend" (Cursor)
    │   └── Expert: "reviewer" (Gemini)
    │
    ├── Mission（任务单元）—— 一个可追踪的工作项
    │
    ├── Campaign（战役）—— 一组相关 Mission 的批处理
    │
    └── Brief（简报）—— Agent 间传递的凝练信息
```

### 为什么是这 6 个

- **Parliament** = Gas Town 的 Town（工作区根目录）
- **Speaker** = Gas Town 的 Mayor（但可以有多个、可选举）
- **Expert** = Gas Town 的 Polecat + Crew（统一为一个概念，通过 `persistent` 属性区分）
- **Mission** = Gas Town 的 Bead/Issue（但不需要单独的数据库）
- **Campaign** = Gas Town 的 Convoy（批量工作追踪）
- **Brief** = **全新概念**，Gas Town 完全没有

---

## 2. 架构总览

```
┌─────────────────────────────────────────────────────────────┐
│                    Cyber Parliament                          │
│                                                             │
│  ┌──────────┐    ┌──────────────┐    ┌─────────────────┐   │
│  │ Speaker  │───▶│  Dispatcher  │───▶│  Expert Pool    │   │
│  │ (AI)     │    │  (Go/Rust)   │    │                 │   │
│  │          │◀───│              │◀───│  [Claude Code]  │   │
│  │ 拆任务   │    │ • 拓扑引擎   │    │  [Codex]        │   │
│  │ 选拓扑   │    │ • 进度追踪   │    │  [Gemini]       │   │
│  │ 读 Brief │    │ • 健康检查   │    │  [Cursor]       │   │
│  └──────────┘    │ • Brief 路由 │    │  [Any CLI]      │   │
│                  └──────────────┘    └─────────────────┘   │
│                         │                                   │
│                  ┌──────┴──────┐                            │
│                  │  State Layer │                           │
│                  │  SQLite +    │                           │
│                  │  Git worktree│                           │
│                  └─────────────┘                            │
└─────────────────────────────────────────────────────────────┘
```

### 三层分离

| 层 | 职责 | 实现 | 说明 |
|----|------|------|------|
| **决策层** | 拆任务、选拓扑、读 Brief、做判断 | AI Agent（Speaker） | **AI 做决策**（ZFC 原则） |
| **传输层** | 派发任务、路由 Brief、追踪进度、健康检查 | Go/Rust 代码（Dispatcher） | **代码做传输**（确定性、可靠） |
| **状态层** | 持久化任务状态、Agent 身份、工作历史 | SQLite + Git | **零外部依赖** |

---

## 3. 状态持久化方案（P0，最核心的工程问题）

### 3.1 从 Gas Town 学到的教训

Gas Town 用 Dolt（一个完整的版本化数据库）来存储所有状态。这给了它强大的时间旅行查询能力，但代价是：
- 需要运行 Dolt SQL Server 进程
- GC 不做数据库膨胀 10-50x
- Push 慢到需要停机维护窗口

**我们的选择：SQLite + Git，零额外进程。**

### 3.2 两种状态，两种存储

```
操作状态（高频读写，不需要版本历史）
    → SQLite 单文件数据库
    → 任务状态、Agent 心跳、队列、进度

工作产物（需要版本控制、可回溯）
    → Git 仓库本身
    → 代码变更、Brief 文件、Mission 日志
```

### 3.3 SQLite Schema（核心表）

```sql
-- Agent 注册表：谁在线，能做什么
CREATE TABLE experts (
    id          TEXT PRIMARY KEY,        -- "backend-claude-1"
    runtime     TEXT NOT NULL,           -- "claude-code" | "codex" | "cursor"
    skills      TEXT,                    -- JSON: ["go", "python", "frontend"]
    status      TEXT DEFAULT 'idle',     -- idle | working | offline | stuck
    pid         INTEGER,                 -- 系统进程 ID
    worktree    TEXT,                    -- git worktree 路径
    heartbeat   DATETIME,               -- 最后心跳时间
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 任务表：谁该做什么，做到哪了
CREATE TABLE missions (
    id          TEXT PRIMARY KEY,        -- "m-a1b2c3"
    campaign_id TEXT,                    -- 所属 Campaign
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT DEFAULT 'pending',  -- pending | assigned | working | review | done | failed
    assignee    TEXT REFERENCES experts(id),
    topology    TEXT,                    -- 在 Campaign DAG 中的位置
    depends_on  TEXT,                    -- JSON: 依赖的 mission id 列表
    branch      TEXT,                    -- git 分支名
    result      TEXT,                    -- Brief（完成后的凝练摘要）
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME
);

-- Campaign 表：一批相关任务
CREATE TABLE campaigns (
    id          TEXT PRIMARY KEY,        -- "c-x7k2m"
    title       TEXT NOT NULL,
    topology    TEXT NOT NULL,           -- "parallel" | "pipeline" | "tree" | "mesh"
    config      TEXT,                    -- JSON: 拓扑配置
    status      TEXT DEFAULT 'active',   -- active | completed | failed
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Brief 表：Agent 间传递的凝练信息
CREATE TABLE briefs (
    id          TEXT PRIMARY KEY,
    from_expert TEXT,
    to_expert   TEXT,                    -- NULL = broadcast
    mission_id  TEXT,
    type        TEXT,                    -- "completion" | "handoff" | "help" | "review"
    summary     TEXT NOT NULL,           -- 凝练后的摘要
    artifacts   TEXT,                    -- JSON: 相关文件路径列表
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    read_at     DATETIME
);

-- Agent CV：工作历史（永久积累）
CREATE TABLE work_history (
    id          TEXT PRIMARY KEY,
    expert_id   TEXT NOT NULL,
    mission_id  TEXT NOT NULL,
    outcome     TEXT,                    -- "success" | "failed" | "partial"
    duration_s  INTEGER,
    skills_used TEXT,                    -- JSON
    quality     REAL,                    -- 0.0-1.0, 由 reviewer 评分
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 3.4 Git 状态结构

```
~/parliament/                       工作区根目录
├── .parliament/
│   ├── state.db                    SQLite 数据库（所有操作状态）
│   ├── config.toml                 全局配置
│   └── speaker/                    Speaker 上下文
│       └── context.md              Speaker 的持久记忆
├── projects/
│   └── <project>/
│       ├── main/                   主分支（bare repo 或 clone）
│       ├── worktrees/              Agent 沙箱
│       │   ├── backend-claude-1/   ← git worktree
│       │   ├── frontend-cursor-1/  ← git worktree
│       │   └── reviewer-gemini-1/  ← git worktree
│       └── briefs/                 Brief 存档（git tracked）
│           ├── m-a1b2c3.md         Mission 完成 Brief
│           └── m-d4e5f6.md
└── logs/                           运行日志
    └── dispatcher.log
```

### 3.5 崩溃恢复流程

```
Agent 崩溃
    │
    ├─ Dispatcher 检测到心跳超时（30s 无心跳）
    │
    ├─ 检查 worktree 状态
    │   ├─ 有未提交的代码？→ git stash 保存
    │   ├─ 有已提交未推送？→ 记录分支名
    │   └─ 干净状态？→ 直接标记 mission 可重派
    │
    ├─ 标记 expert 为 offline
    │
    ├─ 标记 mission 为 pending（可被重新分配）
    │   └─ 附上 Brief："上一个 Agent 做到了 X，崩溃前的代码在分支 Y"
    │
    └─ 等待 Speaker 决策：
        ├─ 重新分配给同一 Agent（如果它恢复了）
        ├─ 分配给另一个 Agent（带上崩溃前 Brief）
        └─ 升级为人工处理
```

---

## 4. 协作拓扑引擎（你的核心差异化）

### 4.1 拓扑定义 DSL

```toml
# 文件：campaigns/build-auth.toml

[campaign]
title = "Build Authentication System"
topology = "tree"   # parallel | pipeline | tree | mesh

# 并行分支
[[branches]]
id = "backend"
expert_skill = "go"           # 需要 Go 技能的 expert
mission = "Build JWT auth API endpoints"

[[branches]]
id = "frontend"
expert_skill = "react"
mission = "Build login/signup UI components"

[[branches]]
id = "database"
expert_skill = "sql"
mission = "Design user table schema and migrations"

# 汇合点
[[merges]]
id = "integration"
depends_on = ["backend", "frontend", "database"]
expert_skill = "fullstack"
mission = "Integration testing and conflict resolution"

# 最终审核
[[merges]]
id = "review"
depends_on = ["integration"]
expert_skill = "reviewer"
mission = "Code review and quality check"
```

### 4.2 四种内置拓扑

```
线性（Linear）              并行（Parallel）
A ──▶ B ──▶ C              A ──┬──▶ B
                               ├──▶ C
适合：bug 修复、小改动         ├──▶ D
                               └──▶ merge
                           适合：前后端并行开发

流水线（Pipeline）           网状（Mesh）
A ──▶ B ──▶ C ──▶ D        A ◀──▶ B
每一步的输出是下一步的输入       ▲      ▲
                               │      │
适合：数据处理、多阶段构建       └──▶ C ◀┘
                           适合：架构设计讨论、需要多方碰撞
```

### 4.3 拓扑引擎的执行逻辑

```go
// 伪代码：Dispatcher 的拓扑执行循环
func (d *Dispatcher) ExecuteCampaign(c *Campaign) {
    graph := BuildDAG(c.Branches, c.Merges)

    for !graph.AllDone() {
        // 找到所有依赖已满足、还没开始的节点
        ready := graph.ReadyNodes()

        for _, node := range ready {
            // 从专家池中选择匹配技能的空闲 expert
            expert := d.Pool.FindBest(node.RequiredSkill)
            if expert == nil {
                continue // 没有空闲 expert，下轮再试
            }

            // 收集上游 Brief 作为输入上下文
            upstreamBriefs := graph.GetUpstreamBriefs(node)

            // 派发任务
            d.Assign(expert, node.Mission, upstreamBriefs)
        }

        sleep(5s) // 轮询间隔
    }
}
```

---

## 5. 信息凝练协议（Brief Protocol）

### 5.1 为什么这是关键

Gas Town 的 agent 之间传完整消息。在 3 个 agent 时没问题，30 个 agent 时，每个 agent 的 context 会被上游信息淹没。

**Brief 的核心思想：人类开周会不会逐行读代码，Agent 之间也不应该。**

### 5.2 Brief 格式规范

```markdown
# Brief: [Mission Title]

## 结论（3 句话以内）
实现了 JWT 认证的 3 个 API endpoint，支持注册、登录、刷新 token。

## 变更清单
- `api/auth/register.go` — 新增，用户注册逻辑
- `api/auth/login.go` — 新增，JWT 签发
- `api/auth/middleware.go` — 新增，认证中间件
- `migrations/001_users.sql` — 新增，用户表

## 接口契约（下游需要知道的）
- POST /api/auth/register → { token: string }
- POST /api/auth/login → { token: string, refresh: string }
- 中间件从 Authorization header 解析 JWT
- Token 有效期 15 分钟，Refresh Token 7 天

## 假设与风险
- 假设使用 PostgreSQL（如果是 MySQL 需改 migration）
- 未实现 rate limiting，生产环境需补充

## 状态
✅ 完成 | 测试通过 | 分支: feat/auth-backend
```

### 5.3 Brief 的生成与消费

```
Agent A 完成任务
    │
    ├─ 生成 Full Report（git commit messages + diff，存档用）
    │
    ├─ 生成 Condensed Brief（上述格式，传递用）
    │   └─ 由 Agent 自己写，这是它的「工作总结」
    │
    └─ Brief 存入 briefs/ 目录（git tracked）
        │
        ├─ Dispatcher 将 Brief 路由给下游 Agent
        │
        └─ 下游 Agent 收到 Brief 作为任务输入上下文
            └─ 不是收到代码 diff，而是收到凝练摘要
```

### 5.4 Brief 类型

| 类型 | 触发场景 | 内容 |
|------|---------|------|
| **Completion Brief** | 任务完成 | 做了什么、改了什么、接口契约 |
| **Handoff Brief** | Agent 会话即将结束 | 做到哪了、下一步该做什么 |
| **Help Brief** | Agent 遇到困难 | 什么问题、试过什么、需要什么 |
| **Review Brief** | 代码审查完成 | 通过/不通过、需修改项、质量评分 |

---

## 6. Expert 生命周期管理

### 6.1 Expert 注册

```toml
# experts/backend-claude.toml — Expert 声明文件

[expert]
name = "backend-claude-1"
runtime = "claude-code"           # 使用什么 AI CLI
command = "claude"                # 启动命令
args = ["--model", "sonnet"]     # 启动参数

[capabilities]
skills = ["go", "python", "sql", "api-design"]
max_concurrent = 1                # 同时处理任务数
context_limit = "200k"            # 上下文容量（影响调度决策）

[health]
heartbeat_interval = "10s"        # 心跳间隔
stuck_threshold = "5m"            # 超过此时间无进展视为 stuck
max_retries = 2                   # 任务失败最大重试次数
```

### 6.2 Expert 状态机

```
                ┌──────────────┐
                │   offline    │◀─── 注册但未启动 / 心跳超时
                └──────┬───────┘
                       │ cp expert start
                       ▼
                ┌──────────────┐
        ┌──────│     idle     │◀─── 等待任务分配
        │      └──────┬───────┘
        │             │ 被分配 Mission
        │             ▼
        │      ┌──────────────┐
        │      │   working    │──── 正在执行任务
        │      └──┬───────┬───┘
        │         │       │
        │    任务完成   卡住/超时
        │         │       │
        │         ▼       ▼
        │      ┌─────┐ ┌──────┐
        │      │idle │ │stuck │──── Dispatcher 介入
        │      └─────┘ └──┬───┘
        │                 │ 重启/重派
        └─────────────────┘
```

### 6.3 Agent 运行时抽象

```go
// Runtime 接口：任何 AI CLI 都可以接入
type Runtime interface {
    // 在 worktree 中启动一个 agent session
    Start(worktree string, mission Mission, briefs []Brief) (Session, error)

    // 检查 session 是否还活着
    IsAlive(session Session) bool

    // 优雅关闭
    Stop(session Session) error

    // 发送消息（用于 nudge/help）
    Send(session Session, message string) error
}

// 内置实现
type ClaudeCodeRuntime struct { ... }   // claude --resume
type CodexRuntime struct { ... }         // codex
type CursorRuntime struct { ... }        // cursor CLI
type GenericRuntime struct { ... }       // 任意命令行工具
```

**关键设计决策**：Runtime 接口不依赖 tmux。

- 默认用**子进程管理**（`os/exec`），最简单可靠
- 可选 tmux backend（给需要视觉监控的用户）
- 可选 Docker backend（给需要沙箱隔离的 CI 场景）

---

## 7. Dispatcher（传输层）详细设计

### 7.1 职责边界

Dispatcher 是纯机械层（Go 代码），**不做任何业务决策**：

| Dispatcher 做 | Dispatcher 不做 |
|--------------|----------------|
| 按 DAG 顺序派发就绪任务 | 决定任务怎么拆 |
| 路由 Brief 到下游 Agent | 决定 Brief 内容 |
| 检测心跳超时 | 判断 Agent 是「在思考」还是「卡死了」|
| 管理 git worktree 创建/清理 | 解决 merge conflict |
| 收集 Agent 完成信号 | 评估 Agent 工作质量 |

**所有「需要判断」的事，都交给 Speaker（AI）。**

### 7.2 心跳与推进机制

```
每 10 秒：
    ├─ 检查所有 working expert 的心跳
    │   └─ 超时 → 标记 stuck，通知 Speaker 决策
    │
    ├─ 检查 Campaign DAG 中新就绪的节点
    │   └─ 有就绪节点 + 有空闲 expert → 自动分配
    │
    └─ 检查 Brief 队列
        └─ 有未投递的 Brief → 路由到目标 expert

每 60 秒：
    ├─ Speaker 巡逻（如果 Speaker 是 AI session）
    │   └─ 读取所有活跃 Campaign 的状态摘要
    │   └─ 决策：是否需要调整拓扑/重派/升级
    │
    └─ 持久化运行统计
```

### 7.3 合并协调

Gas Town 用了一个完整角色（Refinery）做合并。我们简化为 Dispatcher 内置合并调度：

```
并行分支全部完成
    │
    ├─ Dispatcher 收集所有分支名
    │
    ├─ 尝试自动合并到 integration 分支
    │   ├─ 无冲突 → 合并成功，标记 merge 节点就绪
    │   └─ 有冲突 → 生成 Conflict Brief，分配给 merge expert
    │
    └─ merge expert 解决冲突后提交
        └─ Dispatcher 继续 DAG 下一步
```

**Conflict Brief 格式**：

```markdown
# Conflict Brief

## 冲突分支
- feat/auth-backend (by backend-claude-1)
- feat/auth-frontend (by frontend-cursor-1)

## 冲突文件
- `src/types/user.ts` — 两边都定义了 User 类型，字段不同
- `package.json` — 依赖版本冲突

## 各方的 Brief 摘要
[Backend] JWT 认证 API，User 类型包含 password_hash 字段
[Frontend] 登录表单组件，User 类型包含 display_name 字段

## 建议
合并两个 User 类型定义，保留双方字段
```

---

## 8. Speaker 设计（编排者）

### 8.1 单 Speaker 模式（v1 先做这个）

```toml
# .parliament/config.toml

[speaker]
runtime = "claude-code"
model = "opus"
# Speaker 的持久上下文文件，每次启动注入
context_file = ".parliament/speaker/context.md"
```

Speaker 是一个长期运行的 AI Agent，它的 context.md 是持久化的「记忆」：

```markdown
# Speaker Context（持久记忆，每次启动注入）

## 当前项目状态
- 项目：MyApp — Go 后端 + React 前端
- 活跃 Campaign：build-auth（3/5 missions done）
- Expert 池：3 online，1 offline

## Expert 能力档案
- backend-claude-1：Go/Python 强，完成 12 个任务，成功率 91%
- frontend-cursor-1：React/TS 强，完成 8 个任务，成功率 88%
- reviewer-gemini-1：Review 准确，但速度慢

## 决策日志（最近 5 条）
- 2026-02-28 14:30：将 auth-backend 重派给 backend-claude-1（原 Agent 崩溃）
- 2026-02-28 15:10：auth-frontend 完成，质量评分 0.85，Brief 已路由
```

### 8.2 多 Speaker 模式（v2 再考虑）

你原始设计中的 Raft 选举，简化为**「竞标制」**：

```
新 Campaign 到来
    │
    ├─ 所有候选 Speaker 各出一份拆解方案
    │   ├─ Speaker A：分 3 个并行任务
    │   ├─ Speaker B：分 5 个流水线步骤
    │   └─ Speaker C：分 2 个大任务
    │
    ├─ 评估函数打分（或人类选择）
    │   ├─ 预期并行度
    │   ├─ 预期耗时
    │   ├─ 与现有 Expert 技能的匹配度
    │   └─ Speaker 历史决策质量
    │
    └─ 得分最高者获得该 Campaign 的执行权
```

这比 Raft 更符合 AI 的非确定性本质——**不要求行为一致，只选最优方案**。

---

## 9. CLI 设计

```bash
# 工作区管理
cp init ~/my-parliament              # 初始化工作区
cp project add myapp <repo-url>      # 添加项目

# Expert 管理
cp expert add backend-claude \        # 注册 expert
    --runtime claude-code \
    --skills go,python
cp expert list                        # 查看 expert 池
cp expert start backend-claude        # 启动 expert

# 任务执行
cp campaign create build-auth.toml    # 从拓扑文件创建 Campaign
cp campaign status                    # 查看进度
cp mission list                       # 查看所有任务

# Speaker
cp speaker start                      # 启动 Speaker（交互模式）
cp speaker auto                       # Speaker 自动编排模式

# 监控
cp status                             # 全局状态概览
cp brief list                         # 查看 Brief 流转
cp history <expert>                   # Expert 工作历史

# 运维
cp doctor                             # 健康检查
cp gc                                 # 清理已完成的 worktree
```

命令前缀 `cp`（**C**yber **P**arliament），短且不冲突。

---

## 10. 最小可行产品（MVP）路线图

### Phase 0：脚手架（1 周）

```
目标：cp init + cp project add + cp expert add 能跑通

实现：
- [ ] CLI 骨架（Cobra）
- [ ] SQLite 初始化（建表）
- [ ] config.toml 解析
- [ ] git worktree 创建/清理
```

### Phase 1：单 Agent 能跑（2 周）

```
目标：Speaker 把一个任务分给一个 Expert，Expert 完成后生成 Brief

实现：
- [ ] ClaudeCodeRuntime 实现（启动/停止/心跳）
- [ ] Mission 创建与分配
- [ ] Brief 生成模板注入（让 Agent 知道要写 Brief）
- [ ] 完成信号检测
- [ ] Brief 存储
```

### Phase 2：并行拓扑能跑（2 周）

```
目标：一个 Campaign 拆成 2 个并行 Mission，两个 Expert 同时工作，各自完成后生成 Brief

实现：
- [ ] Campaign TOML 解析
- [ ] DAG 拓扑引擎
- [ ] 并行派发
- [ ] Brief 路由到下游
- [ ] 合并节点：自动 merge + conflict 检测
```

### Phase 3：崩溃恢复 + Speaker 自动化（2 周）

```
目标：Agent 崩溃后任务自动重派；Speaker 作为 AI 自动做决策

实现：
- [ ] 心跳超时检测
- [ ] 崩溃恢复流程（stash + 重派 + Brief 传递）
- [ ] Speaker AI session 管理
- [ ] Speaker context.md 持久化
- [ ] work_history 记录
```

### Phase 4：完善体验（2 周）

```
目标：多种拓扑、多 runtime、TUI 监控

实现：
- [ ] Pipeline / Tree / Mesh 拓扑
- [ ] Codex / Cursor runtime 接入
- [ ] cp status TUI（类似 Gas Town 的 gt feed）
- [ ] Expert 技能匹配算法
- [ ] Review Brief 与质量评分
```

---

## 11. 技术栈选择

| 组件 | 选择 | 理由 |
|------|------|------|
| 主语言 | **Go** | 单二进制分发、并发模型好、与 Gas Town 同生态可借鉴 |
| CLI 框架 | **Cobra** | Go 生态标准 |
| 数据库 | **SQLite**（CGo-free: modernc.org/sqlite） | 零部署、单文件、够用 |
| 沙箱隔离 | **Git worktree** | Gas Town 验证过的方案 |
| 进程管理 | **os/exec + 可选 tmux** | 不强绑 tmux |
| TUI | **BubbleTea**（可选） | Go 生态最好的 TUI 库 |
| 配置格式 | **TOML** | 人类可读、比 YAML 不易出错 |

### 为什么不用 Rust？

Go 的优势在于：与 Claude Code、Codex 等 AI CLI 工具同生态（它们的 wrapper 都是 Node/Go），且 Gas Town 积累的大量 git worktree 管理代码可参考。Rust 的性能优势在此场景不明显（瓶颈是 AI agent 响应时间，不是调度器速度）。

---

## 12. 与 Gas Town 的最终对比

| 维度 | Gas Town | Cyber Parliament |
|------|----------|-----------------|
| 依赖 | Dolt + beads CLI + tmux + sqlite3 | **仅 Git**（SQLite 内嵌） |
| 概念数 | 15+（Polecat/Bead/Wisp/Hook/Convoy...） | **6 个**（Parliament/Speaker/Expert/Mission/Campaign/Brief） |
| 协作拓扑 | 固定星形（Mayor→Polecat） | **可配置 DAG**（线性/并行/流水线/网状） |
| 信息传递 | 完整 mail body | **Condensed Brief 协议** |
| Agent 运行时 | 后期支持多个，但架构初始绑 Claude | **Day 1 多 runtime**（接口抽象优先） |
| 编排者容错 | 单点（Mayor） | **竞标制多 Speaker**（v2） |
| 存储复杂度 | Dolt SQL Server（需运行维护） | **SQLite 单文件**（零运维） |
| 成熟度 | 0.8.x，12 万行代码 | **设计阶段** |
| 目标用户 | 重度 AI 开发用户 | **愿意尝试 AI 协作的开发者** |

---

## 13. 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| SQLite 并发写入不够用 | 低 | 中 | WAL 模式 + 单写入者模式；真不够再换 PostgreSQL |
| Brief 质量差（Agent 生成的摘要不准确） | 中 | 高 | 提供 Brief 模板 + 关键字段必填校验 + Review Brief 交叉检查 |
| 不同 Runtime 的行为差异太大 | 中 | 中 | Runtime 接口约束输入输出格式；差异通过 adapter 吸收 |
| Agent 不按规矩来（不写 Brief、不发完成信号） | 高 | 高 | **这是最大风险**——通过 CLAUDE.md/AGENTS.md 注入行为规范 + 超时兜底 |

---

*本文档是从 idea 到可落地方案的第一步。核心策略：从 Gas Town 偷最好的工程方案（git worktree、归因、崩溃恢复），砍掉最重的依赖（Dolt、tmux、beads），加上 Gas Town 没有的（Brief 协议、可配置拓扑、轻量化设计）。*

*下一步：选定 Phase 0 开始写代码。*
