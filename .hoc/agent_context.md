# Agent Context - 当前工作上下文

> 每次交互必读必写

## 项目状态

- **阶段**: v0.2 Phase 1 全部完成 + 巡检修复完成（2026-03-08），待开始 Phase 2
- **目标**: 下一步 Phase 2 — A-1 CLAUDE.md + B-1 Gazette+ACK 协议

## 最近工作

### Phase 1 巡检修复（2026-03-08）

对照 `docs/v0.2-feature-spec.md` + `docs/v0.2/` 草案巡检，发现并修复 3 个问题：

| # | 问题 | 修复 |
|---|------|------|
| 1 (Critical) | RecordEvent 全项目零调用 | 在 6 个文件追加 19 处调用，覆盖 Spec 列出的全部 16 个 topic |
| 2 (Medium) | session open 缺 `--force` | 添加 `--force` flag + 校验跳过逻辑 |
| 3 (Low) | serve.go 未集成校验 | 确认当前无 bill 创建 API，属"未来端点"，暂不改 |

RecordEvent 调用分布：
- `liveness.go`(3): minister.stuck, by_election.triggered/completed
- `scheduler.go`(7): bill.assigned, session.completed(x2), privy.merge_success/conflict, autoscale.triggered(x2)
- `poller.go`(3): bill.enacted, committee.assigned/result
- `dispatch.go`(2): gazette.delivered (tmux + inbox)
- `cmd/bill.go`(1): bill.created
- `cmd/session.go`(1): bill.created

### v0.2 Phase 1 全部完成（2026-03-08）

| Feature | 编号 | 状态 |
|---------|------|------|
| 统一事件表 | D-1 | ✅ 完成（含 19 处 RecordEvent 埋点） |
| Bill 入口校验 | D-2 | ✅ 完成（bill draft + session open 均支持 --force） |
| GitHub Actions CI | E-1 | ✅ 完成 |
| Whip 子模块拆分 | C-1 | ✅ 完成（6 文件） |

### 更早的工作（已归档）

- v0.2 Feature 规格文档整合定稿
- 巡检修复 6 任务全部 PASS，覆盖率 31.6% -> 40.3%
- v0.2 总体草案、v0.1 文档归档

## v0.2 实施路线进度

| Phase | 内容 | 状态 |
|-------|------|------|
| **Phase 1** | D-1 事件表 + D-2 入口校验 + E-1 CI + C-1 Whip 拆分 | **✅ 完成** |
| Phase 2 | A-1 CLAUDE.md + B-1 Gazette+ACK + C-1 测试补强 | 待开始 |
| Phase 3 | A-2 Autoscale + A-3 API + B-2/B-3 + D-3 治理 + E-2 | 待开始 |
| Phase 4 | C-2 Doctor + C-3 回放 + E-3/E-4 + 质询度量 | 待开始 |

## 待解决问题

| 问题 | 说明 | 优先级 |
|------|------|--------|
| serve.go bill 创建 API 校验 | 当前无 bill 创建端点，A-3 实施时集成 | P1 |
| whip tick/threeLineWhip 测试 | 依赖真实 tmux/PID | P2 |
| privy AnalyzeBranch 测试 | 需更复杂 git 分叉场景 | P2 |

## 下一步候选

1. **开始 Phase 2**：A-1 Minister CLAUDE.md 写入 + B-1 Gazette 结构化 + ACK 协议
2. B-1 分 3 步：Step 1 结构化 payload → Step 2 ACK 基础设施 → Step 3 Question Time

---
