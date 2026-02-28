# Agent Context - 当前工作上下文

> 每次交互必读必写

## 项目状态

- **阶段**: Phase 3 ✅ 完成
- **目标**: 崩溃恢复 + Speaker AI + Hansard 记录系统

## 当前状态

Phase 3 所有子项已完成并验证：

- [x] `internal/speaker/speaker.go` — Speaker 包（GenerateContext + WriteContext + Summon）
- [x] `hoc speaker summon` — 刷新 context.md + 启动 Claude 作为 Speaker（tmux/前台）
- [x] `hoc speaker context [--refresh]` — 查看/刷新议长备忘录
- [x] `internal/whip/whip.go` — By-election 补选流程（git stash + Handoff Gazette + Hansard + 重置议案）
- [x] Whip `hansardUpdate` 每 60 秒自动刷新 speaker/context.md
- [x] `hoc bill enacted [id]` — 标记通过 + 写 Hansard + 创建 completion Gazette
- [x] `hoc hansard [minister-id]` — 部长履历（含成功率）
- [x] `hoc hansard list` — 全量议事录
- [x] `hoc minister by-election [id]` — 手动触发补选
- [x] `internal/store/store.go` — ListHansard/ListHansardByMinister/GetBillsByAssignee/ClearBillAssignment/HansardSuccessRate/ListMinistersWithStatus

## 本次改动

1. `internal/store/store.go` — 6 个新方法
2. `internal/speaker/speaker.go` — 新建 Speaker 包
3. `internal/whip/whip.go` — 增加 hocDir 字段 + byElection() + hansardUpdate 刷新 context.md
4. `cmd/hansard.go` — 完整实现
5. `cmd/bill.go` — 新增 bill enacted 命令
6. `cmd/ministers.go` — 新增 minister by-election 命令
7. `cmd/speaker.go` — 新建，speaker summon/context 命令
8. `cmd/root.go` — 注册 speakerCmd

## 验证结果（端到端测试）

- `hoc bill enacted` → 写 Hansard + completion Gazette ✅
- `hoc hansard go-minister` → 成功率 1/1 (100%) ✅
- `hoc minister by-election react-minister` → offline + bill→draft + Hansard(failed) ✅
- `hoc speaker context --refresh` → 生成完整政府备忘录 ✅
- Whip threeLineWhip 自动检测 stuck → byElection ✅

## 下一步 (Phase 4)

- [ ] `hoc floor` TUI — BubbleTea 实时监控界面
- [ ] Pipeline / Tree / Mesh 拓扑引擎扩展
- [ ] Committee 审查流程（review Gazette → committee 状态）
- [ ] Privy Council：并行 Bills enacted 后自动 git merge
- [ ] `hoc cabinet list/reshuffle` 完整实现
- [ ] Codex / Cursor runtime 接入（多 runtime 支持）

## 已知问题 / 待决策

- `minister summon --bill` 需要项目已 `hoc project add`
- Speaker prompt 中 `{{include: ...}}` 是模板语法占位，claude CLI 不支持，需改为直接嵌入 context 内容
- Privy Council 未实现（并行 Bills 完成后无自动合并）
