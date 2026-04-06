# 技术方案：C-1 Linter 配置升级

> Phase 1 | 优先级：P0

---

## 1. 现状分析

### 1.1 当前 `.golangci.yml`

```yaml
linters:
  enable:
    - govet
    - errcheck
    - staticcheck
    - unused
    - gosimple
    - ineffassign
```

6 个 linter，无安全扫描、无代码质量检查、无 switch exhaustive 检查。

### 1.2 errcheck 豁免

```yaml
linters-settings:
  errcheck:
    exclude-functions:
      - fmt.Fprintf
      - fmt.Fprintln
      - fmt.Printf
      - fmt.Println
```

**问题**：这些豁免在 E-1 错误治理和 C-2 日志统一后应缩减（`fmt.Printf` 会被 `slog.*` 替代）。但在 Phase 1 阶段保留，避免与 E-1 冲突。

---

## 2. 升级方案

### 2.1 新增 Linter

| Linter | 作用 | 启用理由 |
|--------|------|---------|
| `gosec` | 安全扫描（G101-G601） | 检测硬编码密钥、SQL 注入、文件权限等 |
| `gocritic` | 代码质量（100+ 检查器） | 检测代码异味、性能问题、风格问题 |
| `exhaustive` | switch/enum 完整性 | 确保 status 枚举（draft/reading/enacted/...）全覆盖 |
| `gochecknoinits` | 禁止 init() 函数 | 强制显式初始化，避免隐式副作用 |
| `godot` | 注释以句号结尾 | 统一注释风格 |

升级后总计 **11 个** linter。

### 2.2 新 `.golangci.yml`

```yaml
# golangci-lint configuration — v0.3 quality gate
run:
  timeout: 3m

linters:
  enable:
    # --- 基础（v0.2 已有）---
    - govet
    - errcheck
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    # --- 新增（v0.3）---
    - gosec          # 安全扫描
    - gocritic       # 代码质量
    - exhaustive     # switch 枚举完整性
    - gochecknoinits # 禁止 init()
    - godot          # 注释规范

linters-settings:
  errcheck:
    exclude-functions:
      - fmt.Fprintf
      - fmt.Fprintln
      - fmt.Printf
      - fmt.Println

  gosec:
    excludes:
      # G104: 审计代码中大量 `_ = db.*()` 在 E-1 治理前暂时豁免
      # E-1 完成后移除此豁免
      - G104
    severity: high
    confidence: medium

  gocritic:
    enabled-checks:
      - appendAssign
      - argOrder
      - badCall
      - badLock
      - dupArg
      - dupBranchBody
      - dupCase
      - dupSubExpr
      - flagDeref
      - nilValReturn
      - sloppyLen
      - typeSwitchVar
      - underef
      - unlambda

  exhaustive:
    default-signifies-exhaustive: true

  godot:
    scope: toplevel
    period: true

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
  exclude-rules:
    # Test files can use init().
    - path: _test\.go
      linters:
        - gochecknoinits
    # Test files don't need exhaustive switch.
    - path: _test\.go
      linters:
        - exhaustive
```

### 2.3 gosec G104 豁免策略

**临时豁免**：`gosec` 的 G104（Errors unhandled）与 E-1 错误治理重叠。在 Phase 1 中：

1. 先启用 gosec 但豁免 G104
2. E-1.1/E-1.2 完成后，在 E-1 的 PR 中同步移除 G104 豁免
3. E-1.3 完成后，确认 G104 零告警

---

## 3. 预期影响与修复

### 3.1 预估新增告警

先运行一次检查估算工作量：

```bash
golangci-lint run --new-from-rev=HEAD~1  # 仅检查增量
golangci-lint run                         # 全量检查
```

预估按模块分布：

| 模块 | gocritic | exhaustive | godot | gosec | gochecknoinits |
|------|----------|-----------|-------|-------|----------------|
| `internal/whip/` | 3-5 | 2-3 | 5-10 | 0（G104 豁免） | 0 |
| `internal/store/` | 2-3 | 0 | 5-10 | 0（G104 豁免） | 0 |
| `cmd/` | 5-8 | 3-5 | 10-15 | 0 | 1-2（cobra init） |
| `internal/config/` | 0-1 | 0 | 2-3 | 0 | 0 |

### 3.2 gochecknoinits 与 Cobra

Cobra 的 `cmd/*.go` 使用 `init()` 注册子命令。处理方式：

```go
// 方案 A：nolint 注释（推荐，改动最小）
//nolint:gochecknoinits // Cobra convention: register subcommands in init()
func init() {
    rootCmd.AddCommand(serveCmd)
}

// 方案 B：在 _test.go 排除规则中也排除 cmd/ 目录
// 不推荐——cmd 内其他 init() 也会被豁免
```

采用**方案 A**。

### 3.3 exhaustive 与 status switch

`status` 字段是 string 而非 enum，`exhaustive` 不会触发。但未来如果将 status 改为 `type BillStatus string` const 枚举，`exhaustive` 将自动生效。当前 Phase 1 主要是基础设施建设。

---

## 4. CI 集成

### 4.1 Makefile target

```makefile
.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: lint-fix
lint-fix:
	golangci-lint run --fix ./...
```

### 4.2 CI workflow

```yaml
# .github/workflows/ci.yml
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: golangci/golangci-lint-action@v6
        with:
          version: v1.62
          args: --timeout=3m
```

---

## 5. 实施步骤

```
1. 更新 .golangci.yml（新配置）
2. 运行 golangci-lint run，记录所有告警
3. 修复 gocritic 告警（代码质量）
4. 修复 godot 告警（注释规范）
5. 对 Cobra init() 添加 nolint 注释
6. 确认 golangci-lint run 零 error
7. 更新 CI workflow（如需要）
8. 更新 Makefile
```

---

## 6. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `.golangci.yml` | 完全重写 |
| `cmd/*.go` | Cobra init() 添加 `nolint:gochecknoinits` |
| `internal/**/*.go` | 修复 gocritic / godot 告警 |
| `Makefile` | 新增 lint / lint-fix target |
| `.github/workflows/ci.yml` | 新增 lint job（如尚未有） |
