# House of Cards 🃏

> AI Agent 协作框架 — 用政府隐喻管理 AI 团队

House of Cards (`hoc`) 是一个 AI Agent 编排框架。它将多 Agent 协作建模为一套完整的"议会系统"：每个 AI Agent 是一位**部长**（Minister），每项任务是一份**议案**（Bill），整个开发会期是一场**议会**（Session）。

---

## 术语速查（隐喻对照表）

| 概念 | 隐喻术语 | 说明 |
|------|---------|------|
| 编排者 | **Speaker**（议长） | 负责分解任务、分配议案 |
| 执行 Agent | **Minister**（部长） | 处理具体议案的 AI Agent |
| 工作项 | **Bill**（议案） | 一项具体任务（feature/bugfix/etc.） |
| 批量工作 | **Session**（会期） | 一组相关议案的集合 |
| Agent 池 | **Cabinet**（内阁） | 所有注册部长的注册表 |
| 推进/监控 | **Whip**（党鞭） | 心跳监控、DAG 调度、补选恢复 |
| 信息摘要 | **Gazette**（公报） | 部长间传递的工作摘要 |
| 审计记录 | **Hansard**（议事录） | 每次任务完成的历史记录 |
| 审查 | **Committee**（委员会） | 议案质量审查阶段 |
| 合并仲裁 | **Privy Council**（枢密院） | 多 Agent 成果合并 |
| 工作沙箱 | **Chamber**（议事厅） | Git worktree 隔离环境 |
| 崩溃恢复 | **By-election**（补选） | 部长失联后重新分配议案 |

---

## 快速上手

```bash
# 安装
go install github.com/house-of-cards/hoc/cmd/hoc@latest

# 1. 起草会期文件 (TOML 格式)
cat > session.toml << 'EOF'
[session]
title    = "Build Auth System"
topology = "pipeline"

[[bills]]
id         = "auth-api"
title      = "Implement JWT API"
motion     = "Build JWT authentication endpoints in Go"
portfolio  = "go"
depends_on = []

[[bills]]
id         = "auth-frontend"
title      = "Build Login UI"
motion     = "Create React login/signup pages"
portfolio  = "react"
depends_on = ["auth-api"]
EOF

# 2. 开启会期
hoc session open session.toml

# 3. 传召部长
hoc minister summon backend-claude --runtime claude-code

# 4. 分配议案
hoc bill assign <bill-id> backend-claude

# 5. 启动党鞭（后台调度 + 监控）
hoc whip start
```

---

## CLI 命令参考

### Session（会期）

```bash
hoc session open <file.toml>          # 开启新会期
hoc session status                    # 列出所有会期
hoc session status <session-id>       # 单会期详情 + DAG 图
hoc session stats                     # 所有会期统计
hoc session stats <session-id>        # 单会期统计（质量、耗时、部长负荷）
hoc session dissolve <session-id>     # 解散会期
```

### Bill（议案）

```bash
hoc bill list                         # 列出所有议案
hoc bill list --json                  # JSON 格式输出
hoc bill show <bill-id>               # 议案详情（含复杂度预测）
hoc bill draft --title "..." --motion "..."  # 起草新议案
hoc bill assign <bill-id> <minister-id>     # 分配议案
hoc bill enacted <bill-id>            # 标记为通过
hoc bill review <bill-id> --pass      # 委员会审查通过
hoc bill review <bill-id> --fail      # 委员会审查未通过
```

### Minister（部长）

```bash
hoc minister list                     # 列出所有部长
hoc minister summon <id> --runtime <rt>  # 传召部长
hoc minister dismiss <id>             # 解散部长
hoc minister show <id>                # 部长详情
```

### Whip（党鞭）

```bash
hoc whip start                        # 启动守护进程（后台调度）
hoc whip report                       # 当前状态报告
hoc whip report --history             # 含历史记录
```

### Hansard（议事录）

```bash
hoc hansard                           # 查看所有议事录
hoc hansard <minister-id>             # 查看特定部长履历
hoc hansard list                      # 列出所有记录
hoc hansard trend                     # 成功率趋势图
hoc hansard score                     # 质量评分排名
```

### Gazette（公报）

```bash
hoc gazette list                      # 列出所有公报
hoc gazette read <id>                 # 标记为已读
```

### Cabinet（内阁）

```bash
hoc cabinet list                      # 内阁概览
```

### Speaker（议长）

```bash
hoc speaker plan <session-id>         # AI 规划：分解任务为议案
```

---

## 架构图（ASCII）

```
┌─────────────────────────────────────────────────────────┐
│                     hoc CLI (Cobra)                     │
└────────────┬────────────────────────────────────────────┘
             │
    ┌────────▼────────┐
    │  Speaker（议长）  │  ← AI 任务规划（claude-code / codex）
    └────────┬────────┘
             │ Session + Bills
    ┌────────▼────────┐
    │  Whip（党鞭）    │  ← 10s tick: 心跳 + DAG + 补选
    └────┬────────────┘
         │ autoAssign / byElection
    ┌────▼──────────────────────┐
    │  Minister Pool（内阁）     │
    │  ┌──────┐ ┌──────┐       │
    │  │ M-A  │ │ M-B  │ ...   │  ← claude-code / cursor / codex
    │  └──────┘ └──────┘       │
    └───────────────────────────┘
         │
    ┌────▼──────────────────┐
    │  Chamber（议事厅）     │  ← git worktree 隔离
    └───────────────────────┘
         │
    ┌────▼──────────────────┐
    │  Privy Council（枢密院）│  ← git merge 仲裁
    └───────────────────────┘
         │
    ┌────▼──────────────────┐
    │  SQLite（store）       │  ← 持久化状态
    └───────────────────────┘
```

---

## 会期拓扑示例

### Parallel（并行）

所有议案同时开始，彼此独立。

```
Session
├── Bill A  (portfolio: go)      → Minister-1
├── Bill B  (portfolio: react)   → Minister-2
└── Bill C  (portfolio: python)  → Minister-3
```

### Pipeline（流水线）

议案串行，前一个完成后触发下一个。

```
[Bill A] → [Bill B] → [Bill C]
  enacted    reading    draft
```

### Mesh（网状）

任意 DAG 结构，多依赖关系。

```
[Bill A]──┐
          ▼
[Bill B]──► [Bill D] → [Bill E]
          ▲
[Bill C]──┘
```

---

## 质量评分系统

Hansard 记录中的质量分（0.0–1.0）由以下公式计算：

```
质量分 = outcome_score + committee_bonus + stability_bonus

outcome_score:
  enacted  → 0.80
  partial  → 0.40
  failed   → 0.00

committee_bonus:
  审查通过（notes 含 "PASS"）→ +0.15

stability_bonus:
  无补选中断（notes 不含 "补选"）→ +0.05
```

---

## 开发

### 运行测试

```bash
go test ./...           # 全部测试
go test ./cmd/...       # 集成测试
go test ./internal/...  # 单元测试
```

### 构建

```bash
go build -o hoc ./cmd/hoc/
```

### 代码检查

```bash
go vet ./...
```

---

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.22+ |
| CLI 框架 | [Cobra](https://github.com/spf13/cobra) |
| 数据库 | SQLite（[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，CGo-free） |
| 配置格式 | TOML |
| AI Runtime | claude-code / cursor / codex（抽象层） |
| 沙箱 | Git worktree |
| 可观测性 | OpenTelemetry（指标 + 追踪） |

---

*House of Cards — 因为软件开发就像一座由 AI 建造的纸牌屋。*
