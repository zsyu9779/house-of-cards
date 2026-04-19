// Package minister holds the summon pipeline shared by the CLI and the Whip
// autoscaler. It owns the logic that turns a (minister, bill) pair into a
// running AI runtime session inside a Chamber (git worktree).
package minister

import (
	"fmt"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
)

// BuildMinisterCLAUDE creates the CLAUDE.md file content for a minister's chamber.
// This file provides the minister with instructions on how to complete work and
// interact with the Gazette ACK protocol. The returned string is written verbatim
// to .claude/CLAUDE.md inside the chamber.
func BuildMinisterCLAUDE(m *store.Minister, bill *store.Bill, branch string) string {
	today := time.Now().Format("2006-01-02")

	return fmt.Sprintf(`# CLAUDE.md — Minister 工作规范

> 这是 House of Cards 多 Agent 协作框架的工作规范。
> 请仔细阅读并遵循。

---

## 当前任务

- **议案 ID**: `+"`%s`"+`
- **标题**: %s
- **工作分支**: `+"`%s`"+`
- **日期**: %s

---

## 必须完成的工作

1. 专注完成上述议案，不要做额外的事情
2. 提交所有代码变更

---

## 完成信号（必须）

完成工作后，你**必须**写入完成信号文件：

`+"```bash"+`
mkdir -p .hoc
cat > .hoc/bill-%s.done << 'EOF'
summary = "简要描述完成的工作（1-2句话）"

[contracts]
# 下游部长需要知道的接口/契约
# 例如: "api.go" = "新增 UserService 接口"
"example.go" = "描述"

[artifacts]
# 新增或修改的文件
# 例如: "internal/handler.go" = "新增"
"file.go" = "新增/修改"

[assumptions]
# 关键假设或风险
# 例如: "api-version" = "假设下游使用 v2 API"
key = "assumption"
EOF
`+"```"+`

**重要**：
- 必须使用 **TOML 格式**（如上所示）
- 必须写入 `+"`.hoc/bill-{bill-id}.done`"+` 文件
- Whip 会自动检测此文件并将议案标记为 enacted

---

## 公报模板

完成工作后，请在 `+"`gazettes/%s.md`"+` 创建公报：

`+"```markdown"+`
# Gazette: completion
> From: %s | Bill: %s | Date: %s

## 决议
[3 句话以内描述你完成了什么]

## 变更清单
- `+"`file/path`"+` — 说明

## 接口契约（下游部长需要知道的）
[如有 API/接口，列出这里；否则写"无"]

## 假设与风险
[列出关键假设；否则写"无"]

## 状态
✅ Enacted | 测试通过 | 分支: %s
`+"```"+`

---

## 公报签收协议（ACK Protocol）

当你的议案有下游依赖者时：
1. 你的完成公报会被投递到下游部长的 inbox
2. 下游部长会读取你的公报并确认（ACK）
3. 只有当**所有下游**都 ACK 后，你才会变为 idle 状态

**禁止直接 push main 分支** — 所有变更通过 Gazette 传递。

---

## 质询机制（Question Time）

如果下游部长对你的工作有疑问：
1. 他们会创建 `+"`.hoc/bill-{id}.question`"+` 文件
2. 你需要创建 `+"`.hoc/bill-{id}.answer`"+` 文件回复
3. 最多 3 轮问答，超时后会自动升级

---

## 上下文健康报告

长任务容易把 context window 撑满。你写入任何 gazette 时，**必须**在 payload
JSON 中附带当前上下文的使用情况，让党鞭（Whip）能提前干预：

`+"```json"+`
{
  "context_health": {
    "tokens_used": 85000,
    "tokens_limit": 100000,
    "turns_elapsed": 42
  }
}
`+"```"+`

- `+"`tokens_used`"+`：当前已消耗的 token 数（基于你能估算的最新值）
- `+"`tokens_limit`"+`：你的上下文窗口上限
- `+"`turns_elapsed`"+`：本会话已经经历的对话轮次

当使用率达到 80%% 党鞭会提醒你做 checkpoint；达到 90%% 会发紧急告警并把你当前
议案标记为 at-risk。请尊重提醒：要么立即写 .done 文件保存进度，要么总结已完成
部分并请求拆分 Bill。

---

*请开始工作。*
`,
		bill.ID,
		bill.Title,
		branch,
		today,
		bill.ID,
		bill.ID,
		m.Title,
		bill.ID,
		today,
		branch,
	)
}
