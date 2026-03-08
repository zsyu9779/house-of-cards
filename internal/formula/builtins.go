package formula

// Builtins returns the five built-in formulas shipped with the binary.
// Users can override any of these by creating a same-named .toml in their
// formula directory.
func Builtins() []*Formula {
	return []*Formula{
		cleanupChambers(),
		autoMerge(),
		syncMain(),
		healthCheck(),
		archiveSession(),
	}
}

// ── 1. cleanup-chambers ────────────────────────────────────────────────────

// cleanupChambers removes git worktrees for offline Ministers that have been
// idle for over 24 hours.
func cleanupChambers() *Formula {
	return &Formula{
		Name:        "cleanup-chambers",
		Description: "清理长期空闲（>24h）的 Minister 议事厅（git worktree）",
		Trigger:     "manual",
		builtin:     true,
		Steps: []Step{
			{
				Name: "prune-worktrees",
				Actions: []Action{
					{
						Type:    "git",
						Command: "worktree prune",
						Targets: []string{},
					},
				},
			},
			{
				Name: "list-remaining",
				Actions: []Action{
					{
						Type:    "git",
						Command: "worktree list",
					},
				},
			},
		},
	}
}

// ── 2. auto-merge ──────────────────────────────────────────────────────────

// autoMerge merges all enacted Bill branches into the project's main branch.
func autoMerge() *Formula {
	return &Formula{
		Name:        "auto-merge",
		Description: "自动合并所有已通过审查（enacted）的议案分支到主分支",
		Trigger:     "manual",
		builtin:     true,
		Steps: []Step{
			{
				Name: "checkout-main",
				Actions: []Action{
					{
						Type:    "hoc",
						Command: "privy merge --session latest",
					},
				},
			},
		},
	}
}

// ── 3. sync-main ───────────────────────────────────────────────────────────

// syncMain rebases all active Chamber worktrees onto the latest main branch.
func syncMain() *Formula {
	return &Formula{
		Name:        "sync-main",
		Description: "将所有活跃议事厅（chambers）同步到最新 main 分支",
		Trigger:     "manual",
		builtin:     true,
		Steps: []Step{
			{
				Name:     "fetch-origin",
				Parallel: false,
				Actions: []Action{
					{
						Type:    "git",
						Command: "fetch origin",
					},
				},
			},
			{
				Name:     "rebase-chambers",
				Parallel: true,
				Actions: []Action{
					{
						Type:    "git",
						Command: "rebase origin/main",
						Targets: []string{"chambers/*"},
					},
				},
				OnFailure: []Action{
					{
						Type:    "notify",
						Message: "议事厅 {{.Target}} rebase 失败，请手动处理",
					},
				},
			},
		},
	}
}

// ── 4. health-check ────────────────────────────────────────────────────────

// healthCheck runs the hoc doctor command to verify system health.
func healthCheck() *Formula {
	return &Formula{
		Name:        "health-check",
		Description: "执行全局健康检查（等同于 hoc doctor）",
		Trigger:     "manual",
		builtin:     true,
		Steps: []Step{
			{
				Name: "run-doctor",
				Actions: []Action{
					{
						Type:    "hoc",
						Command: "doctor",
					},
				},
			},
			{
				Name: "whip-report",
				Actions: []Action{
					{
						Type:    "hoc",
						Command: "whip report",
					},
				},
			},
		},
	}
}

// ── 5. archive-session ─────────────────────────────────────────────────────

// archiveSession dissolves all completed Sessions and archives their Gazettes.
func archiveSession() *Formula {
	return &Formula{
		Name:        "archive-session",
		Description: "归档所有已完成（completed）会期，清理 Chamber worktrees",
		Trigger:     "manual",
		builtin:     true,
		Steps: []Step{
			{
				Name: "list-completed",
				Actions: []Action{
					{
						Type:    "hoc",
						Command: "session list --status completed",
					},
				},
			},
			{
				Name: "prune-worktrees",
				Actions: []Action{
					{
						Type:    "git",
						Command: "worktree prune",
					},
				},
			},
		},
	}
}
