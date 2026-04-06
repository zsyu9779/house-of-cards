# Agent Context - 当前工作上下文

> 每次交互必读必写

## 项目状态

- **阶段**: v0.3 Phase 1 实施中
- **版本定位**: 质量强化版——不加新功能，专注错误处理、测试、日志、Lint
- **当前进度**: PR-2 (E-1.1 Whip 错误治理) ✅ 完成

## 最近工作（2026-04-06）

### PR-2: E-1.1 Whip 错误治理 + 恢复梯度 ✅

**Store 层**:
- Minister struct 新增 `RecoveryAttempts int` 字段
- migrate() 新增 `ALTER TABLE ministers ADD COLUMN recovery_attempts INTEGER DEFAULT 0`
- 所有 7 个 minister SELECT/Scan 更新为包含 `COALESCE(recovery_attempts,0)`
- 新增 `IncrementRecoveryAttempts()` 和 `ResetRecoveryAttempts()` 方法

**liveness.go**:
- Pass 1: 心跳更新错误处理 + 恢复重置逻辑（RecoveryAttempts > 0 时 reset）
- Pass 2: 完整替换为三级恢复梯度（checkpoint → at-risk → by-election）
- byElection: RecordEvent 改为 slog.Warn

**poller.go** (全部修复):
- pollDoneFiles: RecordEvent → slog.Warn, UpdateMinisterStatus → critical, os.Remove → best-effort
- committeeAutomation: RecordEvent → slog.Warn
- pollReviewFile: UpdateBillStatus × 2 → critical+return, UnassignBill → critical, UpdateMinisterStatus → critical, os.Remove → best-effort, RecordEvent/CreateHansard/CreateGazette → slog.Warn
- pollAckFiles: UpdateMinisterStatus → critical+continue, os.Remove → best-effort
- collectQuestionMetrics: UpdateHansardMetrics → slog.Warn

**scheduler.go** (全部修复):
- privyAutoMerge: UpdateSessionStatus × 4 → critical+return, RecordEvent × 4 → slog.Warn, CreateGazette 已有错误处理
- autoAssign: RecordEvent → slog.Warn
- autoscale: UpdateMinisterStatus × 2 → critical (成功后才发 Gazette/RecordEvent), RecordEvent × 2 → slog.Warn, CreateGazette × 2 → slog.Warn

**dispatch.go**:
- os.WriteFile → best-effort 注释, RecordEvent → slog.Warn

**验证**: `golangci-lint run` 零 error + `go build` + `go vet` + `go test -race` 全部通过
- `grep '_ = w.db.' internal/whip/` = 0 匹配（关键路径零 _ =）
- `grep '_ = os.' internal/whip/` = 4 匹配，全部有 `// best-effort:` 注释

## 下一步

继续 Phase 1：
1. ~~**PR-1**: C-1 Linter 升级~~ ✅
2. ~~**PR-2**: E-1.1 Whip 错误治理~~ ✅
3. **PR-3**: E-1.2 Serve 错误治理
4. **PR-4**: E-2 配置校验

---
