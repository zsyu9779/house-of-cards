# 技术方案：E-2 配置启动校验

> Phase 1 | 优先级：P1

---

## 1. 问题分析

`internal/config/config.go` 的 `LoadConfig()` 只做 TOML 语法解析，不验证字段合法性。

### 1.1 现有 LoadConfig 流程

```go
func LoadConfig(homeDir string) (*Config, error) {
    cfg := DefaultConfig(homeDir)          // 1. 填充默认值
    _, err = toml.DecodeFile(configPath, cfg)  // 2. TOML 覆盖
    cfg.Home = homeDir                     // 3. 设置 Home
    return cfg, nil                        // ← 无任何校验
}
```

### 1.2 缺失的校验项

| 字段 | 类型 | 风险 | 示例 |
|------|------|------|------|
| `Whip.HeartbeatInterval` | Duration string | 无效 duration 导致 panic | `"abc"` |
| `Whip.StuckThreshold` | Duration string | 同上 | `"not-a-duration"` |
| `Whip.MaxMinisters` | int | 零值或负值导致 autoscale 失效 | `0` / `-1` |
| `Whip.MaxRetries` | int | 负值导致永不重试 | `-1` |
| `Whip.ScaleUpThreshold` | int | 零值导致持续触发 scale-up | `0` |
| `Whip.ScaleDownThreshold` | int | 零值导致持续触发 scale-down | `0` |
| `Storage.DBPath` | string | 空字符串导致 DB 打开失败 | `""` |
| `Observability.Exporter` | string | 非法值静默无输出 | `"kafka"` |
| `Home` | string | 目录不存在导致运行时错误 | `"/nonexistent"` |

---

## 2. 方案设计

### 2.1 Validate 方法

在 `Config` 上新增 `Validate() error`，在 `LoadConfig()` 返回前调用。

```go
package config

import (
    "errors"
    "fmt"
    "os"
    "time"
)

// ValidationError wraps multiple config validation failures.
type ValidationError struct {
    Errors []string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("config validation failed (%d errors):\n  - %s",
        len(e.Errors), strings.Join(e.Errors, "\n  - "))
}

// Validate checks all config fields for correctness.
// Returns a ValidationError containing all failures (not just the first).
func (c *Config) Validate() error {
    var errs []string

    // --- Whip ---
    if c.Whip.HeartbeatInterval != "" {
        if _, err := time.ParseDuration(c.Whip.HeartbeatInterval); err != nil {
            errs = append(errs, fmt.Sprintf("whip.heartbeat_interval: invalid duration %q: %v",
                c.Whip.HeartbeatInterval, err))
        }
    }

    if c.Whip.StuckThreshold != "" {
        d, err := time.ParseDuration(c.Whip.StuckThreshold)
        if err != nil {
            errs = append(errs, fmt.Sprintf("whip.stuck_threshold: invalid duration %q: %v",
                c.Whip.StuckThreshold, err))
        } else if d < 30*time.Second {
            errs = append(errs, fmt.Sprintf("whip.stuck_threshold: %v is too small (minimum 30s)", d))
        }
    }

    if c.Whip.MaxMinisters <= 0 {
        errs = append(errs, fmt.Sprintf("whip.max_ministers: must be > 0, got %d",
            c.Whip.MaxMinisters))
    }

    if c.Whip.MaxRetries < 0 {
        errs = append(errs, fmt.Sprintf("whip.max_retries: must be >= 0, got %d",
            c.Whip.MaxRetries))
    }

    if c.Whip.ScaleUpThreshold <= 0 {
        errs = append(errs, fmt.Sprintf("whip.scale_up_threshold: must be > 0, got %d",
            c.Whip.ScaleUpThreshold))
    }

    if c.Whip.ScaleDownThreshold <= 0 {
        errs = append(errs, fmt.Sprintf("whip.scale_down_threshold: must be > 0, got %d",
            c.Whip.ScaleDownThreshold))
    }

    // --- Heartbeat vs Stuck 关系 ---
    if c.Whip.HeartbeatInterval != "" && c.Whip.StuckThreshold != "" {
        hb, err1 := time.ParseDuration(c.Whip.HeartbeatInterval)
        st, err2 := time.ParseDuration(c.Whip.StuckThreshold)
        if err1 == nil && err2 == nil && st <= hb*3 {
            errs = append(errs, fmt.Sprintf(
                "whip.stuck_threshold (%v) should be > 3x heartbeat_interval (%v)",
                st, hb))
        }
    }

    // --- Storage ---
    if c.Storage.DBPath == "" {
        errs = append(errs, "storage.db_path: must not be empty")
    }

    // --- Observability ---
    validExporters := map[string]bool{"stdout": true, "otlp": true, "nop": true}
    if c.Observability.Exporter != "" && !validExporters[c.Observability.Exporter] {
        errs = append(errs, fmt.Sprintf(
            "observability.exporter: %q is not valid (choose: stdout, otlp, nop)",
            c.Observability.Exporter))
    }

    // --- Home directory ---
    if c.Home != "" {
        if fi, err := os.Stat(c.Home); err != nil {
            errs = append(errs, fmt.Sprintf("home directory %q: %v", c.Home, err))
        } else if !fi.IsDir() {
            errs = append(errs, fmt.Sprintf("home %q is not a directory", c.Home))
        }
    }

    // --- Doctor ---
    if c.Doctor.DBSizeWarnMB <= 0 {
        errs = append(errs, fmt.Sprintf("doctor.db_size_warn_mb: must be > 0, got %d",
            c.Doctor.DBSizeWarnMB))
    }

    if len(errs) > 0 {
        return &ValidationError{Errors: errs}
    }
    return nil
}
```

### 2.2 LoadConfig 集成

```go
func LoadConfig(homeDir string) (*Config, error) {
    configPath := filepath.Join(homeDir, ".hoc", "config.toml")
    cfg := DefaultConfig(homeDir)

    _, err := os.Stat(configPath)
    if os.IsNotExist(err) {
        // 无配置文件→使用默认值，仍需校验（Home 目录等）
        if err := cfg.Validate(); err != nil {
            return nil, fmt.Errorf("default config invalid: %w", err)
        }
        return cfg, nil
    }

    _, err = toml.DecodeFile(configPath, cfg)
    if err != nil {
        return nil, fmt.Errorf("decode config: %w", err)
    }

    cfg.Home = homeDir
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("config %s: %w", configPath, err)
    }
    return cfg, nil
}
```

### 2.3 错误输出格式

```
$ hoc whip start
Error: config /Users/zhangshiyu/.hoc/config.toml: config validation failed (3 errors):
  - whip.heartbeat_interval: invalid duration "abc": time: invalid duration "abc"
  - whip.max_ministers: must be > 0, got 0
  - observability.exporter: "kafka" is not valid (choose: stdout, otlp, nop)
```

---

## 3. 测试计划

采用 table-driven 测试，覆盖每个校验规则的正/负两面：

```go
func TestConfig_Validate(t *testing.T) {
    tests := []struct {
        name    string
        modify  func(*Config)
        wantErr string // 空字符串 = 不应报错
    }{
        {
            name:    "default config is valid",
            modify:  func(c *Config) {},
            wantErr: "",
        },
        {
            name:    "invalid heartbeat_interval",
            modify:  func(c *Config) { c.Whip.HeartbeatInterval = "abc" },
            wantErr: "invalid duration",
        },
        {
            name:    "stuck_threshold too small",
            modify:  func(c *Config) { c.Whip.StuckThreshold = "5s" },
            wantErr: "too small",
        },
        {
            name:    "max_ministers zero",
            modify:  func(c *Config) { c.Whip.MaxMinisters = 0 },
            wantErr: "must be > 0",
        },
        {
            name:    "max_retries negative",
            modify:  func(c *Config) { c.Whip.MaxRetries = -1 },
            wantErr: "must be >= 0",
        },
        {
            name:    "scale_up_threshold zero",
            modify:  func(c *Config) { c.Whip.ScaleUpThreshold = 0 },
            wantErr: "must be > 0",
        },
        {
            name:    "empty db_path",
            modify:  func(c *Config) { c.Storage.DBPath = "" },
            wantErr: "must not be empty",
        },
        {
            name:    "invalid exporter",
            modify:  func(c *Config) { c.Observability.Exporter = "kafka" },
            wantErr: "not valid",
        },
        {
            name:    "stuck_threshold <= 3x heartbeat",
            modify: func(c *Config) {
                c.Whip.HeartbeatInterval = "10s"
                c.Whip.StuckThreshold = "25s"
            },
            wantErr: "should be > 3x",
        },
        {
            name: "multiple errors aggregated",
            modify: func(c *Config) {
                c.Whip.MaxMinisters = -1
                c.Storage.DBPath = ""
            },
            wantErr: "2 errors",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cfg := DefaultConfig(t.TempDir())
            tt.modify(cfg)
            err := cfg.Validate()
            if tt.wantErr == "" {
                if err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
                return
            }
            if err == nil {
                t.Fatal("expected error, got nil")
            }
            if !strings.Contains(err.Error(), tt.wantErr) {
                t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
            }
        })
    }
}
```

---

## 4. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `internal/config/config.go` | 新增 `ValidationError` + `Validate()` + 修改 `LoadConfig()` |
| `internal/config/config_test.go` | 新增，table-driven 校验测试 |
