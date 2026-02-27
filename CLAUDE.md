# House of Cards — Agent 工作纪律

> 本文件是 Claude Code 在此项目中工作的强制规则。每次会话自动加载。

## 项目简介

House of Cards 是一个 AI Agent 协作框架，使用政府隐喻（Speaker/Minister/Whip/Gazette）构建多 Agent 编排系统。Go 语言实现，CLI 前缀 `hoc`。

设计文档：`docs/02-design-v3-final.md`

## 核心工作纪律

### 1. Agent Context 必须维护

**每次改动后**，必须同步更新 `.hoc/agent_context.md`。

**开始每个任务前**，必须先读取 `.hoc/agent_context.md`，了解当前项目状态。

Agent Context 规则：
- 只保留最近 1-3 次交互的上下文
- 更早的上下文**不删除**，移入 `.hoc/agent_context_archive.md`
- Agent Context 不是归档文件，是**工作台**——保持精简、当前、可操作

### 2. 同步流程（每次交互）

```
开始任务前：
  1. 读取 .hoc/agent_context.md
  2. 了解当前状态、上次做到哪了、有什么待解决问题

完成任务后：
  1. 将 agent_context.md 中超过 3 次交互的旧内容移入 agent_context_archive.md
  2. 更新 agent_context.md，记录：
     - 本次做了什么
     - 当前项目状态
     - 下一步待做事项
     - 已知问题或决策
```

### 3. 术语规范

本项目使用政府隐喻命名，所有代码、文档、CLI 命令必须遵循：

| 概念 | 隐喻术语 | 禁止使用 |
|------|---------|---------|
| 编排者 | Speaker（议长） | Orchestrator, Manager |
| 执行 Agent | Minister（部长） | Worker, Agent, Expert |
| 工作项 | Bill（议案） | Task, Issue, Ticket |
| 批量工作 | Session（会期） | Campaign, Batch, Sprint |
| Agent 池 | Cabinet（内阁） | Pool, Registry |
| 推进/监控 | Whip（党鞭） | Scheduler, Watchdog |
| 信息摘要 | Gazette（公报） | Brief, Message, Mail |
| 审计记录 | Hansard（议事录） | Log, History, CV |
| 审查 | Committee（委员会） | Reviewer, Validator |
| 合并仲裁 | Privy Council（枢密院） | Merger, Refinery |
| 工作沙箱 | Chamber（议事厅） | Worktree, Sandbox |
| 崩溃恢复 | By-election（补选） | Respawn, Recovery |

### 4. 技术栈

- 语言：Go
- CLI：Cobra
- 数据库：SQLite（modernc.org/sqlite，CGo-free）
- 沙箱：Git worktree
- 进程管理：os/exec（可选 tmux backend）
- TUI：BubbleTea（后期）
- 配置：TOML
- CLI 前缀：`hoc`

### 5. 目录约定

```
house-of-cards/
├── CLAUDE.md              ← 你正在读的这个文件
├── .hoc/
│   ├── agent_context.md   ← 当前工作上下文（必读必写）
│   └── agent_context_archive.md  ← 历史上下文归档
├── docs/                  ← 设计文档（参考，不频繁修改）
├── cmd/                   ← CLI 入口
│   └── hoc/
│       └── main.go
├── internal/              ← 核心实现
│   ├── speaker/
│   ├── minister/
│   ├── whip/
│   ├── cabinet/
│   ├── bill/
│   ├── session/
│   ├── gazette/
│   ├── hansard/
│   ├── chamber/           ← git worktree 管理
│   ├── privy/             ← 合并仲裁
│   ├── runtime/           ← AI runtime 抽象层
│   └── store/             ← SQLite 存储层
└── configs/               ← 示例配置
```
