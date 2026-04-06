# 技术方案：C-2 结构化日���统一

> Phase 3 | 优先级：P1

---

## 1. 问题分析

### 1.1 当前日志状态

| 模块 | 日志方式 | 问题 |
|------|---------|------|
| `internal/whip/` | `slog.Info/Warn/Debug` | ✅ 已结构化 |
| `cmd/serve.go` | `slog.Error` + `fmt.Printf` | 混用 |
| `cmd/ministers.go` | `fmt.Printf` 为主 | 无结构化 |
| `cmd/bill.go` | `fmt.Printf` 为主 | 无结构化 |
| `cmd/session.go` | `fmt.Printf` 为主 | 无结构化 |
| `cmd/root.go` | `fmt.Printf` | 无结构化 |
| `internal/config/` | `fmt.Fprintf(os.Stderr, ...)` | 无结构化 |

### 1.2 核心问题

1. **不一致**：whip 用 slog，其余用 fmt.Printf，日志无法统一收集/过滤
2. **无 level 控制**：fmt.Printf 无法按 DEBUG/INFO/WARN 过滤
3. **无结构化字段**：printf 输出无法机器解析
4. **CLI 输出混淆**：用户界面输出（表格、✓/✗ 标记）和程序日志混在一起

### 1.3 关键区分：CLI 输出 vs 日志

| 类型 | 目的 | 目标 | 工具 |
|------|------|------|------|
| **CLI 输出** | 用户交互 | 终端用户 | `fmt.Printf` / `fmt.Fprintf(os.Stdout, ...)` |
| **程序日志** | 运行状态 | 运维/调试 | `slog.*` |

**原则**：CLI 命令的用户输出（表格、状态标记）保留 `fmt.Printf`。后台守护进程（whip、serve）和内部逻辑全部用 `slog`。

---

## 2. 方案设计

### 2.1 日志配置

在 `internal/config/config.go` 的 Config 中增加日志配置：

```go
type Config struct {
    // ...existing...
    Log LogConfig `toml:"log"`
}

type LogConfig struct {
    Level  string `toml:"level"`  // debug, info, warn, error
    Format string `toml:"format"` // text, json
}
```

DefaultConfig 中：

```go
Log: LogConfig{
    Level:  "info",
    Format: "text",
},
```

### 2.2 Logger 初始化

在 `cmd/root.go` 中统一初始化：

```go
// internal/logger/logger.go
package logger

import (
    "log/slog"
    "os"
    "strings"
)

// Init configures the global slog logger based on level and format.
func Init(level, format string) {
    var lvl slog.Level
    switch strings.ToLower(level) {
    case "debug":
        lvl = slog.LevelDebug
    case "warn":
        lvl = slog.LevelWarn
    case "error":
        lvl = slog.LevelError
    default:
        lvl = slog.LevelInfo
    }

    opts := &slog.HandlerOptions{Level: lvl}

    var handler slog.Handler
    switch strings.ToLower(format) {
    case "json":
        handler = slog.NewJSONHandler(os.Stderr, opts)
    default:
        handler = slog.NewTextHandler(os.Stderr, opts)
    }

    slog.SetDefault(slog.New(handler))
}
```

`cmd/root.go` 中调用：

```go
var (
    logLevel  string
    logFormat string
)

func init() {
    rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "日志级别 (debug/info/warn/error)")
    rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "日志格式 (text/json)")
}

// PersistentPreRunE 中初始化 logger
rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
    // CLI flag 优先于 config
    level := logLevel
    format := logFormat
    if level == "" {
        level = cfg.Log.Level
    }
    if format == "" {
        format = cfg.Log.Format
    }
    logger.Init(level, format)
    return nil
}
```

### 2.3 替换规则

#### 规则 1：后台服务（whip/serve）— 全部替换

```go
// Before
fmt.Printf("Processing bill %s\n", billID)

// After
slog.Info("processing bill", "bill_id", billID)
```

#### 规则 2：内部逻辑（store/config）— 替换错误日志

```go
// Before (config.go)
fmt.Fprintf(os.Stderr, "config watcher error: %v\n", err)

// After
slog.Error("config watcher error", "err", err)
```

#### 规则 3：CLI 用户输出 — 保留 fmt

```go
// 保留：这是用户交互输出
fmt.Printf("🏛  启动 House of Cards API Server\n")
fmt.Printf("   地址: http://localhost%s\n", addr)
fmt.Printf("✓ 议案已创建: %s\n", billID)

// 但守护进程启动信息用 slog
slog.Info("API server starting", "addr", addr)
```

### 2.4 替换清单

#### cmd/serve.go

| 行 | 当前 | 改为 | 类型 |
|----|------|------|------|
| 57 | `fmt.Printf("🏛  启动...")` | 保留（CLI 输出） | CLI |
| 58 | `fmt.Printf("   地址: ...")` | 保留（CLI 输出） | CLI |
| 59 | `fmt.Printf("   按 Ctrl+C...")` | 保留（CLI 输出） | CLI |
| 84 | `fmt.Println("\n⏹  关闭...")` | 保留（CLI 输出） | CLI |
| 91 | `fmt.Println("✓ 服务器已关闭")` | 保留（CLI 输出） | CLI |

serve.go 的日志已经是 `slog.Error`，主要需要处理 E-1.2 中新增的 `slog.Warn` 行。

#### cmd/ministers.go（部分示例）

| 类型 | 行为 |
|------|------|
| `fmt.Printf("✓ 部长已任命: %s\n", ...)` | 保留（CLI 输出） |
| `fmt.Printf("Summoning minister %s...\n", ...)` | 替换为 `slog.Info` |
| `fmt.Fprintf(os.Stderr, "Error: %v\n", ...)` | 替换为 `return fmt.Errorf(...)` |

#### internal/config/config.go

| 行 | 当前 | 改为 |
|----|------|------|
| 129 | `fmt.Fprintf(os.Stderr, "config watcher error: %v\n", err)` | `slog.Error("config watcher error", "err", err)` |
| 138 | `fmt.Fprintf(os.Stderr, "failed to reload config: %v\n", err)` | `slog.Error("config reload failed", "err", err)` |

### 2.5 关键操作必须有日志

以下操作必须有 `slog.Info` 或更高级别日志：

| 操作 | 日志级别 | 必须包含字段 |
|------|---------|-------------|
| Bill 状态变更 | Info | `bill_id`, `old_status`, `new_status` |
| Minister 传召 | Info | `minister_id`, `bill_id`, `worktree` |
| Minister 解职 | Info | `minister_id`, `reason` |
| By-election 触发 | Warn | `minister_id`, `bill_id` |
| Autoscale 触发 | Info | `direction`, `pending`, `idle` |
| 恢复梯度执行 | Warn | `minister_id`, `attempt`, `action` |
| 配置校验失败 | Error | 所有失败字段 |
| API 请求 | Debug | `method`, `path`, `status_code` |

---

## 3. config.toml 示例

```toml
[log]
level = "info"    # debug | info | warn | error
format = "text"   # text | json

# 生产环境推荐
# level = "warn"
# format = "json"
```

---

## 4. 测试计划

| 测试 | 验证点 |
|------|--------|
| `TestLoggerInit_DefaultLevel` | 默认 info 级别 |
| `TestLoggerInit_DebugLevel` | debug 模式输出 debug 日志 |
| `TestLoggerInit_JSONFormat` | json 格式输出合法 JSON |
| `TestLoggerInit_CLIFlagOverridesConfig` | --log-level 优先于 config.toml |

日志替换本身不需要测试（是输出变更），但需要确认：
- `golangci-lint run` 通过
- 无 `fmt.Printf` 遗留在非 CLI 路径中

### 4.1 验证脚本

```bash
# 检查非 CLI 输出路径中的 fmt.Printf 遗留
grep -rn 'fmt\.Printf\|fmt\.Println' internal/ --include='*.go' | grep -v '_test.go'
# 预期：0 行
```

---

## 5. 实施步骤

```
1. 新建 internal/logger/logger.go（Init 函数）
2. config.go 新增 LogConfig
3. cmd/root.go 集成 logger 初始化 + CLI flags
4. internal/config/config.go 替换 fmt.Fprintf → slog
5. cmd/serve.go 确认日志已统一
6. cmd/ministers.go 替换（区分 CLI 输出 vs 日志）
7. cmd/bill.go / session.go / gazette.go / hansard.go 替换
8. 运行验证脚本确认无遗留
9. 运行 golangci-lint
```

---

## 6. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `internal/logger/logger.go` | **新文件** |
| `internal/config/config.go` | 新增 LogConfig + fmt.Fprintf → slog |
| `cmd/root.go` | 新增 --log-level / --log-format + logger 初始化 |
| `cmd/serve.go` | 确认/补充 slog 日志 |
| `cmd/ministers.go` | fmt.Printf → slog（非 CLI 输出） |
| `cmd/bill.go` | 同上 |
| `cmd/session.go` | ���上 |
| `cmd/gazette.go` | 同��� |
| `cmd/hansard.go` | 同上 |
| `cmd/doctor.go` | 同上 |
| `cmd/whip.go` | 同上 |
