# Agent Context - 当前工作上下文

> 每次交互必读必写

## 项目状态

- **阶段**: v0.3.0 已发布
- **Tag**: `v0.3.0` — 2026-04-29
- **CI**: 全绿通过

## 最近工作（2026-04-29）— v0.4 调研启动

### VatBrain 集成调研

- 深度调研 `/Users/zhangshiyu/vatbrain` 项目（AI Agent 记忆增强系统）
- 决策：HOC v0.4 集成 VatBrain 作为 Go 库（SQLite 后端），不为 HOC 引入外部依赖
- 创建 `docs/v0.4/v0.4-vatbrain-integration-draft.md` 技术草案
- 同步推动 VatBrain 侧 `docs/v0.2/00-storage-refactor-draft.md`（存储层可插拔重构）

### 核心方向

HOC v0.4 主题：**"记忆与智能"**——给 Speaker/Minister 外挂记忆系统，从调度器升级为能学习的框架

- VatBrain 先做存储层抽象 + SQLite 后端（v0.2）
- HOC 通过 `go get` 引入 VatBrain，`internal/memory/` 封装
- 零 Docker 依赖，共用 SQLite 文件

## 下一步

- 等待明天审查两份技术草案
- 确定后进入详细技术规约阶段

---
