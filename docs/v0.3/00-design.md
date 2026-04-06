# House of Cards v0.3 — 设计定稿

> 状态：**定稿**
>
> 定稿时间：2026-04-05
>
> 前置阅读：
> - `docs/v0.2/v0.2-draft.md`（v0.2 草案）
> - `docs/v0.1/02-design-v3-final.md`（原始设计）
> - `docs/research/2026-04-05-claude-code-source-analysis.md`（Claude Code 源码分析）
>
> 技术方案：
> - [`tech-spec-E1-error-governance.md`](tech-spec-E1-error-governance.md) — 错误治理 + Whip 恢复梯度
> - [`tech-spec-E2-config-validation.md`](tech-spec-E2-config-validation.md) — 配置启动校验
> - [`tech-spec-C1-linter-upgrade.md`](tech-spec-C1-linter-upgrade.md) — Linter 升级
> - [`tech-spec-B1-whip-tests.md`](tech-spec-B1-whip-tests.md) — Whip 测试补强
> - [`tech-spec-context-health.md`](tech-spec-context-health.md) — Minister 上下文健康监控
> - [`tech-spec-A2-autoscale.md`](tech-spec-A2-autoscale.md) — Autoscale 全链路
> - [`tech-spec-C2-structured-logging.md`](tech-spec-C2-structured-logging.md) — 结构化日志统一

---

## 0. v0.2 总结与 v0.3 定位

### v0.2 成果

v0.2 已完成 Phase 1-4，所有核心功能骨架已建立：

| 模块 | 完成状态 |
|------|---------|
| CLI 骨架 | ✅ 完整 |
| Whip 守护进程 | ✅ 完整 |
| DAG 调度引擎 | ✅ 完整 |
| 补选机制 | ✅ 完整 |
| Gazette + ACK | ✅ 完整 |
| Committee 审查 | ✅ 完整 |
| Formula 扩展 | ✅ 完整 |
| OpenTelemetry | ✅ 完整 |
| Doctor 健康检查 | ✅ 增强 |
| Hansard 回放 | ✅ 完成 |
| 版本信息注入 | ✅ 完成 |
| Minister CLAUDE.md | ✅ 完成（`buildMinisterCLAUDE()`） |
| Webhook API | ✅ 完成（HMAC-SHA256 验证） |
| Summon API | ✅ 完成 |
| Graceful Shutdown | ✅ 完成（whip + serve 信号处理） |

**Version: 0.2.0**

### v0.2 遗留问题

| 问题 | 描述 |
|------|------|
| A-2 Autoscale | 只更新 DB 状态，未真正触发 summon 全链路 |
| 错误处理 | 50+ 处 `_ = db.*()` 静默吞没错误 |
| 配置校验 | LoadConfig 不验证字段合法性 |
| 测试覆盖率 | 仅 29.8%（核心路径 whip 12.9%） |
| Linter | 仅 6 个，未含安全检查 |
| OTLP Export | stub，未实现 gRPC/HTTP 导出 |

### v0.3 定位

**主题：从能用到可靠——质量强化版**

v0.3 **不加新的用户功能**，专注四个硬性 Gate：

| Gate | 核心 | 验收标准 |
|------|------|---------|
| **错误处理** | 消除静默错误，关键路径必须处理错误 | 关键路径 `_ =` 清零，辅助路径有 `slog.Warn` |
| **测试** | 关键路径每个分支都必须覆盖 | Whip ≥ 60%，Store ≥ 50%，总计 ≥ 60% |
| **Lint** | Linter 升级 + CI 强制 | `golangci-lint run` 零 error |
| **日志** | 结构化日志统一 | 非 CLI 路径零 `fmt.Printf`，关键操作有日志 |

> **用户交互改进（D-1 交互式创建、D-3 智能过滤）推迟到 v0.4。**

---

## 1. 功能范围

### 方向 A：骨架补完

#### A-2：Autoscale 全链路

> 技术方案：[`tech-spec-A2-autoscale.md`](tech-spec-A2-autoscale.md)

**问题**：`autoscale()` 只更新 minister 状态为 idle，不创建 Chamber、不分配 Bill、不启动 runtime。

**方案**：
1. 提取 summon 核心逻辑为 `internal/minister/summon.go`，CLI 和 autoscale 共用
2. Autoscale scale-up 时调用 `minister.Summon()` 完成全链路
3. 速率限制：每 tick 最多传召 2 个 minister，不超过 `max_ministers` 全局上限
4. 失败回滚：worktree 创建后如果 runtime 启动失败，自动清理

**关键技术决策**：
- 新增 `internal/minister/` 包，含 `Summon()` 和 `BuildMinisterCLAUDE()`
- `shouldScaleUp` / `shouldScaleDown` 提取为纯函数，可独立测试
- `matchBillToMinister` 优先匹配 portfolio skill

**数据库变更**：新增 `UpdateMinisterWorktree()`、`UpdateMinisterPid()`、`UpdateBillBranch()`

---

### 方向 B：测试攻坚

#### B-1：Whip 核心路径测试补强

> 技术方案：[`tech-spec-B1-whip-tests.md`](tech-spec-B1-whip-tests.md)

**目标**：Whip 覆盖率 12.9% → 65%+，新增约 25 个测试

**核心策略**：
1. **AliveChecker 注入**：Whip struct 新增 `aliveChecker func(*store.Minister) bool`，测试时 stub 进程检查
2. **纯函数提取**：`isOverGracePeriod`、`isOverStuckThreshold`、`shouldScaleUp`、`shouldScaleDown`
3. **真实 DB**：沿用 in-memory SQLite，不 mock store
4. **文件系统**：done/review/ack 文件在 `t.TempDir()` 创建

**测试覆盖矩阵**：

| 模块 | 测试类型 | 测试数 |
|------|---------|--------|
| `liveness.go` | threeLineWhip + byElection + 恢复梯度 | 6 |
| `poller.go` | pollDoneFiles + pollReviewFile + pollAckFiles | 8 |
| `scheduler.go` | autoscale + privyAutoMerge + 纯函数 | 6 |
| `dispatch.go` | gazetteDispatch + deliverGazette | 3 |
| `whip.go` | parseDoneFile + tick 路径 | 2 |

#### B-2：Store 并发测试

**目标**：事务边界、并发写入覆盖，覆盖率 ≥ 50%

**策略**：
- goroutine 并发读写测试
- 事务冲突处理测试
- 错误路径覆盖（配合 E-1.3）

#### B-3：CLI 集成测试

**目标**：cmd 层覆盖率达到 40%

**策略**：
- subtest 批量测试参数解析
- 测试错误提示友好性
- 测试 destructive 命令确认流程（配合 D-2）

#### 测试质量规范（强制）

所有 v0.3 新增测试必须遵守：

| 规范 | 要求 |
|------|------|
| 错误路径 | 每个被测函数至少覆盖 1 个错误路径 |
| Table-driven | 同一函数多组输入用 `[]struct{...}` |
| Mock 限制 | 只 mock 外部依赖（AI runtime、网络），禁止 mock store/config |
| 断言 | 禁止 `t.Fatal(err)` 后不验证返回值内容 |
| 命名 | `Test<Function>_<Scenario>` |
| Skip | 零 `t.Skip()` 留桩 |

---

### 方向 C：生产基础设施

#### C-1：Linter 配置升级

> 技术方案：[`tech-spec-C1-linter-upgrade.md`](tech-spec-C1-linter-upgrade.md)

**升级**：6 → 11 个 linter

| 新增 Linter | 作用 |
|------------|------|
| `gosec` | 安全扫描（G104 临时豁免，E-1 完成后移除） |
| `gocritic` | 代码质量（14 项检查） |
| `exhaustive` | switch 枚举完整性 |
| `gochecknoinits` | 禁止 init()（Cobra init 加 nolint） |
| `godot` | 注释规范 |

**关键决策**：
- gosec G104 在 E-1 完成前临时豁免，避免与错误治理重复工作
- Cobra `init()` 使用 `//nolint:gochecknoinits` 注释豁免
- CI 中 lint 作为 blocking check

#### C-2：结构化日志统一

> 技术方案：[`tech-spec-C2-structured-logging.md`](tech-spec-C2-structured-logging.md)

**方案**：
1. 新建 `internal/logger/logger.go`，`Init(level, format string)`
2. Config 新增 `[log]` 段（level + format）
3. CLI 新增 `--log-level` / `--log-format` flag（优先于 config）
4. 替换规则：后台服务全部 slog，CLI 用户输出保留 fmt.Printf

**关键操作必须有日志**：

| 操作 | 级别 | 必须字段 |
|------|------|---------|
| Bill 状态变更 | Info | bill_id, old_status, new_status |
| Minister 传召/解职 | Info | minister_id, bill_id / reason |
| By-election | Warn | minister_id, bill_id |
| Autoscale | Info | direction, pending, idle |
| 恢复梯度 | Warn | minister_id, attempt, action |
| 配置校验失败 | Error | 所有失败字段 |

---

### 方向 D：用户交互改进

#### D-2：Destructive 命令确认

**统一标准**：所有 destructive 命令需要 `--confirm` 或交互确认

| 命令 | 当前 | 改进 |
|------|------|------|
| session dissolve | 无确认 | 强制确认 |
| minister dismiss | 无确认 | 强制确认 |
| bill delete | 缺失 | 添加 |

> **D-1 交互式创建** 和 **D-3 智能过滤** 推迟到 v0.4。

---

### 方向 E：健壮性治理

#### E-1：错误处理治理

> 技术方案：[`tech-spec-E1-error-governance.md`](tech-spec-E1-error-governance.md)

**问题**：50+ 处 `_ = db.*()` 静默吞没错误。

**实测分布**：

| 模块 | `_ =` 数量 | 高严重度 |
|------|-----------|---------|
| `internal/whip/liveness.go` | 6 | 2（UpdateMinisterStatus） |
| `internal/whip/poller.go` | 16 | 4（UpdateBillStatus/MinisterStatus） |
| `internal/whip/scheduler.go` | 14 | 6（UpdateSessionStatus/MinisterStatus） |
| `internal/whip/dispatch.go` | 2 | 0 |
| `cmd/serve.go` | 9 | 3（CreateBill/Decode） |

**分级处理**：

| 级别 | 处理 | 适用 |
|------|------|------|
| **关键路径** | 返回 error / HTTP 500 / 中断 | 状态变更、数据创建 |
| **辅助路径** | `slog.Warn`，不中断 | RecordEvent、Hansard、非核心 Gazette |
| **可忽略** | 保留 `_ =` + `// best-effort:` 注释 | os.Remove、信号文件 |

#### E-1 补充：Whip 自动恢复梯度

> 来源：Claude Code 三层恢复机制

**现状**：stuck → 直接 by-election，代价高昂。

**新方案**：三级恢复梯度

```
stuck detected
  → recovery_attempts=0 : 发 Gazette 提醒 checkpoint（观察）
  → recovery_attempts=1 : 标记 bill at-risk + 发警告 Gazette
  → recovery_attempts≥2 : 触发 by-election + 重置 attempts
```

**数据库变更**：`ALTER TABLE ministers ADD COLUMN recovery_attempts INTEGER DEFAULT 0`

**新增 Store 方法**：`IncrementRecoveryAttempts()`、`ResetRecoveryAttempts()`

**恢复成功**：minister 恢复心跳时自动重置 recovery_attempts

#### E-1 补充：Minister 上下文健康监控

> 来源：Claude Code 自动监控消息列表大小
>
> 技术方案：[`tech-spec-context-health.md`](tech-spec-context-health.md)

**方案**：
1. Gazette payload 中增加 `context_health`（tokens_used/tokens_limit/turns_elapsed）
2. Whip tick 中新增 `checkContextHealth()`
3. 80% → 提醒 Gazette；90% → 紧急 Gazette + 记录 at-risk 事件
4. 去重冷却：同一 minister 5 分钟内不重复告警

**新增文件**：`internal/whip/context_health.go`

**Store 新增**：`GetLatestContextHealth(ministerID)`、`ContextHealth` struct

#### E-2：配置启动校验

> 技术方案：[`tech-spec-E2-config-validation.md`](tech-spec-E2-config-validation.md)

**方案**：
1. `Config.Validate() error` — 聚合所有校验失败为 `ValidationError`
2. `LoadConfig()` 返回前调用 `Validate()`
3. 校验项：Duration 合法性、数值范围、必填字段、Exporter 枚举、Home 可达性、heartbeat/stuck 关系

**校验规则**：10+ 条，Table-driven 测试全覆盖

#### E-3：OTLP Export Stub 标注

**方案**：v0.3 不实现 OTLP export，但需：
1. `hoc doctor` 标注 OTLP 为 stub
2. 配置中加注释说明
3. 选择 `otlp` 时返回明确的 "not yet supported" 错误

---

## 2. Phase 划分与实施顺序

### Phase 1：地基（错误治理 + 配置校验 + Linter）

```
PR-1: C-1 Linter 升级
  → .golangci.yml 重写（6→11 linter）
  → 修复所有 gocritic/godot 告警
  → Cobra init() 加 nolint
  → CI lint job

PR-2: E-1.1 Whip 错误治理
  → store: 新增 recovery_attempts 字段 + 2 方法
  → liveness.go: 关键路径错误处理 + 恢复梯度
  → poller.go: 关键路径错误处理
  → scheduler.go: 关键路径错误处理
  → dispatch.go: best-effort 注释
  → 移除 gosec G104 豁免

PR-3: E-1.2 Serve 错误治理
  → serve.go: webhook CreateBill 返回 500
  → serve.go: enacted handler Decode 返回 400
  → serve.go: 辅助路径改 slog.Warn

PR-4: E-2 配置校验
  → config.go: ValidationError + Validate() + 集成 LoadConfig
  → config_test.go: table-driven 校验测试
```

**Phase 1 Gate 检查**：
- [ ] `golangci-lint run` 零 error
- [ ] `_ = db.*()` 在 whip/serve 关键路径清零
- [ ] `hoc whip start` 对无效配置报错

### Phase 2：测试（Whip + Store + 上下文监控）

```
PR-5: B-1 Whip 测试
  → whip.go: 新增 aliveChecker 字段
  → liveness.go: 使用 w.isAlive()
  → scheduler.go: 提取 shouldScaleUp/shouldScaleDown
  → whip_test.go: ~25 个新测试

PR-6: B-2 Store 测试
  → store_test.go: 并发读写 + 事务冲突 + 错误路径

PR-7: E-1.3 Store 错误治理
  → store.go migrate(): best-effort 注释
  → 审计所有 public 方法错误传播

PR-8: Minister 上下文健康监控
  → store/gazette.go: ContextHealth struct
  → store: GetLatestContextHealth()
  → whip/context_health.go: checkContextHealth()
  → whip.go: tick 中调用
  → ministers.go: CLAUDE.md 新增上报指示
```

**Phase 2 Gate 检查**：
- [ ] Whip 覆盖率 ≥ 60%
- [ ] Store 覆盖率 ≥ 50%
- [ ] 所有测试符合质量规范
- [ ] 零 `t.Skip()`

### Phase 3：补完收尾

```
PR-9: A-2 Autoscale 全链路
  → internal/minister/summon.go: Summon() + BuildMinisterCLAUDE()
  → scheduler.go: autoscale 改为调用 Summon
  → cmd/ministers.go: CLI summon 改为调用 minister.Summon()
  → store: 新增 UpdateMinisterWorktree/Pid, UpdateBillBranch

PR-10: C-2 结构化日志统一
  → internal/logger/logger.go: Init()
  → config.go: LogConfig
  → cmd/root.go: --log-level / --log-format
  → 全局替换非 CLI 路径 fmt.Printf → slog

PR-11: B-3 CLI 集成测试
  → cmd/*_test.go: 参数解析 + 错误提示 + 确认流程

PR-12: D-2 Destructive 命令确认
  → session dissolve / minister dismiss: --confirm
  → bill delete: 新增命令

PR-13: E-3 OTLP stub 标注
  → doctor: OTLP stub 状态
  → config: 注释说明
  → 选择 otlp 时报错
```

**Phase 3 Gate 检查**：
- [ ] 总覆盖率 ≥ 60%
- [ ] `fmt.Printf` 在非 CLI 路径清零
- [ ] 关键操作有结构化日志
- [ ] `hoc doctor` 全部 PASS

---

## 3. 实施清单

| # | Feature | 优先级 | Phase | PR | 技术方案 |
|---|---------|--------|-------|----|---------|
| 1 | C-1 Linter 升级 | P0 | 1 | PR-1 | [C1](tech-spec-C1-linter-upgrade.md) |
| 2 | E-1.1 Whip 错误治理（含恢复梯度） | P0 | 1 | PR-2 | [E1](tech-spec-E1-error-governance.md) |
| 3 | E-1.2 Serve 错误治理 | P0 | 1 | PR-3 | [E1](tech-spec-E1-error-governance.md) |
| 4 | E-2 配置启动校验 | P1 | 1 | PR-4 | [E2](tech-spec-E2-config-validation.md) |
| 5 | B-1 Whip 测试 | P0 | 2 | PR-5 | [B1](tech-spec-B1-whip-tests.md) |
| 6 | B-2 Store 测试 | P1 | 2 | PR-6 | — |
| 7 | E-1.3 Store 错误治理 | P1 | 2 | PR-7 | [E1](tech-spec-E1-error-governance.md) |
| 8 | Minister 上下文健康监控 | P1 | 2 | PR-8 | [context-health](tech-spec-context-health.md) |
| 9 | A-2 Autoscale 全链路 | P0 | 3 | PR-9 | [A2](tech-spec-A2-autoscale.md) |
| 10 | C-2 结构化日志 | P1 | 3 | PR-10 | [C2](tech-spec-C2-structured-logging.md) |
| 11 | B-3 CLI 测试 | P1 | 3 | PR-11 | — |
| 12 | D-2 确认机制 | P1 | 3 | PR-12 | — |
| 13 | E-3 OTLP stub 标注 | P2 | 3 | PR-13 | — |

---

## 4. 关键技术决策汇总

| 决策 | 选择 | 理由 |
|------|------|------|
| 错误分级 | 关键/辅助/可忽略三级 | 避免过度处理或全部忽略 |
| Whip 恢复 | 3 级梯度（gazette → at-risk → by-election） | Claude Code 三层恢复启发，降低 by-election 频率 |
| 上下文监控 | Gazette payload 扩展 context_health | 零 schema 变更，复用现有 payload 字段 |
| Summon 复用 | 新包 `internal/minister/` | CLI 和 autoscale 共用，避免代码重复 |
| 进程检查可测 | `aliveChecker` 函数字段注入 | 测试中 stub tmux/kill 检查 |
| 日志区分 | CLI 输出 fmt.Printf，后台 slog | 用户交互和程序日志职责分离 |
| Linter gosec | G104 临时豁免 | 与 E-1 错误治理解耦，完成后移除 |
| 测试数据库 | 真实 in-memory SQLite，不 mock | CLAUDE.md 规定：禁止 mock 内部模块 |
| 配置校验 | 聚合多错误（ValidationError） | 一次报出所有问题，避免修一个报一个 |
| Autoscale 速率 | 每 tick 最多 2 个 minister | 避免资源竞争，下次 tick 重新评估 |

---

## 5. 数据库变更汇总

```sql
-- E-1.1: Whip 恢复梯度
ALTER TABLE ministers ADD COLUMN recovery_attempts INTEGER DEFAULT 0;
```

新增 Store 方法：

| 方法 | 模块 | 用途 |
|------|------|------|
| `IncrementRecoveryAttempts(ministerID)` | E-1.1 | 恢复计数递增 |
| `ResetRecoveryAttempts(ministerID)` | E-1.1 | 恢复计数重置 |
| `GetLatestContextHealth(ministerID)` | 上下文监控 | 从 Gazette payload 提取 |
| `UpdateMinisterWorktree(id, worktree)` | A-2 | Autoscale 全链路 |
| `UpdateMinisterPid(id, pid)` | A-2 | Autoscale 全链路 |
| `UpdateBillBranch(billID, branch)` | A-2 | Autoscale 全链路 |

---

## 6. 新增文件汇总

| 文件 | 用途 |
|------|------|
| `internal/minister/summon.go` | Summon 全链路逻辑 |
| `internal/minister/claude_md.go` | BuildMinisterCLAUDE 提取 |
| `internal/minister/summon_test.go` | Summon 测试 |
| `internal/whip/context_health.go` | checkContextHealth 实现 |
| `internal/whip/context_health_test.go` | 上下文健康测试 |
| `internal/logger/logger.go` | 统一 Logger 初始化 |
| `internal/config/config_test.go` | 配置校验测试 |

---

## 7. 版本目标

| 指标 | v0.2 | v0.3 目标 |
|------|------|----------|
| Version | 0.2.0 | 0.3.0 |
| 测试覆盖率 | 29.8% | 60%+ |
| Linter | 6 个 | 11+ 个 |
| API stub | 0 个 | 0 个 |
| 静默错误 `_ =` | 50+ 处 | < 5 处（仅 best-effort 注释的可忽略路径） |
| 配置校验 | 无 | 启动时全量校验（10+ 规则） |
| Whip 恢复策略 | 直接 by-election | 3 级梯度恢复 |
| Minister 上下文监控 | 无 | Gazette context_health（80%/90% 阈值） |
| 日志 | 混用 fmt/slog | 后台服务统一 slog，level/format 可配置 |

---

## 8. 版本验收 Checklist

v0.3.0 发布前必须全部通过：

**编译与构建**：
- [ ] `go build ./...` 零 warning
- [ ] `go vet ./...` 零错误

**质量 Gate**：
- [ ] `golangci-lint run` 零 error（gosec G104 豁免已移除）
- [ ] 总测试覆盖率 ≥ 60%
- [ ] Whip 覆盖率 ≥ 60%
- [ ] Store 覆盖率 ≥ 50%
- [ ] 零 `t.Skip()` 留桩
- [ ] 所有新增测试符合测试质量规范

**功能验收**：
- [ ] `_ = db.*()` 在关键路径清零
- [ ] Whip 恢复梯度可演示（3 级恢复）
- [ ] 配置校验：无效 duration/数值/exporter 在启动时报错
- [ ] Autoscale 全链路：scale-up 自动创建 chamber + 启动 runtime
- [ ] `fmt.Printf` 在非 CLI 路径清零
- [ ] `--log-level` / `--log-format` 生效
- [ ] Destructive 命令有确认机制
- [ ] `hoc doctor` 全部 PASS（含 OTLP stub 标注）

**CI/CD**：
- [ ] CI 中 lint 作为 blocking check
- [ ] 所有 Phase 的 PR 已合并且 CI 通过

---

## 9. 设计原则

1. **CLI-first**：所有 Feature 先有 CLI，API 是可选层
2. **可测试优先**：核心逻辑提取为纯函数或可注入依赖
3. **零新依赖**：slog 标准库、golangci-lint 开发依赖
4. **错误不沉默**：关键路径必须处理错误，辅助路径至少记录日志
5. **质量优先于数字**：测试覆盖率是结果不是目标

### 架构验证（来源：Claude Code 源码分析）

以下 HOC 设计经 Claude Code 生产级源码（v2.1.88）验证，方向正确：

| HOC 设计 | Claude Code 对应 | 结论 |
|----------|-----------------|------|
| Gazette 摘要协议 | 子代理只返回摘要给父 Agent | 完全一致 |
| Chamber（git worktree 隔离） | `isolation: 'worktree'` | 完全一致 |
| Minister 禁止递归调用 | 子 Agent 没有 AgentTool | 完全一致 |
| Whip tick 驱动 | Agent Loop `while(true)` | 本质相同 |
| Hansard 审计记录 | JSONL 转录 + 元数据 | 高度对齐 |

当前最大差距在**韧性工程**（恢复策略、上下文监控），已纳入本版本。

---

## 10. 与 v0.1 设计的对账

以下 v0.1 设计中的功能不在 v0.3 范围内：

| 功能 | 说明 | 计划 |
|------|------|------|
| Mesh Topology | Schema 支持但未实现 | v1.0+ |
| Formula 版本管理 | Formula 存在但无版本控制 | v0.4 评估 |
| Minister Runtime 终止 | shutdown 不通知活跃 tmux session | A-2 附带处理 |
| Multi-Speaker 竞争 | speaker council 是实验 stub | v1.0+ |

### 研究报告启发项（v0.4+ 规划）

以下来自 Claude Code 源码分析，已评估但不纳入 v0.3：

| 启发项 | 描述 | 建议版本 |
|--------|------|---------|
| Bill 生命周期 Hook | 状态变更触发用户自定义 shell 脚本 | v0.4 |
| Minister 权限按类型限制 | 按 Bill 类型限制 Minister 工具集 | v0.4 |
| 分层配置模型 | 组织策略 > 项目配置 > Minister 级覆盖 | v0.5+ |
| 统一 Tool/Action 接口 | Minister 多工具支持时定义统一接口 | v0.5+ |

---

*v0.3 设计定稿。2026-04-05。*
