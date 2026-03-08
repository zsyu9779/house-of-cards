# House of Cards — 第三阶段（Phase 3）路线图

> 基于 Gas Town 深度分析与 Phase 2 完成总结
> 文档创建：2026-03-01
> 适用版本：Phase 3 起

---

## 一、Phase 2 完成总结

### 已完成里程碑

| Phase | 状态 | 完成时间 |
|-------|------|---------|
| Phase 6B — 代码工程化 | ✅ | 2026-02-XX |
| Phase 7 — 可视化与监控增强 | ✅ | 2026-02-XX |
| Phase 8 — 自动化闭环 | ✅ | 2026-02-XX |
| Phase 9 — Speaker AI 增强 | ✅ | 2026-02-XX |
| Phase 10 — 平台化扩展 | ✅ | 2026-03-01 |

### 当前遗留问题（需在 Phase 3 解决）

| 问题 | 描述 | 优先级 |
|------|------|--------|
| `cabinet reshuffle` 每个部长只分配一份议案（first-match） | 非阻塞，但影响吞吐量 | 中 |
| Whip `privyAutoMerge` 依赖 `session.project` 字段 | 旧会期需手动更新 | 低 |
| `hoc whip start` 默认 WARN 级别 slog 不输出运行日志 | 需 `--verbose` | 低 |
| DAG 渲染多父节点汇聚显示 | MVP 定位，可接受 | 低 |

---

## 二、Gas Town 对比分析结论

### 我们已做得比 Gas Town 好的

| 特性 | House of Cards | Gas Town |
|------|---------------|----------|
| 持久化 | SQLite + Git | Dolt（太重） |
| 进程管理 | 可选 tmux | 强绑 tmux |
| 信息凝练 | Gazette 协议 | Mail 传完整 body |
| 协作拓扑 | 可配置 DAG | 固定星形 |
| CLI | 单一 `hoc` | beads CLI 紧耦合 |

### Phase 3 需要借鉴的 Gas Town 机制

| 机制 | Gas Town 做法 | 我们现状 | Phase 3 目标 |
|------|--------------|---------|-------------|
| **Hook 队列** | 每个 Polecat 有 pinned bead 队列 | Whip 每次 tick 重新匹配 | Minister hook 字段，队列化分配 |
| **Done 文件检测** | Bead 状态机自动流转 | Phase 8 已规划 | 增强可靠性 |
| **OpenTelemetry** | 完整 OTEL 埋点 | Phase 6B 完成 slog | 升级 OTEL |
| **Plugin/Formula** | 可扩展门控类型 | Session TOML 已有 | 开放插件系统 |

---

## 三、Phase 3 目标与主题

### 主题：智能自主与生态开放

> 从「可运行的工具」到「智能化的协作平台」

### 核心目标

1. **智能化**：冲突解决自动化 + AI 辅助决策增强
2. **可观测性**：OpenTelemetry 全链路埋点
3. **生态化**：Plugin/Formula 扩展系统
4. **体验优化**：Hook 队列 + 空闲池化

---

## 四、Phase 3 详细规划

### Phase 3A — 智能冲突解决（估时：2 周）

#### 目标

将 Privy Council 从「标记冲突」升级为「智能解决」。

#### 任务列表

##### 3A-1：冲突理解增强

- 解析 `git merge` 输出，
- 对提取冲突文件列表每个冲突文件，提取冲突块数量（`<<<<<<< HEAD` 计数）
- 生成结构化 Conflict Gazette，包含：
  - 冲突文件路径
  - 每个文件的冲突块数量
  - 冲突类型（内容冲突 / 删除 vs 修改 / 二者皆修改）

```go
// internal/privy/privy.go - 增强版冲突分析
type ConflictInfo struct {
    File       string   // 冲突文件路径
    Blocks     int      // 冲突块数量
    Type       string   // "content" | "delete_vs_modify" | "both_modified"
    OurSHA     string   // ours 分支最新 commit
    TheirSHA   string   // theirs 分支最新 commit
}
```

**改动文件**：
```
internal/privy/privy.go     ← 增强 ConflictGazette 内容
cmd/privy.go               ← 新增 privy analyze 子命令
```

##### 3A-2：自动解决策略尝试

- Whip 触发 Privy Council 时，自动尝试策略链：
  1. `git merge --abort`（清理状态）
  2. `git rebase -X theirs`（保留对方修改）
  3. `git merge -X ours`（保留我方修改）
  4. 如皆失败，生成增强版 Conflict Gazette

- 记录每次尝试结果到日志，供后续优化

**改动文件**：
```
internal/privy/privy.go     ← MergeSession 增强策略链
```

##### 3A-3：冲突解决 Gazette 模板

- 定义冲突解决后的标准 Gazette 格式：
```markdown
# Conflict Resolution Gazette: [Bill Title]
> Bill: bill-a1b2c3 | Resolved by: Minister of Backend | Date: 2026-03-01

## 冲突文件
- `api/auth.go` — 3 块冲突，采用 ours
- `models/user.go` — 1 块冲突，手动合并

## 解决策略
- 自动合并尝试：2 次
- 最终方案：手动合并 + 代码重构

## 遗留问题
- 待与 frontend 确认 API 契约变更
```

**改动文件**：
```
internal/privy/privy.go     ← ResolveConflict() 生成 Gazette
```

#### 验收标准

- [ ] `git merge` 冲突后，Conflict Gazette 包含冲突文件列表和块数量
- [ ] Privy Council 自动尝试 3 种合并策略
- [ ] 冲突解决 Gazette 符合模板格式

---

### Phase 3B — Hook 队列与空闲池化（估时：1 周）

#### 目标

借鉴 Gas Town Polecat Hook 机制，实现 Minister 队列化分配。

#### 任务列表

##### 3B-1：Minister Hook 字段

- 在 `internal/store/store.go` 的 Minister 表新增 `Hook` JSON 字段：
```go
// internal/store/store.go
type Minister struct {
    // ... existing fields
    Hook    string  // JSON array of bill IDs waiting for this minister
}
```

- 提供队列操作接口：
```go
func (s *Store) PushHook(ministerID, billID string) error
func (s *Store) PopHook(ministerID string) (string, error) // 返回最早进入的 bill ID
func (s *Store) PeekHook(ministerID string) ([]string, error)
```

**改动文件**：
```
internal/store/store.go     ← Minister.Hook 字段 + 队列操作
```

##### 3B-2：Whip 队列化分配

- Whip 分配时优先检查 Minister hook
- 如果 hook 非空，直接分配给该 Minister
- 如果 hook 为空，执行现有 first-match 逻辑

```go
// internal/whip/whip.go
func (w *Whip) assignWithHook(bill Bill) error {
    // 1. 查找有非空 hook 且 skills 匹配的 Minister
    minister := w.store.FindMinisterWithMatchingHook(bill)
    if minister != nil {
        return w.assignBill(bill, minister)
    }
    // 2. fallback: 现有匹配逻辑
    return w.assignBillFirstMatch(bill)
}
```

**改动文件**：
```
internal/whip/whip.go      ← assignWithHook() 函数
```

##### 3B-3：Minister 连续承接

- 实现 Phase 8 规划的 `minister auto` 连续承接机制
- Bill 完成后，Minister 变为 idle 时自动检查 hook 队列
- 如果有 pending bills，自动接新活

```go
// internal/whip/whip.go - pollIdleMinisterReassign
func (w *Whip) pollIdleMinisterReassign() {
    idleMinisters := w.store.ListIdleMinisters()
    pendingBills := w.store.ListBillsByStatus("reading")

    for _, m := range idleMinisters {
        // 检查 hook 队列
        billID, _ := w.store.PopHook(m.ID)
        if billID != "" {
            w.assignBill(billID, m)
        }
    }
}
```

**改动文件**：
```
internal/whip/whip.go      ← pollIdleMinisterReassign() 函数
```

#### 验收标准

- [ ] Minister 有 hook 字段，可存储多个 bill ID
- [ ] Whip 分配时优先使用 hook 队列
- [ ] idle Minister 自动接新 Bill（连续承接）

---

### Phase 3C — OpenTelemetry 可观测性（估时：2 周）

#### 目标

从 slog 升级到完整 OpenTelemetry 埋点。

#### 任务列表

##### 3C-1：OTEL 基础架构

- 添加 OpenTelemetry 依赖：
```go
// go.mod
go.opentelemetry.io/otel        v1.21.0
go.opentelemetry.io/otel/trace v1.21.0
go.opentelemetry.io/otel/sdk   v1.21.0
```

- 在 `internal/otel/` 创建观测基础设施：
```
internal/otel/
├── tracer.go      // 追踪器初始化
├── metrics.go     // 指标定义
└── exporter.go   // 导出器（stdout / otlp）
```

```go
// internal/otel/tracer.go
func InitTracer(serviceName string, exporterType string) (*tracing.TracerProvider, error)
```

**改动文件**：
```
internal/otel/                  ← 新建目录
cmd/root.go                    ← 初始化 OTEL
go.mod                         ← 添加依赖
```

##### 3C-2：关键路径埋点

- 为核心操作添加 span：
  - `whip.tick` — Whip 主循环
  - `minister.summon` — Minister 启动
  - `minister.dismiss` — Minister 结束
  - `privy.merge` — 合并操作
  - `gazette.dispatch` — 公报投递

- 添加关键指标：
  - `hoc_ministers_active_total` — 活跃 Minister 数量
  - `hoc_bills_duration_seconds` — Bill 完成耗时
  - `hoc_by_election_total` — 补选次数
  - `hoc_conflicts_total` — 冲突次数

**改动文件**：
```
internal/whip/whip.go          ← 添加 span
internal/privy/privy.go        ← 添加 span
internal/runtime/claudecode.go ← 添加 span
```

##### 3C-3：OTEL 导出配置

- 支持 3 种导出模式：
  - `stdout` — 开发调试（JSON 行）
  - `otlp` — 发送到 OTEL Collector
  - `nop` — 禁用（生产默认）

- 配置项：
```toml
# .hoc/config.toml
[observability]
exporter = "stdout"  # stdout | otlp | nop
otlp_endpoint = "localhost:4317"
service_name = "house-of-cards"
```

**改动文件**：
```
internal/config/config.go      ← observability 配置
internal/otel/exporter.go     ← 导出器实现
```

#### 验收标准

- [ ] `hoc whip start` 输出 JSON 格式 trace 到 stdout
- [ ] 活跃 Minister 数量可通过 metrics 查询
- [ ] Bill 完成耗时被记录到 histogram

---

### Phase 3D — Plugin/Formula 可扩展性（估时：2 周）

#### 目标

借鉴 Gas Town Plugin 系统，开放扩展能力。

#### 任务列表

##### 3D-1：Formula 定义格式

- Formula = 预定义工作流模板
- 位置：`~/.hoc/formulas/` 或项目 `.hoc/formulas/`

```toml
# formulas/rebase-all.toml
[formula]
name = "rebase-all"
description = "Rebase all feature branches onto main"
trigger = "manual"  # manual | cron | event

[steps]
[[steps.action]]
type = "git"
command = "fetch origin"
targets = ["chambers/*"]

[[steps.action]]
type = "git"
command = "rebase origin/main"
targets = ["chambers/*"]

[[steps.on_failure]]
type = "notify"
message = "Rebase failed for {{.Target}}"
```

- 支持变量替换：`{{.Variable}}`
- 支持条件：`if: "{{.Branch}}" != "main"`

**改动文件**：
```
internal/formula/
├── parser.go      ← TOML 解析
├── executor.go   ← 执行引擎
└── types.go      ← 类型定义
```

##### 3D-2：Plugin 接口

- Plugin = 自定义门控/触发器
- 位置：`~/.hoc/plugins/`（Go plugin 或脚本）

```go
// internal/plugin/plugin.go
type Plugin interface {
    Name() string
    OnBillCreated(bill *Bill) error
    OnBillCompleted(bill *Bill) error
    OnMinisterIdle(minister *Minister) error
}
```

- 初始内置插件：
  - `cooldown` — 技能冷却时间
  - `cron` — 定时触发
  - `condition` — 条件门控

**改动文件**：
```
internal/plugin/
├── loader.go     ← 插件加载
├── runner.go    ← 插件执行
└── registry.go  ← 内置插件注册
```

##### 3D-3：hoc formula 命令

```bash
# 列出可用 Formula
hoc formula list

# 应用 Formula
hoc formula apply rebase-all --targets "chambers/*"

# 查看 Formula 状态
hoc formula status rebase-all
```

**改动文件**：
```
cmd/formula.go               ← 新建
cmd/root.go                 ← 注册 formulaCmd
```

##### 3D-4：Formula 市场（可选）

- 内置 5 个实用 Formula：
  1. `cleanup-chambers` — 清理空闲超过 24h 的 chamber
  2. `auto-merge` — 自动合并已通过审查的分支
  3. `sync-main` — 同步所有 chamber 到最新 main
  4. `health-check` — 全局健康检查
  5. `archive-session` — 归档已完成会期

**改动文件**：
```
internal/formula/builtins.go ← 内置 Formula
```

#### 验收标准

- [ ] `hoc formula list` 显示内置 Formula
- [ ] `hoc formula apply cleanup-chambers` 正确执行
- [ ] Plugin 接口可被外部扩展实现

---

### Phase 3E — 体验优化与增强（估时：1 周）

#### 目标

解决遗留问题，提升用户体验。

#### 任务列表

##### 3E-1：批量分配优化

- `cabinet reshuffle` 支持批量分配模式：
```bash
# 每个部长分配最多 N 个 Bills
hoc cabinet reshuffle --max-per-minister 3
```

- Whip 分配时考虑负载均衡：
```go
// internal/whip/whip.go
func (w *Whip) assignBillWithLoadBalancing(bill Bill) error {
    // 选择负载最低的匹配 Minister
    minister := w.store.FindLeastLoadedMinister(bill.Portfolio)
    return w.assignBill(bill, minister)
}
```

**改动文件**：
```
internal/whip/whip.go      ← FindLeastLoadedMinister()
cmd/cabinet.go             ← --max-per-minister flag
```

##### 3E-2：Whip 日志默认输出

- 修改默认日志级别为 `INFO`
- `--verbose` 保留 `DEBUG`
- 添加 `--quiet` 模式（只输出 ERROR）

**改动文件**：
```
cmd/root.go                ← 默认 INFO，添加 --quiet
internal/whip/whip.go      ← 调整关键日志级别
```

##### 3E-3：Session 项目字段兼容性

- 自动检测旧 Session（无 project 字段）
- 回退到全局 project 配置
- 迁移脚本：`hoc session migrate`

**改动文件**：
```
cmd/session.go             ← 迁移子命令
internal/store/store.go   ← MigrateSessionProject()
```

#### 验收标准

- [ ] `cabinet reshuffle --max-per-minister 3` 正确批量分配
- [ ] `hoc whip start` 无需 `--verbose` 即输出运行日志
- [ ] 旧 Session 自动兼容（无需手动更新）

---

## 五、技术设计要点

### 3.1 Hook 队列数据结构

```sql
-- Minister 表扩展
ALTER TABLE ministers ADD COLUMN hook TEXT DEFAULT '[]';

-- JSON 格式：["bill-a1b2c3", "bill-d4e5f6"]
```

### 3.2 Formula 执行模型

```
Formula Apply
    │
    ├─► 解析 TOML
    │       │
    │       ▼
    ├─► 变量替换
    │       │
    │       ▼
    ├─► 步骤验证
    │       │
    │       ▼
    ├─► 并发/串行执行
    │       │
    │       ▼
    ├─► 错误处理
    │       │
    │       ▼
    └─► 结果报告
```

### 3.3 OpenTelemetry 链路

```
┌─────────────┐     ┌──────────────┐     ┌────────────────┐
│ House of    │     │ OTLP Exporter│     │ OTEL Collector │
│ Cards (Go)   │────▶│ (grpc/http)  │────▶│                │
└─────────────┘     └──────────────┘     └───────┬────────┘
                                                 │
                                                 ▼
                                        ┌────────────────┐
                                        │ Jaeger/Zipkin  │
                                        │ / Prometheus   │
                                        └────────────────┘
```

### 3.4 冲突解决策略链

```
Privy Council Merge
        │
        ├─► 策略 1: git merge (默认)
        │       │
        │       ├─► 成功 → 完成
        │       │
        │       └─► 冲突 → 策略 2
        │
        ├─► 策略 2: git rebase -X theirs
        │       │
        │       ├─► 成功 → 完成
        │       │
        │       └─► 冲突 → 策略 3
        │
        ├─► 策略 3: git merge -X ours
        │       │
        │       ├─► 成功 → 完成
        │       │
        │       └─► 冲突 → 生成增强版 Conflict Gazette
        │
        └─► 记录每次尝试结果到日志
```

---

## 六、依赖关系

```
Phase 3A（智能冲突）
    │
    └─► Phase 3B（Hook 队列）
            │
            └─► Phase 3C（OpenTelemetry）
                    │
                    └─► Phase 3D（Plugin/Formula）
                            │
                            └─► Phase 3E（体验优化）
```

### 外部依赖

| 依赖 | 版本 | 用途 | 引入阶段 |
|------|------|------|---------|
| `go.opentelemetry.io/otel` | v1.21+ | 可观测性 | Phase 3C |
| `go.opentelemetry.io/otel/sdk` | v1.21+ | SDK | Phase 3C |
| `github.com/BurntSushi/toml` | v1.3+ | Formula 解析 | Phase 3D |

---

## 七、里程碑检查点

| 检查点 | 条件 | 目标日期 |
|--------|------|---------|
| 3A 完成 | Conflict Gazette 包含文件列表和块数量 | Week 2 |
| 3B 完成 | Minister hook 队列正常工作 | Week 3 |
| 3C 完成 | OTEL trace 输出到 stdout | Week 5 |
| 3D 完成 | Formula 可列出和执行 | Week 7 |
| 3E 完成 | 批量分配 + 日志默认输出 | Week 8 |

---

## 八、与 Gas Town 的最终对比

| 维度 | Gas Town | House of Cards Phase 3 |
|------|----------|----------------------|
| 冲突解决 | Refinery 标记冲突 | **智能策略链 + 自动尝试** |
| 任务队列 | Polecat Hook | **Minister Hook + Whip 队列化** |
| 可观测性 | OpenTelemetry | **OpenTelemetry（同等）** |
| 扩展性 | Plugin 系统 | **Formula + Plugin（同等）** |
| 负载均衡 | 未知 | **Least-loaded 分配** |

---

## 九、后续展望

Phase 3 完成后的可能方向：

1. **AI 决策增强**
   - 基于 Hansard 数据的技能匹配优化
   - Bill 复杂度预测（决定分配策略）
   - 冲突自动解决（LLM 介入）

2. **多租户支持**
   - 多个独立 House 可以协作
   - 跨 House 的 Gazette 路由

3. **云原生**
   - Kubernetes Operator
   - Helm Chart
   - 云端存储（替代本地 SQLite）

---

*文档维护：每个阶段完成后更新对应章节状态。*

*下一步：Phase 3A — 智能冲突解决。*
