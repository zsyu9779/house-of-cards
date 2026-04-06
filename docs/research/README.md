# House of Cards 研究报告索引

## 文档概述

本目录包含 House of Cards 项目的深度研究报告，包括对 LangGraphGo 等项目的设计分析，为 HOC 项目提供可借鉴的设计参考。

---

## 文档列表

### LangGraphGo 分析 (重点)

| 文档 | 描述 |
|------|------|
| [05-LangGraphGo可借鉴设计分析](05-LangGraphGo可借鉴设计分析.md) | LangGraphGo 核心设计分析及其对 HOC 的借鉴价值 |

### 项目设计分析

| 文档 | 描述 |
|------|------|
| [01-项目设计分析报告](01-项目设计分析报告.md) | HOC 项目整体架构、核心模块、版本演进分析 |
| [02-政府隐喻体系分析](02-政府隐喻体系分析.md) | HOC 政府隐喻设计原理与三权分立架构 |
| [03-技术实现分析](03-技术实现分析.md) | HOC 存储层、调度引擎、配置管理等技术实现 |
| [04-可借鉴设计总结与建议](04-可借鉴设计总结与建议.md) | HOC 设计总结与改进建议 |

### 已有研究报告

| 文档 | 描述 |
|------|------|
| [2026-04-05-claude-code-source-analysis](2026-04-05-claude-code-source-analysis.md) | Claude Code 源码分析 |
| [2026-04-06-langchat-research-report](2026-04-06-langchat-research-report.md) | LangChat 研究报告 |

### 设计规范文档 (v0.3)

| 文档 | 主题 |
|------|------|
| [00-design](v0.3/00-design.md) | v0.3 设计定稿 |
| [tech-spec-E1-error-governance](v0.3/tech-spec-E1-error-governance.md) | 错误治理规范 |
| [tech-spec-E2-config-validation](v0.3/tech-spec-E2-config-validation.md) | 配置校验规范 |
| [tech-spec-C1-linter-upgrade](v0.3/tech-spec-C1-linter-upgrade.md) | Linter 升级 |
| [tech-spec-B1-whip-tests](v0.3/tech-spec-B1-whip-tests.md) | Whip 测试规范 |
| [tech-spec-context-health](v0.3/tech-spec-context-health.md) | Context 健康监控 |
| [tech-spec-A2-autoscale](v0.3/tech-spec-A2-autoscale.md) | 自动扩缩容 |
| [tech-spec-C2-structured-logging](v0.3/tech-spec-C2-structured-logging.md) | 结构化日志 |
| [user-interaction-design](v0.3/user-interaction-design.md) | 用户交互设计 |

---

## 阅读顺序建议

### 快速了解

1. [01-项目设计分析报告](01-项目设计分析报告.md) → 整体把握
2. [04-可借鉴设计总结与建议](04-可借鉴设计总结与建议.md) → 了解可借鉴点

### 深度理解

1. [02-政府隐喻体系分析](02-政府隐喻体系分析.md) → 设计理念
2. [03-技术实现分析](03-技术实现分析.md) → 技术细节
3. [05-LangGraphGo可借鉴设计分析](05-LangGraphGo可借鉴设计分析.md) → LangGraphGo 核心模式

### 参考学习

- [2026-04-05-claude-code-source-analysis](2026-04-05-claude-code-source-analysis.md) → Claude Code 对比分析
- [v0.3/tech-spec-E1-error-governance](v0.3/tech-spec-E1-error-governance.md) → 错误处理最佳实践

---

## 核心发现

### 最具借鉴价值的设计

1. **政府隐喻体系** - 通过隐喻压缩行为规范
2. **双层持久化模型** - SQLite + Git 分离状态与版本
3. **三级恢复梯度** - 渐进式故障恢复
4. **错误分级处理** - 关键/辅助/可忽略三级
5. **配置热重载** - fsnotify 动态配置

### 架构对齐验证

| HOC 设计 | Claude Code 对应 |
|----------|-----------------|
| Gazette 摘要协议 | 子代理只返回摘要 |
| Chamber (git worktree 隔离) | isolation: 'worktree' |
| Minister 禁止递归调用 | 子 Agent 无 AgentTool |
| Whip tick 驱动 | Agent Loop while(true) |

---

## 相关项目

- **LangGraphGo**: https://github.com/tongjichao/langgraphgo
- **House of Cards**: https://github.com/tongjichao/house-of-cards

---

*生成日期: 2026-04-06*
