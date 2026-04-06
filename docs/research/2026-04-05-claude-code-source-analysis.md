# 研究报告：Claude Code 源码架构分析及对 HOC 的启发

> 日期：2026-04-05
>
> 来源：[Claude Code 源码分析教程](https://nangongwentian-fe.github.io/learn-claude-source/overview.html)（基于 v2.1.88 泄漏源码）
>
> 目的：提炼 Claude Code 的工程设计决策，评估对 House of Cards 的借鉴价值

---

## 1. Claude Code 架构概览

### 1.1 项目基本面

| 维度 | 数据 |
|------|------|
| 版本 | v2.1.88 |
| 技术栈 | TypeScript + React（Ink 终端框架） |
| 构建工具 | Bun |
| 内置工具 | 45+ |
| CLI 命令 | 101 个 |
| 编译产物 | 单个 13MB 的 `cli.js` |

### 1.2 七层架构

```
┌─────────────────────────────────────┐
│  UI 层        146 个 Ink/React 组件  │
├─────────────────────────────────────┤
│  Hook 层      87 个 React Hooks     │
├─────────────────────────────────────┤
│  核心引擎     QueryEngine + Agent Loop │
├─────────────────────────────────────┤
│  工具层       45+ Tool 实现          │
├─────────────────────────────────────┤
│  服务层       38 个 Service 模块     │
├─────────────────────────────────────┤
│  基础设施     331 个工具模块          │
├─────────────────────────────────────┤
│  运行时       REPL + 状态管理 + CLI  │
└─────────────────────────────────────┘
```

### 1.3 核心设计哲学

教程提炼的五个关键认知：

1. **基础循环**："一个通过工具调用的 `while` 循环构成整个系统"
2. **统一接口**：所有功能（文件操作、Shell 执行、Agent 调用）统一为 Tool
3. **权限优先**：每次工具调用都必须经过权限审核
4. **资源管理**：三层策略压缩有限的 Context Window
5. **异步编程**：`async function*`（异步生成器）贯穿全系统

---

## 2. 核心模块深度分析

### 2.1 Agent Loop（S01）

**最小 Agent 核心 = `while` 循环 + 一个工具。**

教学版 30 行 Python：

```python
def agent_loop(query):
    messages = [{"role": "user", "content": query}]
    while True:
        response = client.messages.create(
            model=MODEL, system=SYSTEM, messages=messages,
            tools=TOOLS, max_tokens=8000,
        )
        messages.append({"role": "assistant", "content": response.content})
        if response.stop_reason != "tool_use":
            return
        results = []
        for block in response.content:
            if block.type == "tool_use":
                output = run_bash(block.input["command"])
                results.append({
                    "type": "tool_result",
                    "tool_use_id": block.id,
                    "content": output,
                })
        messages.append({"role": "user", "content": results})
```

生产版 ~5800 行，分布在三个文件：

| 文件 | 行数 | 职责 |
|------|------|------|
| `QueryEngine.ts` | ~1295 | 会话生命周期管理 |
| `query.ts` | ~1729 | 核心 `while(true)` 循环 |
| `claude.ts` | ~2800 | 流式 API + 事件处理 |

**生产版的关键差异——三层恢复机制：**

```typescript
// 恢复路径 1：上下文溢出 → 自动折叠
if (isPromptTooLongMessage(lastMessage)) {
    const drained = contextCollapse.recoverFromOverflow(messages)
    if (drained.committed > 0) {
        state.messages = drained.messages
        continue
    }
    // 恢复路径 2：反应式压缩
    if (!state.hasAttemptedReactiveCompact) {
        const compacted = reactiveCompact(messages)
        state.messages = compacted
        state.hasAttemptedReactiveCompact = true
        continue
    }
}
// 恢复路径 3：max_tokens 截断 → 加倍上限
if (lastStopReason === 'max_tokens') {
    state.maxOutputTokensOverride = currentLimit * 2
    state.maxOutputTokensRecoveryCount++
    continue
}
```

**设计决策记录：**

- **为什么 `while(true)` 而非递归？** 防止数百次工具迭代导致栈溢出；`continue` 实现清晰的重试逻辑。
- **为什么可变消息列表？** 性能（避免大量拷贝）+ 持久化（实时写盘）+ 压缩（需要原地修改）。
- **为什么异步生成器？** 流式输出、惰性求值、可取消、背压控制。

### 2.2 工具系统（S02）

**核心原则：循环不变，新工具只是注册到分发表中。**

统一的 Tool 接口：

```typescript
type Tool<Input, Output> = {
    name: string
    inputSchema: ZodSchema<Input>      // 运行时类型验证（不信任 LLM 输出）
    description(): Promise<string>
    checkPermissions(): Promise<PermissionResult>
    call(): Promise<ToolResult<Output>>
    isConcurrencySafe(input): boolean   // 是否可并行执行
    isReadOnly(input): boolean          // 是否只读
    maxResultSizeChars: number          // 超限自动持久化到磁盘
}
```

**Builder 模式代替继承：**

```typescript
const TOOL_DEFAULTS = {
    isConcurrencySafe: () => false,  // fail-closed：安全第一
    checkPermissions: (input) => Promise.resolve({ behavior: 'allow' })
}

export function buildTool<D>(def: D): BuiltTool<D> {
    return { ...TOOL_DEFAULTS, ...def }
}
```

**工具执行管线（8 步）：**

1. 名称查找（支持别名）
2. Zod schema 输入验证
3. 自定义验证（如 BashTool 检测 sleep 循环）
4. PreHook 执行（可修改输入）
5. 权限检查（allow / deny / ask）
6. 工具执行
7. PostHook 执行
8. 结果格式化 + 自动大小管理

**并发控制（StreamingToolExecutor）：**

- `Read + Grep + Glob` 标记为 `isConcurrencySafe: true`，可并行
- `Write / Edit / Bash` 必须独占执行
- 非安全工具等待所有并发操作完成后才执行

### 2.3 权限系统（S03）

**多维度分层设计，6 阶段权限管线。**

五种全局权限模式：

| 模式 | 行为 |
|------|------|
| `default` | 只读自动放行，写操作询问用户 |
| `acceptEdits` | 文件编辑自动接受，其他询问 |
| `bypassPermissions` | 全部自动放行（危险模式） |
| `plan` | 禁止所有写操作 |
| `auto` | AI 分类器自动判断（实验性） |

**BashTool 的 6 阶段权限流程：**

1. **AST 安全解析**：用 tree-sitter WASM 解析 shell 语法（不用正则）
2. **语义检查**：检测 `eval`、`exec` 等动态执行
3. **沙箱放行**：已知安全命令自动通过
4. **精确匹配**：检查用户 allow/deny 规则
5. **AI 分类器**：用 LLM 判断是否匹配语义规则
6. **回退**：默认询问用户

**7 层配置优先级（deny 永远优先于 allow）：**

```
组织策略 > 设备管理 > 用户全局 > 项目级 > 本地 > CLI 参数 > 当前会话
```

### 2.4 系统提示词（S04）

**系统提示词是"动态程序"，不是静态文本。**

8 个组成部分：

1. 身份基础（"You are Claude Code"）
2. 工具使用指南（schemas + 说明）
3. 环境快照（平台、shell、工作目录、OS）
4. 权限模式文档
5. CLAUDE.md 用户项目指令
6. 日期信息
7. Skills 目录
8. MCP 服务器指令

**延迟工具加载（三层）：**

- Layer 1：核心工具 schema 始终包含（Bash、Read、Edit、Write、Glob、Grep、Agent）
- Layer 2：延迟工具仅注入名称+描述（通过 `system-reminder` 标签）
- Layer 3：MCP 工具在服务器连接后动态注册

**CLAUDE.md 加载层次（从高到低）：**

```
~/.claude/CLAUDE.md（全局用户指令）
.claude/CLAUDE.md（项目团队指令）
CLAUDE.md（项目根目录）
subdir/CLAUDE.md（子目录覆盖）
```

### 2.5 子代理架构（S06）

**核心模式："分治" + 独立消息历史。**

子代理不是独立进程，而是 Tool——每个子代理获得一个全新的 `QueryEngine` 实例，只有最终文本结果返回给父 Agent。

**工具集隔离：**

- 父 Agent 有完整工具套件（含 `AgentTool`）
- 子 Agent **没有** `AgentTool`（防止无限递归）
- 专用类型有限制工具集：
  - `Explore` 类型：只有 Read/Glob/Grep（只读）
  - `Plan` 类型：禁止所有写操作

**共享 vs 隔离：**

| 共享 | 隔离 |
|------|------|
| 文件系统、工作目录、权限 | 消息历史、token 追踪、压缩周期 |

**安全约束：**

- 50 轮迭代上限（防止死循环）
- `isolation: 'worktree'` 支持独立 git 分支
- `run_in_background: true` 支持后台执行

### 2.6 MCP 协议（S07）

**MCP 将 Claude Code 从"封闭工具集"变为"开放平台"。**

- 客户端-服务器模式（Claude Code 是 Client）
- 三种传输：stdio（本地进程）/ SSE（远程流式）/ HTTP（REST）
- 发现的外部工具包装为标准 `Tool` 对象，命名 `mcp__serverName__toolName`
- 安全：首次审批 + autoApprove + OAuth + 统一权限框架

### 2.7 Hooks 系统（S08）

**在关键事件节点注入用户自定义逻辑。**

20+ 种事件类型：

| 类别 | 事件 |
|------|------|
| 工具生命周期 | PreToolUse、PostToolUse、PostToolUseFailure、PermissionDenied |
| 会话生命周期 | SessionStart、SessionEnd、Stop |
| 用户交互 | UserPromptSubmit、Notification |
| 多代理 | SubagentStart、SubagentEnd |
| 任务 | TaskCreate、TaskUpdate |

**执行位置：** 输入验证 → 自定义验证 → **PreHook** → 权限检查 → 工具执行 → **PostHook** → 返回

**Hook 协议：** 通过 stdin 接收 JSON，stdout 返回 JSON（支持 `block` / `updatedInput` / `updatedResult` / `notification`）。

### 2.8 流式处理（S09）

**四层流式架构：**

| 层 | 职责 | 关键文件 |
|----|------|---------|
| Layer 1 | API 响应流解析 | `claude.ts` |
| Layer 2 | Agent Loop 工具调度 | `query.ts` |
| Layer 3 | 会话管理与事件转发 | `QueryEngine.ts` |
| Layer 4 | 终端渲染 | React/Ink UI |

**关键机制：** 工具 JSON 完成后立即执行（不等全部接收完）；只读操作可并发，写操作独占。

### 2.9 Skills 与 CLAUDE.md（S10）

**两层知识注入：**

- **CLAUDE.md**：会话开始时自动加载到 system prompt（"你应该始终知道的"）
- **Skills**：按需加载到 tool_result（"你需要时再查的"）

**Token 经济学：** 10 个 Skills × 2000 tokens = 20,000 tokens，大部分任务用不到。延迟加载只在触发时消耗。

### 2.10 状态与会话持久化（S11）

**JSONL 追加写入 + JSON 元数据：**

- 每条消息追加写入（crash-safe，最多丢一行）
- 支持 `--resume` 恢复会话
- FileStateCache 检测外部文件修改
- 后台会话通过守护进程独立运行

---

## 3. 与 House of Cards 的对应关系

### 3.1 架构对齐

| Claude Code 概念 | HOC 对应 | 对齐程度 |
|------------------|---------|---------|
| Agent Loop（`while(true)`） | Whip tick loop（10s 间隔） | 本质相同，驱动对象不同 |
| 主 Agent | Speaker | 直接对应 |
| SubAgent（独立 messages[]） | Minister（独立 Chamber） | 高度对齐 |
| 只返回摘要给主 Agent | Gazette 协议（传摘要不传原文） | 完全一致 |
| 子 Agent 禁止嵌套 | Minister 不能传召 Minister | 完全一致 |
| `isolation: 'worktree'` | Chamber（git worktree） | 完全一致 |
| 50 轮迭代上限 | stuck 检测 + by-election | 功能等价 |
| Tool 注册表 | Cabinet（内阁） | 概念对应 |
| Hooks（PreToolUse/PostToolUse） | 无直接对应 | **差距** |
| 三层上下文压缩 | 无直接对应 | **差距** |
| 7 层权限配置 | 扁平 `config.toml` | **差距** |

### 3.2 HOC 已做对的设计

1. **Gazette 摘要协议** — 与 Claude Code "只返回摘要给父 Agent" 的理念完全一致
2. **Git worktree 隔离** — Claude Code 也用 worktree 做子代理隔离，验证了我们的方向
3. **禁止递归调用** — 子 Agent 没有 AgentTool，我们的 Minister 不能传召 Minister
4. **Tick 驱动的推进机制** — Whip 和 Agent Loop 本质都是循环驱动
5. **Hansard 审计** — Claude Code 的 JSONL 转录 + 元数据对应我们的 Hansard 设计

---

## 4. 对 HOC 的具体启发

### 4.1 【高优先级】Whip 自动恢复策略

**现状：** Minister stuck → 直接触发 by-election（重新分配）。

**Claude Code 做法：** 三层恢复——先压缩上下文，再反应式压缩，最后加倍 token，全部失败才终止。

**建议：** 在 Whip 的 `threeLineWhip()` 中增加恢复梯度：

```
Minister 异常
  → 第 1 次：发 Gazette 提醒 Minister 做 checkpoint
  → 第 2 次：缩减 Bill scope（自动拆分子 Bill）
  → 第 3 次：触发 by-election
```

**落地时机：** v0.3 Phase 1（E-1.1 Whip 错误治理），与现有错误处理改进合并。

### 4.2 【高优先级】Minister 上下文健康监控

**现状：** Minister 在长任务中 token 爆了，Whip 无法感知，只能等超时。

**Claude Code 做法：** 自动监控消息列表大小，接近阈值时触发压缩。

**建议：** 在 Gazette 协议中增加 `context_health` 字段：

```go
type GazettePayload struct {
    // 现有字段...
    ContextHealth struct {
        TokensUsed    int  `json:"tokens_used"`
        TokensLimit   int  `json:"tokens_limit"`
        TurnsElapsed  int  `json:"turns_elapsed"`
    } `json:"context_health,omitempty"`
}
```

Whip 在 tick 中检查该字段，接近 80% 时主动干预（提醒 checkpoint / 拆分 Bill）。

**落地时机：** v0.3 Phase 2（B-1 Whip 测试时顺带加）。

### 4.3 【中优先级】Bill 生命周期 Hook 机制

**现状：** Bill 状态变更只写数据库，无外部通知机制。

**Claude Code 做法：** 20+ 种事件类型，用户通过 JSON 配置自定义 shell 脚本，通过 stdin/stdout JSON 交互。

**建议：** 在 `hoc.toml` 中增加 Hook 配置：

```toml
[hooks]
# Bill 生命周期
on_bill_enacted = ["curl -X POST $SLACK_WEBHOOK -d '{\"text\": \"Bill enacted: {{.BillID}}\"}'"]
on_bill_stuck   = ["python3 scripts/alert.py --bill {{.BillID}}"]

# Session 生命周期
on_session_start = ["echo 'Session {{.SessionID}} started'"]
on_session_end   = ["scripts/report.sh {{.SessionID}}"]

# Minister 事件
on_minister_summoned  = []
on_minister_dismissed = []
on_by_election        = ["scripts/incident.sh {{.MinisterID}}"]
```

**落地时机：** v0.4。

### 4.4 【中优先级】Minister 工具权限按 Bill 类型限制

**现状：** Minister 的行为边界完全靠 CLAUDE.md prompt 约束。

**Claude Code 做法：** 子代理按类型（Explore/Plan/code-reviewer）获得不同工具集，Explore 只能读不能写。

**建议：** 在 Bill 或 Minister 配置中增加权限声明：

```toml
[[minister]]
name = "code-reviewer"
type = "review"
permissions = ["read", "comment"]  # 禁止 write/execute

[[minister]]
name = "backend-dev"
type = "codegen"
permissions = ["read", "write", "execute"]

[[minister]]
name = "researcher"
type = "research"
permissions = ["read", "web_search"]  # 禁止 write
```

Minister summon 时根据类型注入对应的 CLAUDE.md 权限段。

**落地时机：** v0.4。

### 4.5 【低优先级】分层配置模型

**现状：** 扁平的 `hoc.toml`。

**Claude Code 做法：** 7 层配置优先级，deny 永远优先于 allow。

**建议：** 随团队场景扩展，逐步引入：

```
组织策略（~/.hoc/policy.toml）
  > 项目配置（.hoc/config.toml）
    > Minister 级覆盖（Minister CLAUDE.md）
      > Session 级临时参数（CLI --flag）
```

**落地时机：** v0.5+。

### 4.6 【低优先级】统一 Tool 接口

**现状：** Minister 通过 os/exec 调用外部 AI runtime，动作能力由 runtime 决定。

**Claude Code 做法：** 所有能力统一为 `Tool` 接口，自带 `isConcurrencySafe`、`isReadOnly`、`checkPermissions`。

**建议：** 当 Minister 需要支持多种工具（不仅限于 AI subprocess）时，定义统一接口：

```go
type Action interface {
    Name() string
    Validate(input json.RawMessage) error
    IsConcurrencySafe() bool
    IsReadOnly() bool
    CheckPermission(ctx context.Context, minister string) error
    Execute(ctx context.Context, input json.RawMessage) (ActionResult, error)
}
```

**落地时机：** v0.5+（Minister 支持多工具时）。

---

## 5. 落地优先级总览

| 优先级 | 启发项 | 对应 HOC 模块 | 建议版本 | 依据 |
|--------|--------|-------------|---------|------|
| **P0** | Whip 自动恢复策略 | `internal/whip/` | v0.3 P1 | 与 E-1.1 错误治理直接相关 |
| **P0** | Minister 上下文健康监控 | `internal/whip/` + Gazette | v0.3 P2 | 防止长任务无声失败 |
| **P1** | Bill 生命周期 Hook | `internal/store/` + config | v0.4 | 可扩展性基础设施 |
| **P1** | Minister 权限按类型限制 | `cmd/ministers.go` + config | v0.4 | 安全边界从 prompt 升级为代码 |
| **P2** | 分层配置模型 | `internal/config/` | v0.5+ | 多用户/团队场景需要 |
| **P2** | 统一 Tool/Action 接口 | 新模块 | v0.5+ | Minister 多工具支持时需要 |

---

## 6. 结论

Claude Code 源码揭示了一个核心事实：**工业级 Agent 系统的复杂性不在 AI 调用本身，而在围绕 AI 调用的工程层——恢复、权限、上下文管理、并发控制。**

HOC 在架构层面与 Claude Code 的子代理模式高度对齐（Gazette 摘要、worktree 隔离、禁止递归），验证了设计方向的正确性。当前最大的差距在**韧性工程**（恢复策略、上下文监控），这恰好与 v0.3 "从能用到可靠" 的主题契合，建议在 v0.3 的错误治理和 Whip 测试中优先补齐。
