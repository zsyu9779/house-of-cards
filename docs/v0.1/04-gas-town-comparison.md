# House of Cards — Gas Town 对比分析与优化建议

> 基于 Gas Town 深度调研报告的分析
> 创建日期：2026-02-28

---

## 一、总体评估：我们的设计已经很好

回顾 `docs/02-design-v3-final.md` 中的设计立场，House of Cards 已经正确地从 Gas Town 汲取了两大核心智慧：

| 我们做对的 | Gas Town 的教训 |
|----------|----------------|
| SQLite + Git（零额外依赖） | Dolt 太重，运维复杂 |
| 进程管理抽象层（tmux 可选） | 强绑 tmux，容器/CI 不可用 |
| Gazette 协议（信息凝练） | Mail 传完整 body，无压缩 |
| 可配置协作拓扑 | 固定星形 Mayor→Polecat |
| 单一 CLI，自包含 | beads CLI 紧耦合 |

**我们的核心优势已经确立**，但 Gas Town 仍有一些值得借鉴的细节机制。

---

## 二、可借鉴的机制（从小处着手）

### 2.1 Hook 队列机制（Gas Town 的 Polecat Hook）

**现状**：我们的 Whip 直接查询 `ListIdleMinisters()` + `ListReadyBills()`，每次 tick 重新匹配。

**Gas Town 做法**：每个 Polecat 有一个 Hook（ pinned bead），任务分配时写入 Hook，Agent 启动时读取 Hook 立即执行（GUPP 原则）。

**我们可以借鉴**：
- 为每个 Minister 在 DB 中维护一个 `hook` 字段（待处理 Bill ID 队列）
- Whip 分配时写入 `hook`，而不是每次都重新匹配
- 这让 Minister 有"队列"概念，支持批量分配

**实现位置**：`internal/store/store.go` 的 `Minister` 表新增 `hook` JSON 字段

```go
// internal/store/store.go - Minister 表
type Minister struct {
    // ... existing fields
    Hook    string  // JSON array of bill IDs waiting for this minister
}
```

**优先级**：中等（Phase 8 或后续优化）

---

### 2.2 Done 文件检测自动化（与 Phase 8 规划一致）

**现状**：Phase 8 规划了 `.hoc/bill-<id>.done` 文件检测。

**Gas Town 做法**：Bead 有明确状态（open → in_progress → closed），Agent 完成时自动更新 bead 状态。

**我们的优化**：
- 已在 Phase 8 规划中（done 文件轮询）
- 建议增加一个更可靠的机制：**Whip 每次 tick 检查 Chamber 目录中的 `.hoc/inbox/`**

**实现位置**：`internal/whip/whip.go` 的 `pollInbox()` 函数

---

### 2.3 OpenTelemetry 可观测性

**现状**：`log/slog` 接入是 Phase 6B 的目标。

**Gas Town 做法**：0.8.0 完整接入 OpenTelemetry，有 `gt feed` 三面板 TUI、`gt dashboard` Web UI、`gt vitals` 健康仪表盘。

**我们可以借鉴**：
- Phase 6B 先完成 `slog` 接入
- Phase 7 增强 `hoc floor` TUI（已有基础）
- 后续版本可考虑 OpenTelemetry 埋点（可选）

**当前优先级**：低（先把核心功能做好）

---

### 2.4 Plugin/Formula 可扩展性

**Gas Town 做法**：Plugin 系统允许定义门控类型（cooldown、cron、condition、event、manual），Deacon 巡逻循环触发。

**我们可以借鉴**：
- 当前 Session TOML 已是类似 Formula 的工作流定义
- 未来可以支持 `hoc formula list/apply` 命令
- 内置 Formula：如 `rebase-all`、`cleanup-chambers`、`auto-merge`

**当前优先级**：低（Phase 10 平台化阶段考虑）

---

## 三、需要改进的设计点

### 3.1 Minister 空闲池化的缺失

**问题**：当前 `cabinet reshuffle` 只给每个部长分配一个 Bill（first-match），Bill 完成后 Minister 变 idle 但没有自动接新活的机制。

**Gas Town 做法**：`gt sling` 时重用已有 worktree，polecat 进入 idle 而非销毁，下次 `gt sling` 复用。

**我们的优化方向**：
- Phase 8 的 `minister auto` 模式可以解决这个问题
- 或在 Whip 的 `orderPaper()` 中增加：如果有 idle Minister + 有 pending Bills，自动再次分配

**建议**：在 Phase 8 实现 `minister auto` 时，确保支持连续承接多个 Bill

---

### 3.2 合并冲突解决的智能化

**现状**：Privy Council 做 git merge，冲突时生成 Conflict Gazette，需要人工/AI 介入解决。

**Gas Town 做法**：Refinery 有完整的合并队列状态机，理解冲突语义（虽然文档说智能有限）。

**我们的优化方向**：
- Phase 8 可增强 Privy Council：自动尝试 `git merge --abort` + `git rebase` 策略组合
- 生成更智能的 Conflict Gazette，包含：
  - 冲突文件列表
  - 每个文件的冲突块数量
  - 建议解决方向（保留ours/ theirs/ 手动）

**当前实现**：`internal/privy/privy.go` 的 `MergeSession` 函数

---

### 3.3 多项目会期的简化

**Phase 10 规划**：Session 支持多仓库（`projects []string`）。

**Gas Town 做法**：每个 Rig（项目）有独立的 bead 数据库，跨 Rig 通过 Town 级 beads（hq-* 前缀）协调。

**我们的优化**：
- 保持简单：Session 的 `projects` 字段用逗号分隔即可
- Chamber 创建时根据 Bill 的 `project` 字段选择对应的 worktree
- Privy Council 合并时分仓库串行执行

---

## 四、术语对照与概念对齐

| Gas Town 术语 | House of Cards 术语 | 对比 |
|--------------|-------------------|------|
| Polecat | Minister | 相同理念：持久身份 + 短暂会话 |
| Hook | Bill Assignment | 我们更简单：直接分配，无需队列 |
| Bead | Bill | 相同 |
| Convoy | Session | 相同（批量工作追踪） |
| Witness | Committee | 我们更明确：审查角色 |
| Refinery | Privy Council | 相同：合并冲突处理 |
| Mail | Gazette | 我们更好：强制摘要，不传原文 |
| CV | Hansard | 相同 |
| GUPP | Whip 推进力 | 我们已实现类似逻辑 |
| Daemon + Boot + Deacon | Whip | 我们更简洁：单一 Whip 角色 |
| Formula / Molecule | Session TOML | 我们更轻：单文件定义 |

---

## 五、具体可执行的优化建议

### 5.1 短期（Phase 6B-7）

| 优化项 | 描述 | 借鉴来源 |
|-------|------|---------|
| 完善 slog 日志 | 将 Whip/Speaker/Privy 的 `fmt.Fprintf` 替换为 slog | Gas Town OTEL 基础 |
| DB 索引 | `idx_ministers_status`, `idx_bills_session_id` 等 | 性能优化 |
| `--json` 输出 | 便于外部工具集成 | API 化基础 |

### 5.2 中期（Phase 8）

| 优化项 | 描述 | 借鉴来源 |
|-------|------|---------|
| done 文件自动检测 | `.hoc/bill-<id>.done` 轮询 | Gas Town bead 状态机 |
| Minister 连续承接 | idle Minister 自动接新 Bill | Gas Town polecat 复用 |
| Gazette tmux 投递 | 投递到 tmux pane | Gas Town tmux 集成 |

### 5.3 长期（Phase 9-10）

| 优化项 | 描述 | 借鉴来源 |
|-------|------|---------|
| Speaker Patrol 增强 | 每 60s 决策循环 | Gas Town Deacon 巡逻 |
| OpenTelemetry | metrics/traces 埋点 | Gas Town 0.8.0 |
| API Server | REST API + webhook | Gas Town 运维化 |

---

## 六、结论

House of Cards 的设计已经很好地继承了 Gas Town 的核心智慧（持久身份、Git 沙箱、推进力），并通过以下创新超越了 Gas Town：

1. **政府隐喻**比小镇隐喻更直观
2. **Gazette 协议**强制信息凝练
3. **SQLite** 避免了 Dolt 的运维负担
4. **可选 tmux** 避免了强绑定

**剩余的优化空间**主要是：
- 细节机制的完善（Hook 队列、done 文件检测）
- 可观测性增强（slog → OpenTelemetry）
- 智能化提升（冲突解决、Speaker 决策）

这些都在 Phase 2 路线图的各个阶段中有对应规划。

---

*本分析基于 Gas Town research report (`/Users/zhangshiyu/gastown/gas-town-research-report.md`) 及 House of Cards 当前实现。*
