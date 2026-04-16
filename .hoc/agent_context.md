# Agent Context - 当前工作上下文

> 每次交互必读必写

## 项目状态

- **阶段**: v0.3 Phase 2 进行中
- **版本定位**: 质量强化版——不加新功能，专注错误处理、测试、日志、Lint
- **当前进度**: PR-5 完成

## 最近工作（2026-04-07）

### PR-5: B-1 Whip 核心路径测试 ✅

**覆盖率**: 17.8% → 62.3%（目标 60%+ 达成）

**修复的编译问题**:
- 4 个文件中的重复 `contains` helper → 统一到 `whip_test.go`
- `db.Exec()` → `db.DB().Exec()`（store.DB 不直接暴露 Exec）
- CreateMinister 不插入 `worktree`/`recovery_attempts` → 使用 `UpdateMinisterWorktree` 和原始 SQL
- `COALESCE(assignee,'')` 导致 `Assignee.Valid` 始终为 true → 改用 `Assignee.String != ""`
- TOML 测试中 dotted key (`api.go`) 需要引号
- 多处未使用的 `sess` 变量
- 未使用的 helper 函数 `mustCreateMinisterWithStatus`/`mustCreateMinisterFull`

**新增测试（scheduler_test.go）**:
- `TestEpicIsComplete_AllTerminal` / `SomeInProgress` / `NoSubBills` — epicIsComplete 0%→100%
- `TestBuildUpstreamGazette_WithDeps` / `NoDeps` — buildUpstreamGazetteSection 15%→85%
- `TestAutoAssign_CreatesGazetteAndUpdates` — autoAssign 61.5%
- `TestPrivyAutoMerge_NoBranches` — privyAutoMerge early-return 路径
- `TestAutoscale_ScaleUp` / `ScaleDown` / `NoAction` — autoscale 0%→75%
- `TestScaleThresholds` — scaleUpThreshold/scaleDownThreshold 0%→100%
- `TestOrderPaper_DelegatesToAdvance` — orderPaper 0%→66.7%

**新增测试（dispatch_test.go）**:
- `TestGazetteDispatch_RoutesAndMarksRead` — gazetteDispatch 0%→69.2%

**验证通过**:
- `golangci-lint run ./internal/whip/...` → 零 error
- `go test -race ./...` → 全部 PASS
- `go test -race -cover ./internal/whip/...` → 62.3%

## 下一步

继续 Phase 2（测试攻坚）：
1. **PR-6**: B-2 Store 并发测试
2. **PR-7**: E-1.3 Store 错误治理
3. **PR-8**: Minister 上下文健康监控

---
