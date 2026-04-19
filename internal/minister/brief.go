package minister

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
)

// BuildBillBrief composes the markdown brief injected into the Minister's chamber.
// The brief summarises the bill, the working branch, and the expected completion
// protocol (gazette + done-file).
func BuildBillBrief(m *store.Minister, bill *store.Bill, branch string) string {
	var skills []string
	if m.Skills != "" {
		_ = json.Unmarshal([]byte(m.Skills), &skills)
	}
	skillsStr := strings.Join(skills, ", ")
	if skillsStr == "" {
		skillsStr = "通用"
	}

	motion := bill.Description.String
	if motion == "" {
		motion = bill.Title
	}

	today := time.Now().Format("2006-01-02")

	return fmt.Sprintf(`# 部长就职简报

> **你是 %s**（ID: `+"`%s`"+`），一位 AI Agent。
> 你正在 House of Cards 多 Agent 协作框架中工作。
> 技能领域：%s

---

## 你的议案（Bill）

| 字段 | 值 |
|------|----|
| **议案 ID** | `+"`%s`"+` |
| **标题** | %s |
| **状态** | %s → In Progress |
| **工作分支** | `+"`%s`"+` |
| **日期** | %s |

## 任务指示（Motion）

%s

---

## 工作规范

1. 你正在 **git worktree（议事厅）** 中工作，分支为 `+"`%s`"+`，已与 main 分离。
2. 专注完成上述议案，不要做额外的事情。
3. 完成后，**在当前目录的 `+"`gazettes/%s.md`"+` 中创建公报**（见模板）。
4. 提交所有代码：

   `+"`"+`git add -A && git commit -m "bill(%s): %s"`+"`"+`

5. **最后一步（必须执行）**：写入完成信号文件，让 Whip 自动将议案标记为 enacted：

   `+"```bash"+`
   mkdir -p .hoc
   echo "工作已完成。[简短摘要]" > .hoc/bill-%s.done
   `+"```"+`

---

## 完成后公报模板

将以下内容写入 `+"`gazettes/%s.md`"+`：

`+"```markdown"+`
# Gazette: %s
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

*议案已就绪，请开始工作。*
`,
		m.Title,
		m.ID,
		skillsStr,
		bill.ID,
		bill.Title,
		bill.Status,
		branch,
		today,
		motion,
		branch,
		bill.ID,
		bill.ID,
		bill.ID,
		bill.ID,
		bill.Title,
		bill.ID,
		m.Title,
		bill.ID,
		today,
		branch,
	)
}

// FormatUpstreamGazette renders a single upstream gazette with its structured
// payload when available. Falls back to the summary field if the payload is
// missing or empty.
func FormatUpstreamGazette(g *store.Gazette) string {
	if g.Payload != "" {
		var p store.DoneFilePayload
		if err := json.Unmarshal([]byte(g.Payload), &p); err == nil &&
			(p.Summary != "" || len(p.Contracts) > 0 || len(p.Artifacts) > 0 || len(p.Assumptions) > 0) {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("**[%s]** %s:\n\n", g.Type.String, util.OrDash(g.FromMinister.String)))
			if p.Summary != "" {
				sb.WriteString(fmt.Sprintf("**摘要**: %s\n", p.Summary))
			}
			if len(p.Contracts) > 0 {
				sb.WriteString("**接口契约**:\n")
				for k, v := range p.Contracts {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
				}
			}
			if len(p.Artifacts) > 0 {
				sb.WriteString("**产出物**:\n")
				for k, v := range p.Artifacts {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
				}
			}
			if len(p.Assumptions) > 0 {
				sb.WriteString("**假设**:\n")
				for k, v := range p.Assumptions {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
				}
			}
			return sb.String()
		}
	}
	return fmt.Sprintf("**[%s]** %s:\n\n%s\n\n", g.Type.String, util.OrDash(g.FromMinister.String), g.Summary)
}
