# House of Cards 用户交互设计方案

> 状态：**讨论稿**（不包含在 v0.3 范围内）
>
> 起草时间：2026-03-15

---

## 1. 概述

本文档定义 House of Cards 的用户交互层架构，包含三个层次：

| 层次 | 当前状态 | 目标 |
|------|---------|------|
| **CLI** | 完整 | 持续增强 |
| **TUI** | 基础 floor | 增强 |
| **GUI** | 无 | v1.0+ |

**设计原则**：
- **CLI-first**：所有功能先有 CLI，TUI/GUI 是可选层
- **渐进增强**：不破坏现有 CLI 使用方式
- **API 驱动**：TUI/GUI 复用同一套 API

---

## 2. CLI 层（基础层）

### 2.1 现状

现有 20+ 子命令，覆盖核心功能：

```
hoc bill          # 议案管理
hoc minister      # 部长管理
hoc session       # 会期管理
hoc cabinet       # 内阁管理
hoc gazette       # 公报管理
hoc hansard       # 审计记录
hoc floor         # TUI 实时监控
hoc doctor        # 健康检查
hoc serve         # API 服务
```

### 2.2 改进计划

#### D-1：交互式创建流程

**问题**：纯命令行参数对新用户不友好。

**方案**：使用 `survey` 库实现向导

```bash
# 当前
hoc init --path /Users/zhangshiyu/myapp --cabinet default

# 改进后（交互模式）
$ hoc init
? 项目路径: /Users/zhangshiyu/myapp
? 会期名称: sprint-q1
? 选择内阁配置:
  > [1] default (3 ministers)
    [2] backend (5 ministers)
    [3] frontend (2 ministers)
    [4] custom
✓ 会期 sprint-q1 已创建
```

**涉及命令**：
- `hoc init`
- `hoc bill draft`
- `hoc minister appoint`

---

#### D-2：Destructive 命令确认

**问题**：危险操作缺乏确认。

**当前状态**：

| 命令 | 确认机制 | 状态 |
|------|---------|------|
| `hoc session dissolve` | 无 | ❌ 需添加 |
| `hoc minister dismiss` | 无 | ❌ 需添加 |
| `hoc cabinet reshuffle` | `--confirm` | ⚠️ 已有 |
| `hoc session migrate` | `--confirm` | ⚠️ 已有 |

**方案**：统一 `--confirm` 标志 + 危险操作二次确认

```bash
# 强制确认模式
$ hoc session dissolve
⚠️  这将终止当前会期，所有未完成的议案将丢失。
   输入会话名称确认: sprint-q1
# 或
$ hoc session dissolve --confirm
```

---

#### D-3：智能过滤与搜索

**问题**：`hoc bill list` 只支持简单展示。

**方案**：添加 `--filter` 和 `--format` 标志

```bash
# 过滤
hoc bill list --filter status=draft
hoc bill list --filter status=draft --filter priority=high
hoc minister list --with-skill go --status idle

# 格式化输出
hoc bill list --format table  # 默认
hoc bill list --format json
hoc bill list --format compact

# 排序
hoc bill list --sort priority --desc
hoc session stats --sort quality
```

---

#### D-4：错误信息优化

**问题**：错误信息过于技术化。

**方案**：统一的错误格式 + 提示

```go
// 当前
Error: bill not found

// 改进后
Error: 议案 [bill-xyz] 不存在
Hint: 使用 hoc bill list 查看所有议案
```

---

## 3. TUI 层（实时层）

### 3.1 现状

`hoc floor` 已实现基础功能：
- 全局状态面板
- 会期/Bill/部长列表视图
- Gazette 阅读视图

### 3.2 改进计划

#### T-1：告警面板

**功能**：
- Stuck bills 列表
- Missed ACKs 告警
- DB 大小警告
- 孤儿 worktree 警告

```
┌─────────────────────────────────────────────────────────────┐
│  ⚠️ 告警面板                                    [q] 退出   │
├─────────────────────────────────────────────────────────────┤
│  🔴 Stuck Bills                                             │
│  ├─ bill-003 "API 限流"    超过 2h 无响应                   │
│  └─ bill-007 "文档站点"    超过 1h 无响应                   │
│                                                             │
│  🟡 Missed ACKs                                             │
│  └─ minister-backend: 3 次未响应                             │
│                                                             │
│  🟢 System                                                  │
│  └─ DB: 12MB (正常)                                         │
└─────────────────────────────────────────────────────────────┘
```

---

#### T-2：快捷操作

**功能**：在 TUI 中直接执行操作

```
按键映射：
  [d] dismiss minister   [p] pause bill
  [e] enact bill        [r] refresh
  [h] help              [q] quit
```

---

#### T-3：多会话切换

**功能**：同时监控多个会话

```
Tab 键切换：
  [Tab] sprint-q1 | [Tab] hotfix-2024 | [Tab] feature-auth
```

---

## 4. GUI 层（v1.0+）

> 本层不包含在 v0.3 范围内，作为 v1.0 长期规划。

### 4.1 定位

| 场景 | 推荐层 |
|------|--------|
| 日常操作（开发者） | CLI |
| 实时监控（TUI） | TUI |
| 可视化展示（非技术用户） | GUI |
| 多人协作 | GUI |

### 4.2 技术选型

| 方案 | 优点 | 缺点 |
|------|------|------|
| **Web (HTMX)** | 部署简单、API 复用、开发快 | 需要浏览器 |
| Wails (Go) | 原生桌面体验 | 包体积大、开发量大 |
| Tauri (Rust) | 包体积最小 | 需学 Rust |

**推荐**：Web (HTMX)

理由：
1. 复用已有 `serve.go` API
2. HTMX 快速实现动态页面
3. 无需安装，浏览器即用

---

### 4.3 GUI 功能规划

```
v1.0 GUI 范围
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
P0（Dashboard）
  ├─ 全局状态：活跃会话、待办议案、部长状态
  ├─ 实时看板：Whip 进度、阻塞告警
  └─ 快捷操作：一键 enact、dismiss

P1（可视化）
  ├─ DAG 可视化：Bill 依赖关系图
  ├─ 质量趋势：折线图、柱状图
  └─ Minister 负载分布

P2（管理）
  ├─ 配置编辑器（TOML 可视化）
  ├─ 日志查看器（带过滤）
  └─ 用户管理（如果有多人协作）
```

---

### 4.4 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                    用户层                                     │
│  ┌─────────┐   ┌─────────┐   ┌─────────┐                  │
│  │  CLI    │   │  TUI    │   │  Web   │                  │
│  │ (hoc)   │   │ (floor) │   │(HTMX)  │                  │
│  └────┬────┘   └────┬────┘   └────┬────┘                  │
│       │             │             │                         │
│       └─────────────┼─────────────┘                         │
│                     ▼                                        │
│              ┌─────────────┐                                 │
│              │  API Layer  │  ← 复用 serve.go               │
│              │  (REST)     │                                 │
│              └──────┬──────┘                                 │
│                     │                                        │
│       ┌─────────────┼─────────────┐                         │
│       ▼             ▼             ▼                         │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐                    │
│  │ Speaker │  │ Whip    │  │ Store   │                    │
│  └─────────┘  └─────────┘  └─────────┘                    │
└─────────────────────────────────────────────────────────────┘
```

---

### 4.5 前端目录结构（规划）

```
web/                          # v1.0+ 添加
├── index.html                # HTMX 入口
├── styles.css
├── pages/
│   ├── dashboard.html        # 主仪表盘
│   ├── bills.html            # Bill 管理
│   ├── ministers.html        # Minister 管理
│   ├── sessions.html         # Session 管理
│   └── settings.html         # 配置编辑
└── scripts/
    └── utils.js              # 工具函数
```

---

## 5. API 驱动策略

为支持 TUI 和 GUI，需提前完善 `serve.go` API：

### 5.1 现有 stub（需实现）

| Endpoint | 状态 | 优先级 |
|----------|------|--------|
| `POST /api/v1/ministers/:id/summon` | stub | P1 |
| `POST /api/v1/webhooks` | stub | P1 |

### 5.2 新增 Dashboard API（v1.0 准备）

| Endpoint | 描述 |
|----------|------|
| `GET /api/v1/dashboard` | 全局状态聚合 |
| `GET /api/v1/alerts` | 告警列表 |
| `GET /api/v1/bills/dag` | Bill 依赖图数据 |

---

## 6. 总结

| 层次 | 目标用户 | 交付版本 |
|------|---------|---------|
| CLI | 开发者 | v0.3 改进 |
| TUI | 开发者/运维 | v0.3 增强 |
| GUI | 非技术用户 | v1.0+ |

**实施路径**：

```
v0.3: CLI 改进（交互式创建、确认机制、过滤）
     ↑
v0.4: TUI 增强（告警面板、快捷操作）
     ↑
v1.0: GUI Web (HTMX) Dashboard
```

---

*本文档为讨论稿，持续更新*
