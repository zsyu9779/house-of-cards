# House of Cards 项目实施质量巡检报告

> 巡检日期：2026-03-01
> 巡检版本：Phase 5 完成后首次例行巡检
> 巡检员：Claude Code

---

## 一、测试文件统计

| 模块 | 测试文件 | 状态 |
|------|---------|------|
| chamber | internal/chamber/chamber_test.go | ✅ |
| formula | internal/formula/formula_test.go | ✅ |
| otel | internal/otel/otel_test.go | ✅ |
| privy | internal/privy/privy_test.go | ✅（覆盖率低）|
| runtime | internal/runtime/runtime_test.go | ✅（覆盖率低）|
| speaker | internal/speaker/speaker_test.go | ✅ |
| store | internal/store/store_test.go | ✅ |
| util | internal/util/complexity_test.go | ✅ |
| whip | internal/whip/whip_test.go | ✅（覆盖率极低）|
| config | — | ❌ 无测试文件 |
| cmd (E2E) | cmd/integration_test.go | ✅ |

---

## 二、测试运行结果

```
go test ./...
ok  github.com/house-of-cards/hoc/cmd
ok  github.com/house-of-cards/hoc/internal/chamber
ok  github.com/house-of-cards/hoc/internal/formula
ok  github.com/house-of-cards/hoc/internal/otel
ok  github.com/house-of-cards/hoc/internal/privy
ok  github.com/house-of-cards/hoc/internal/runtime
ok  github.com/house-of-cards/hoc/internal/speaker
ok  github.com/house-of-cards/hoc/internal/store
ok  github.com/house-of-cards/hoc/internal/util
ok  github.com/house-of-cards/hoc/internal/whip
```

全部 PASS ✅

---

## 三、测试覆盖率分析

| 模块 | 覆盖率 | 评级 | 核心未覆盖函数 |
|------|--------|------|--------------|
| otel | 78.9% | ⭐⭐⭐⭐⭐ | — |
| formula | 56.0% | ⭐⭐⭐⭐ | — |
| speaker | 54.0% | ⭐⭐⭐⭐ | — |
| store | 46.2% | ⭐⭐⭐ | 部分 CRUD 路径 |
| chamber | 28.1% | ⭐⭐ | CreateWorktree, RemoveWorktree |
| util | 22.1% | ⭐⭐ | 边缘复杂度场景 |
| privy | 12.4% | ⭐ | MergeSession, tryMergeWithStrategyChain, analyzeConflicts, detectConflictType, FormatConflictGazette |
| runtime | 6.3% | ⭐ | 全部实现类 (claudecode, codex, cursor, factory) |
| whip | 2.6% | ⭐ | New, Run, tick, threeLineWhip, byElection, orderPaper, advanceSession, autoAssign, pollDoneFiles, autoscale 等 |
| config | 0.0% | ❌ | 全部 |
| **总计** | **31.6%** | ⭐⭐ | — |

---

## 四、代码规范分析

### 4.1 gofmt 格式化（巡检前状态）

**发现 16 个文件未通过 gofmt 检查**（P0，已修复）：

```
cmd/bill.go
cmd/config_cmd.go
cmd/floor.go
cmd/serve.go
cmd/session.go
cmd/speaker.go
internal/chamber/chamber.go
internal/chamber/chamber_test.go
internal/config/config.go
internal/otel/tracer.go
internal/privy/privy.go
internal/privy/privy_test.go
internal/runtime/runtime_test.go
internal/store/store.go
internal/store/store_test.go
internal/whip/whip.go
```

**修复操作**：`gofmt -w <files>` — 全部格式化完成 ✅

### 4.2 go vet 静态分析

巡检前后均无问题 ✅

### 4.3 依赖管理

`go mod tidy` 无输出，依赖状态整洁 ✅

---

## 五、代码质量评估

### 5.1 whip 模块（党鞭）

- **总代码量**：1114 行，是最大单文件
- **覆盖率**：2.6%（仅 `billIsReady` 100%）
- **问题**：`tick()`、`threeLineWhip()`、`byElection()`、`autoAssign()`、`autoscale()` 等核心调度函数完全无测试
- **根因**：这些函数强依赖真实 DB、真实 git worktree、真实 os/exec，单元测试需要 mock 或集成环境
- **风险**：中等。补选（by-election）逻辑、自动扩缩（autoscale）、委员会自动化（committeeAutomation）均无验证

### 5.2 privy 模块（枢密院）

- **覆盖率**：12.4%（核心合并函数全部 0%）
- **问题**：`MergeSession`、`tryMergeWithStrategyChain`、`analyzeConflicts` 完全无测试
- **根因**：依赖真实 git 仓库操作
- **风险**：较高。合并冲突解决是 Phase 3A 的核心功能，无测试意味着回归风险高

### 5.3 runtime 模块（AI 运行时抽象）

- **覆盖率**：6.3%（只有 sessionIsSeated 55%、sessionDispatch 37%）
- **问题**：ClaudeCode、Codex、Cursor 三个实现类全部 0%
- **根因**：需要真实 AI 进程运行
- **建议**：集成测试或 smoke test，检验 CLI 参数拼接正确性

### 5.4 store 模块（存储层）

- **覆盖率**：46.2%，中等
- **亮点**：核心 CRUD、Hansard、SessionStats 均有测试
- **缺口**：`UpdateMinisterHeartbeat`、`ListWorkingMinisters`、`ListMinistersWithStatus` 等被 whip 大量调用的函数

---

## 六、安全性评估

### 6.1 SQL 注入

所有 SQL 使用参数化查询（`?` placeholder），无拼接风险 ✅

### 6.2 命令注入

`os/exec` 调用均使用参数数组形式（非 shell string），无注入风险 ✅

### 6.3 文件路径

privy 和 chamber 中路径拼接使用 `filepath.Join`，无路径穿越风险 ✅

---

## 七、巡检结论

### 7.1 评分汇总

| 维度 | 评分 | 说明 |
|------|------|------|
| 测试覆盖 | ⭐⭐ | 31.6%，whip/runtime/privy 严重不足 |
| 代码规范 | ⭐⭐⭐⭐ | gofmt 已修复，vet 干净 |
| 安全性 | ⭐⭐⭐⭐⭐ | 参数化 SQL，exec 数组调用 |
| 依赖管理 | ⭐⭐⭐⭐⭐ | mod tidy 无问题 |
| 架构一致性 | ⭐⭐⭐⭐ | 政府隐喻术语使用规范 |

### 7.2 本次发现的问题

| 优先级 | 问题 | 状态 |
|--------|------|------|
| P0 | 16 个 Go 文件 gofmt 不合规 | ✅ 已修复 |
| P1 | whip 核心调度函数覆盖率 2.6% | 待处理 |
| P1 | privy 合并策略链覆盖率 0% | 待处理 |
| P1 | runtime 实现类覆盖率 0% | 待处理 |
| P2 | config 模块无测试文件 | 待处理 |
| P2 | store.ListWorkingMinisters 等被 whip 依赖的函数无测试 | 待处理 |

### 7.3 改进建议

1. **为 whip 添加可测试的纯函数提取**：将 `orderPaper` 中的 DAG 调度决策逻辑提取为无副作用的纯函数，易于单元测试
2. **为 privy 增加 git 集成测试**：使用 `t.TempDir()` 创建真实临时 git 仓库，覆盖 MergeSession 的成功/冲突路径
3. **为 store 补充 whip 相关方法测试**：`ListWorkingMinisters`、`UpdateMinisterHeartbeat`、`ListMinistersWithStatus`
4. **添加 config 包基础测试**：验证默认配置生成和 TOML 解析
5. **CI 中加入 gofmt 检查**：防止格式问题再次积累（`gofmt -l . | grep -q . && exit 1`）
