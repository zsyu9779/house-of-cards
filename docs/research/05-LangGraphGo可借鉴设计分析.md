# LangGraphGo 项目分析：可借鉴设计报告

## 文档概述

本报告分析 LangGraphGo 项目的核心设计理念、架构模式和技术实现，识别可以借鉴到 House of Cards 项目的具体设计。

---

## 1. 核心图引擎 (graph/)

### 1.1 泛型 StateGraph

**文件**: `graph/state_graph.go`

**模式**: `StateGraph[S any]` - 使用 Go 泛型实现编译时类型安全

```go
type StateGraph[S any] struct {
    nodes            map[string]TypedNode[S]
    edges            []Edge
    conditionalEdges map[string]func(ctx context.Context, state S) string
    entryPoint       string
    retryPolicy      *RetryPolicy
    stateMerger      TypedStateMerger[S]
    Schema           StateSchema[S]
}

type TypedNode[S any] struct {
    Name        string
    Description string
    Function    func(ctx context.Context, state S) (S, error)
}
```

**HOC 借鉴价值**: 
- 替换 HOC 现有的 ad-hoc state map，使用带 Schema 的类型安全状态管理
- 编译期捕获状态相关错误

### 1.2 Schema 系统与 Reducer

**文件**: `graph/schema.go`

**模式**: `StateSchema[S any]` 接口 + 可插拔的 Reducer

```go
type StateSchema[S any] interface {
    Init() S
    Update(current, new S) (S, error)
}

// MapSchema with registered reducers
agentSchema := graph.NewMapSchema()
agentSchema.RegisterReducer("messages", graph.AppendReducer)
agentSchema.RegisterReducer("workflow_plan", graph.OverwriteReducer)

// 内置 Reducer
func AppendReducer(current, new any) (any, error)
func OverwriteReducer(current, new any) (any, error)
```

**HOC 借鉴价值**: 
- HOC 的状态管理可以用 Reducer 处理不同字段类型：对话消息用 append，配置值用 overwrite
- 解决 "append vs overwrite" 的状态合并问题

### 1.3 并行执行

**文件**: `graph/parallel.go`

**模式**: `ParallelNode[S]` 和 `MapReduceNode[S]`

```go
func (pn *ParallelNode[S]) Execute(ctx context.Context, state S) ([]S, error) {
    results := make(chan result, len(pn.nodes))
    var wg sync.WaitGroup
    
    for i, node := range pn.nodes {
        wg.Add(1)
        go func(idx int, n TypedNode[S]) {
            defer wg.Done()
            defer func() { /* panic recovery */ }()
            value, err := n.Function(ctx, state)
            results <- result{index: idx, value: value, err: err}
        }(i, node)
    }
    // Collect and aggregate results
}
```

**HOC 借鉴价值**: 
- 并行工具执行
- 并行文档检索
- Fan-out 查询多个 Agent

### 1.4 Checkpointing 与自动恢复

**文件**: `graph/checkpointing.go`

**模式**: `CheckpointListener[S]` + `CheckpointableStateGraph[S]`

```go
type CheckpointConfig struct {
    Store          store.CheckpointStore
    AutoSave       bool
    SaveInterval   time.Duration
    MaxCheckpoints int
}

// Auto-resume on invocation
func (cr *CheckpointableRunnable[S]) InvokeWithConfig(ctx context.Context, initialState S, config *Config) (S, error) {
    if threadID != "" {
        if latestCP, err := cr.getLatestCheckpoint(ctx, threadID); err == nil && latestCP != nil {
            initialState = cr.mergeStates(ctx, checkpointState, initialState)
            config.ResumeFrom = []string{latestCP.NodeName}
        }
    }
}
```

**HOC 借鉴价值**: 
- 长工作流支持，允许中断和恢复
- 基于线程的 Checkpoint 支持多会话场景

### 1.5 Command 模式

**文件**: `graph/command.go`

**模式**: `Command` 结构体允许节点动态更新状态 AND 控制流程

```go
type Command struct {
    Update any    // Value to merge into state via schema reducers
    Goto   any    // String or []string - overrides graph edges
}

// 节点中返回
return Command{
    Update: map[string]any{"result": "processed"},
    Goto:   "next_node",  // 动态路由
}
```

**HOC 借鉴价值**: 
- Agent 可以基于中间结果动态路由，无需复杂的条件边逻辑

### 1.6 弹性模式

**文件**: `graph/retry.go`

**模式**: `RetryNode`, `TimeoutNode`, `CircuitBreaker`, `RateLimiter`

```go
type RetryConfig struct {
    MaxAttempts     int
    InitialDelay    time.Duration
    MaxDelay        time.Duration
    BackoffFactor   float64
    RetryableErrors func(error) bool
}

type CircuitBreakerConfig struct {
    FailureThreshold int
    SuccessThreshold int
    Timeout          time.Duration
    HalfOpenMaxCalls int
}
```

**HOC 借鉴价值**: 
- 包装不可靠的节点（LLM 调用、外部 API）
- 防止级联失败

### 1.7 Listener/Event 系统

**文件**: `graph/listeners.go`

**模式**: `NodeListener[S]` 接口用于可观测性

```go
type NodeListener[S any] interface {
    OnNodeEvent(ctx context.Context, event NodeEvent, nodeName string, state S, err error)
}

type StreamEvent[S any] struct {
    Timestamp time.Time
    NodeName  string
    Event     NodeEvent
    State     S
    Error     error
    Duration  time.Duration
}

// Streaming
func (lr *ListenableRunnable[S]) Stream(ctx context.Context, initialState S) <-chan StreamEvent[S]
```

**HOC 借鉴价值**: 
- 调试、日志、实时 UI 更新

### 1.8 Interrupt 与 Resume

**文件**: `graph/state_graph.go`, `graph/context.go`

**模式**: 可配置的 interrupt 点

```go
type GraphInterrupt struct {
    Node           string
    State          any
    InterruptValue any
    NextNodes      []string
}

// Resume with value
ctx = WithResumeValue(ctx, userInput)
result, err := runnable.Invoke(ctx, state, config)
```

**HOC 借鉴价值**: 
- 人机协作审批
- 工具权限检查
- 调试断点

---

## 2. 预置代理 (prebuilt/)

### 2.1 Agent 工厂模式

**文件**: `prebuilt/create_agent.go`

**模式**: `CreateAgentMap` 和 `CreateAgent[S]` 工厂函数 + 函数式选项

```go
type CreateAgentOptions struct {
    skillDir               string
    Verbose                bool
    SystemMessage          string
    MaxIterations          int
    DisableModelInvocation bool
}

func WithSystemMessage(message string) CreateAgentOption
func WithMaxIterations(maxIterations int) CreateAgentOption
func WithSkillDir(skillDir string) CreateAgentOption
```

**HOC 借鉴价值**: 
- 为常见 Agent 类型提供工厂函数

### 2.2 ReAct Agent

**文件**: `prebuilt/react_agent.go`

**模式**: Think-Act-Observe 循环

```
agent node (LLM decides action) 
    ↓ (tool calls?)
tools node (execute) 
    ↓
agent node (observe result)
```

### 2.3 Supervisor / 多 Agent 编排

**文件**: `prebuilt/supervisor.go`

**模式**: LLM 驱动的子 Agent 路由

```go
// Supervisor 使用 route tool 选择下一个 Agent
routeTool := llms.Tool{
    Function: &llms.FunctionDefinition{
        Name: "route",
        Parameters: map[string]any{
            "properties": map[string]any{"next": map[string]any{
                "type":  "string",
                "enum":  memberNames,  // ["researcher", "coder", "FINISH"]
            }},
        },
    },
}
```

**HOC 借鉴价值**: 
- 复杂工作流需要多个专业 Agent 时使用

### 2.4 Reflection Agent

**文件**: `prebuilt/reflection_agent.go`

**模式**: Generate → Reflect → Revise 循环

```
generate (生成/修改)
    ↓
reflect (评估)
    ↓ (是否满意?)
  END ←──────────┐
    ↓
generate (重新生成)
```

### 2.5 PEV Agent

**文件**: `prebuilt/pev_agent.go`

**模式**: Plan → Execute → Verify → Repeat/Replan

```
planner → executor → verifier
              ^          |
              |          v (if failed)
              |-----------------> (replan)
              |
              v (if successful)
           executor → ... → synthesizer → END
```

### 2.6 Tree of Thoughts

**文件**: `prebuilt/tree_of_thoughts.go`

**模式**: 多路径并行探索 + 剪枝

```go
type TreeOfThoughtsConfig struct {
    Generator    ThoughtGenerator
    Evaluator    ThoughtEvaluator
    MaxDepth     int
    MaxPaths     int
    InitialState ThoughtState
}
```

---

## 3. 内存系统 (memory/)

### 3.1 Memory 接口

```go
type Memory interface {
    AddMessage(ctx context.Context, msg *Message) error
    GetContext(ctx context.Context, query string) ([]*Message, error)
    Clear(ctx context.Context) error
    GetStats(ctx context.Context) (*Stats, error)
}
```

### 3.2 BufferMemory with Auto-Summarization

**文件**: `memory/buffer.go`

```go
type BufferMemory struct {
    messages      []*Message
    maxMessages   int
    maxTokens     int
    autoSummarize bool
    Summarizer    func(ctx context.Context, messages []*Message) (string, error)
}
```

### 3.3 SummarizationMemory

**文件**: `memory/summarization.go`

- 两层存储：近期（原始）+ 历史（摘要）
- 超过阈值后触发摘要

---

## 4. RAG 系统 (rag/)

### 4.1 LightRAG 引擎

**文件**: `rag/engine/lightrag.go`

**模式**: 四种检索模式结合向量搜索与知识图谱

| 模式 | 描述 |
|------|------|
| **Naive** | 简单向量相似度搜索 |
| **Local** | 实体提取 + 图遍历 N 跳 |
| **Global** | 知识图谱社区级别检索 |
| **Hybrid** | 本地 + 全局reciprocal rank fusion |

```go
func (l *LightRAGEngine) QueryWithConfig(ctx context.Context, query string, config *rag.RetrievalConfig) (*rag.QueryResult, error) {
    switch mode := config.SearchType; mode {
    case "naive":
        return l.naiveRetrieval(ctx, query, config)
    case "local":
        return l.localRetrieval(ctx, query, config)
    case "global":
        return l.globalRetrieval(ctx, query, config)
    case "hybrid":
        return l.hybridRetrieval(ctx, query, config)
    }
}
```

### 4.2 混合检索器

**文件**: `rag/retriever/hybrid.go`

- 可插拔的 Retriever 权重
- 多源 boost（多检索器检索的文档 +10%）
- 分数阈值过滤

---

## 5. 存储系统 (store/)

### 5.1 Checkpoint Store 接口

```go
type CheckpointStore interface {
    Save(ctx context.Context, checkpoint *Checkpoint) error
    Load(ctx context.Context, checkpointID string) (*Checkpoint, error)
    List(ctx context.Context, executionID string) ([]*Checkpoint, error)
    ListByThread(ctx context.Context, threadID string) ([]*Checkpoint, error)
    GetLatestByThread(ctx context.Context, threadID string) (*Checkpoint, error)
    Delete(ctx context.Context, checkpointID string) error
}
```

### 5.2 存储后端

- **Memory**: 内存检查点
- **File**: 文件检查点
- **SQLite**: SQLite 后端
- **PostgreSQL**: PostgreSQL 后端
- **Redis**: Redis 后端

---

## 6. 创新模式总结

### 6.1 泛型 + 无类型双 API

```go
// 快速原型
func CreateAgentMap(model llms.Model, inputTools []tools.Tool, ...) (*graph.StateRunnable[map[string]any], error)

// 生产类型安全
func CreateAgent[S any](model llms.Model, getMessages func(S) []llms.MessageContent, ...) (*graph.StateRunnable[S], error)
```

### 6.2 节点包装器

```go
g.AddNodeWithRetry("api_call", "Description", apiNodeFunc, retryConfig)
g.AddNodeWithTimeout("slow_op", "Description", slowNodeFunc, 30*time.Second)
g.AddNodeWithCircuitBreaker("unstable", "Description", unstableFunc, cbConfig)
```

### 6.3 Context 依赖注入

```go
ctx = WithConfig(ctx, config)  // graph/context.go
func GetConfig(ctx context.Context) *Config
func GetResumeValue(ctx context.Context) any
```

### 6.4 通过 Channel 流式输出

```go
eventChan := runnable.Stream(ctx, initialState)
for event := range eventChan {
    fmt.Printf("Node: %s, Event: %s\n", event.NodeName, event.Event)
}
```

---

## 7. HOC 实施建议

### 7.1 Phase 1: 状态管理增强

- [ ] 引入 Schema + Reducer 模式处理状态合并
- [ ] 定义 HOC 核心状态结构

### 7.2 Phase 2: Checkpoint 支持

- [ ] 实现 Checkpoint Store 接口
- [ ] 支持工作流中断与恢复

### 7.3 Phase 3: Agent 模式

- [ ] 采用 Supervisor 模式进行多 Agent 编排
- [ ] 实现 PEV / Reflection 模式

### 7.4 Phase 4: 弹性与可观测性

- [ ] 添加 Retry / Circuit Breaker 包装
- [ ] 实现 Listener / Streaming

### 7.5 Phase 5: 内存与 RAG

- [ ] 集成 Memory 接口
- [ ] 引入 LightRAG 混合检索

---

## 参考文档

- LangGraphGo 源码: `/Users/zhangshiyu/langgraphgo/`
- 图引擎: `graph/state_graph.go`, `graph/schema.go`, `graph/parallel.go`
- 预置代理: `prebuilt/create_agent.go`, `prebuilt/supervisor.go`
- 内存: `memory/buffer.go`, `memory/summarization.go`
- RAG: `rag/engine/lightrag.go`
- 存储: `store/checkpoint.go`

---

*分析日期: 2026-04-06*
