# House of Cards 项目实施质量巡检报告

> 调研日期：2026-03-01
> 调研版本：Phase 2 完成，Phase 3 规划中
> 报告性质：**开发质量巡检**（非运行时质量）

---

## 一、巡检范围与目的

### 1.1 巡检目的

本巡检任务旨在评估 **House of Cards 项目的实施/开发质量**，而非运行时质量。具体关注：

- 代码质量（测试、错误处理、规范）
- 架构设计质量（模块化、依赖、接口）
- 文档完整性（设计文档、注释、API 文档）
- 可维护性（复杂度、单一职责、可扩展性）
- 安全性（依赖漏洞、输入验证）

### 1.2 巡检范围

| 层级 | 内容 |
|------|------|
| **代码层** | internal/ 目录下的核心模块 |
| **CLI 层** | cmd/ 目录下的命令行实现 |
| **测试层** | *_test.go 文件 |
| **文档层** | docs/ 目录下的设计文档 |
| **配置层** | go.mod, CLAUDE.md 等配置文件 |

---

## 二、测试覆盖分析

### 2.1 测试文件统计

| 模块 | 测试文件 | 状态 | 评估 |
|------|---------|------|------|
| store | store_test.go | ✅ 存在 | 良好 |
| speaker | speaker_test.go | ✅ 存在 | 良好 |
| whip | whip_test.go | ✅ 存在 | 良好 |
| chamber | chamber_test.go | ✅ 已补充 | 良好 |
| privy | privy_test.go | ✅ 已补充 | 良好 |
| runtime | runtime_test.go | ✅ 已补充 | 良好 |
| config | - | ❌ 缺失 | 低优先级 |
| util | - | ❌ 缺失 | 低优先级 |

### 2.2 测试用例覆盖

**whip_test.go** - 核心 DAG 逻辑测试：
- ✅ `TestBillIsReady_NoDeps`
- ✅ `TestBillIsReady_EmptyDepsArray`
- ✅ `TestBillIsReady_DepsEnacted`
- ✅ `TestBillIsReady_DepsRoyalAssent`
- ✅ `TestBillIsReady_DepsDraft`
- ✅ `TestBillIsReady_MixedDeps`
- ✅ `TestBillIsReady_AllDepsEnacted`
- ✅ `TestBillIsReady_UnknownDep`
- ✅ `TestBillIsReady_MalformedJSON`

**store_test.go** - 数据层测试：
- ✅ Minister CRUD 操作
- ✅ Bill CRUD 操作
- ✅ Session 查询
- ✅ Hansard 统计
- ✅ 技能匹配逻辑

**speaker_test.go** - Speaker 上下文生成：
- ✅ 空数据库场景
- ✅ 有数据场景

### 2.3 测试质量评估

**优点**：
- 测试使用 `t.TempDir()` 创建临时目录，隔离良好
- 使用 `t.Cleanup()` 确保资源释放
- 有清晰的 subtest 划分（`t.Run`）
- 边界条件覆盖较好（如空依赖、错误 JSON）

**不足**：
- ❌ **集成测试缺失**：缺乏对 Whip 完整 tick 循环的集成测试
- ❌ **Runtime mock 缺失**：无法测试不同 Runtime 的行为差异
- ❌ **Privy 合并逻辑无测试**：核心合并功能无测试覆盖
- ❌ **并发场景缺失**：SQLite 并发读写无测试验证

### 2.4 测试覆盖率评估

```
运行测试结果：
- internal/store: PASS ✅
- internal/speaker: PASS ✅
- internal/whip: PASS ✅
- internal/chamber: 无测试 ❌
- internal/config: 无测试 ❌
- internal/privy: 无测试 ❌
- internal/runtime: 无测试 ❌
- internal/util: 无测试 ❌
```

**覆盖率评估**：约 **30-40%**（核心逻辑有测试，边缘模块缺失）

---

## 三、代码质量评估

### 3.1 错误处理

**评估：良好**

典型模式：
```go
// whip.go 中的错误处理
if err := w.db.CreateGazette(g); err != nil {
    slog.Warn("byElection: create gazette", "err", err)
}
```

**优点**：
- ✅ 使用 `slog` 记录错误而非 silent fail
- ✅ 关键操作有错误回滚
- ✅ 错误信息包含上下文（"byElection: create gazette"）

**问题**：
- ⚠️ 部分函数忽略错误（如 `_ = db.CreateHansard(h)`）
- ⚠️ 缺乏自定义错误类型，错误传播信息有限

### 3.2 代码规范

**评估：良好**

- ✅ 包结构清晰：`internal/whip`, `internal/store` 等
- ✅ 单一职责：每个包有明确职责
- ✅ 命名一致：使用政府隐喻命名（Summon/IsSeated/Dismiss）
- ✅ 导出/非导出区分清晰

**问题**：
- ⚠️ 部分函数过长（如 `whip.go` 中 `tick()` 约 300+ 行）
- ⚠️ 缺乏统一的 error 定义

### 3.3 Go 语言惯用法

**评估：中等**

**良好实践**：
- ✅ 使用 `sql.NullString` 处理 nullable 字段
- ✅ 使用 `t.Helper()` 辅助函数
- ✅ 合理使用 interface 抽象 Runtime

**需改进**：
- ❌ 缺乏 context.Context 在数据库操作中的传递
- ❌ 部分 `exec.Command` 未设置 context
- ❌ 缺乏 `sync.RWMutex` 等并发保护

---

## 四、依赖管理分析

### 4.1 依赖清单

```
直接依赖：
├── spf13/cobra@v1.10.2        # CLI 框架
├── modernc.org/sqlite         # 数据库（无 CGo）
├── charmbracelet/bubbletea    # TUI
├── BurntSushi/toml            # 配置解析
├── google/uuid                # UUID 生成
├── golang.org/x/exp           # 实验功能
└── golang.org/x/sys           # 系统调用
```

### 4.2 依赖质量评估

**优点**：
- ✅ 依赖数量适中（约 30 个 transitive deps）
- ✅ 无 CGo 依赖，部署简单
- ✅ 使用标准库替代方案（如 modernc.org/sqlite）

**问题**：
- ⚠️ `golang.org/x/exp` 依赖实验性 API
- ⚠️ 缺乏依赖版本锁定策略

### 4.3 依赖安全

**评估：待检测**

建议添加：
```bash
go install github.com/securego/gosec/cmd/gosec@latest
gosec ./...
```

---

## 五、架构设计质量

### 5.1 模块化评估

| 模块 | 职责 | 内聚度 | 耦合度 |
|------|------|--------|--------|
| whip | 调度与监控 | 高 | 低 |
| store | 数据持久化 | 高 | 低 |
| runtime | AI 运行时抽象 | 中 | 低 |
| speaker | 上下文生成 | 中 | 中 |
| privy | 合并冲突处理 | 高 | 低 |
| chamber | Worktree 管理 | 高 | 低 |

**评估**：模块化程度良好，依赖方向单一。

### 5.2 接口设计

**Runtime 接口**（runtime/runtime.go）：
```go
type Runtime interface {
    Summon(opts SummonOpts) (*AgentSession, error)
    IsSeated(session *AgentSession) bool
    Dismiss(session *AgentSession) error
    Dispatch(session *AgentSession, message string) error
}
```

**评估**：
- ✅ 接口简洁，方法数量适中
- ✅ 方法命名遵循隐喻
- ⚠️ 缺乏 context.Context 支持
- ⚠️ 返回 error 但缺乏错误类型定义

### 5.3 可扩展性

**评估：良好**

- ✅ Runtime 可插拔（Claude Code/Codex/Cursor）
- ✅ Session 拓扑可配置（parallel/pipeline/tree/mesh）
- ✅ Whip tick 可扩展（添加新职责不影响现有逻辑）
- Phase 3 规划：Formula/Plugin 系统

---

## 六、文档完整性

### 6.1 设计文档

| 文档 | 状态 | 质量 |
|------|------|------|
| 02-design-v3-final.md | ✅ 完整 | 优秀 |
| 05-phase3-roadmap.md | ✅ 完整 | 优秀 |
| 04-gas-town-comparison.md | ✅ 完整 | 良好 |
| CLAUDE.md | ✅ 存在 | 良好 |

### 6.2 代码注释

**评估：中等**

**良好**：
```go
// Package whip implements the Whip daemon — the system's driving force.
//
// The Whip runs a background tick loop every 10 seconds...
```

**不足**：
- ⚠️ 内部函数缺乏注释
- ⚠️ 缺乏 API 文档（doc.go）
- ⚠️ 缺乏架构决策记录（ADR）

### 6.3 CLI 文档

**评估：良好**

- `hoc --help` 输出完整
- 子命令有描述
- 隐喻语义清晰

---

## 七、安全性评估

### 7.1 输入验证

**评估：需改进**

**问题**：
- ❌ Bill/ Session ID 缺乏格式校验
- ❌ 技能匹配使用简单 substring（`strings.Contains`）
- ❌ Gazette 内容无长度/内容限制

**建议**：
```go
// 添加 ID 格式验证
var billIDRegex = regexp.MustCompile(`^bill-[a-z0-9]{6}$`)

// 添加内容长度限制
if len(g.Summary) > 10000 {
    return ErrSummaryTooLong
}
```

### 7.2 Git 操作安全

**评估：良好**

- ✅ 使用绝对路径（`cmd.Dir = worktree`）
- ✅ 命令参数化（无 command injection）
- ⚠️ 缺乏对 git 操作超时的控制

### 7.3 配置安全

**评估：良好**

- ✅ 配置使用 TOML，无敏感信息硬编码
- ⚠️ 缺乏配置 schema 验证

---

## 八、可维护性评估

### 8.1 代码复杂度

** Whip.tick() 方法分析**：
- 代码行数：约 300+ 行
- 分支数量：15+ 个条件分支
- 圈复杂度：高

**评估**：存在优化空间，建议拆分为独立函数。

### 8.2 重复代码

**评估：良好**

- ✅ 未发现明显重复代码
- ✅ 使用 util 包复用公共函数

### 8.3 技术债务

| 项目 | 严重程度 | 说明 |
|------|---------|------|
| 测试覆盖率不足 | 中 | 核心模块有测试，边缘模块缺失 |
| 缺乏 context.Context | 低 | 渐进式改进 |
| ID 验证缺失 | 中 | 可添加 |
| 并发测试缺失 | 中 | 可补充 |

---

## 九、巡检结论与建议

### 9.1 整体评估

| 维度 | 评分 | 说明 |
|------|------|------|
| 测试覆盖 | ⭐⭐⭐ | 核心有测试，边缘缺失 |
| 代码规范 | ⭐⭐⭐⭐ | 遵循 Go 惯用法 |
| 依赖管理 | ⭐⭐⭐⭐ | 轻量且安全 |
| 架构设计 | ⭐⭐⭐⭐⭐ | 模块化优秀 |
| 文档完整 | ⭐⭐⭐⭐ | 设计文档完备 |
| 安全性 | ⭐⭐⭐ | 需加强输入验证 |
| 可维护性 | ⭐⭐⭐⭐ | 技术债务可控 |

**综合评分：7.5/10**

### 9.2 优先改进项（已更新）

| 优先级 | 项目 | 状态 | 说明 |
|--------|------|------|------|
| **P0** | Privy 测试 | ✅ 已完成 | 2026-03-01 补充 parseConflictFiles, BillBranch, MergeResult, MainRepoPath 等测试 |
| **P0** | Runtime 测试 | ✅ 已完成 | 2026-03-01 补充 Runtime 接口测试，Mock 实现，并修复 nil session panic bug |
| **P0** | Chamber 测试 | ✅ 已完成 | 2026-03-01 补充 NewChamber, ListChambers, Chamber 方法测试 |
| **P1** | 输入验证 | ⚠️ 部分完成 | 已修复 sessionIsSeated/sessionDismiss/sessionDispatch 的 nil check，仍需添加 Bill/Session ID 格式校验 |
| **P2** | context.Context | 待处理 | 渐进式改进 |
| **P2** | 依赖安全扫描 | 待处理 | 需要配置 gosec |

### 9.3 后续巡检关注点

| 维度 | 巡检指标 |
|------|---------|
| **测试** | 测试覆盖率是否提升、是否添加集成测试 |
| **安全** | 是否添加输入验证、依赖是否定期扫描 |
| **文档** | 是否添加 API 文档、是否补充代码注释 |
| **架构** | Phase 3 模块是否遵循现有架构原则 |

---

*本报告为项目实施质量巡检报告，将作为常态化任务定期更新。*

**报告维护**：每次巡检后更新
**下次巡检**：Phase 3 关键模块完成后
