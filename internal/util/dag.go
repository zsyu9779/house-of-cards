// Package util provides shared formatting and display helpers for House of Cards.
package util

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DAGItem represents a bill-like entity for DAG rendering.
// Callers convert their domain types to DAGItem before rendering.
type DAGItem struct {
	ID        string
	Title     string
	Status    string
	DependsOn []string // IDs this item depends on (must finish before this one)
}

// DAGNode is a node in the visual dependency tree.
type DAGNode struct {
	Item     *DAGItem
	Children []*DAGNode // items that depend on this item (visual children)
	AllDeps  []string   // all dependency IDs (for multi-parent annotation)
}

// BuildDAG constructs a visual dependency tree from a list of DAGItems.
// Bills with multiple parents are attached to their LAST listed dependency so
// they appear after all their prerequisites in the rendered tree.
// Returns root nodes (items with no dependencies).
func BuildDAG(items []*DAGItem) []*DAGNode {
	nodeMap := make(map[string]*DAGNode, len(items))
	for _, item := range items {
		nodeMap[item.ID] = &DAGNode{Item: item, AllDeps: item.DependsOn}
	}

	hasParent := make(map[string]bool)
	for _, item := range items {
		for i, depID := range item.DependsOn {
			parent, ok := nodeMap[depID]
			if !ok {
				continue
			}
			hasParent[item.ID] = true
			// Attach as visual child to the LAST dependency only.
			// Earlier deps are noted in AllDeps for annotation.
			if i == len(item.DependsOn)-1 {
				parent.Children = append(parent.Children, nodeMap[item.ID])
			}
		}
	}

	var roots []*DAGNode
	for _, item := range items {
		if !hasParent[item.ID] {
			roots = append(roots, nodeMap[item.ID])
		}
	}
	return roots
}

// ParseDepsJSON parses a JSON array of bill IDs (e.g. `["bill-abc","bill-def"]`).
// Returns nil for empty or invalid input.
func ParseDepsJSON(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var deps []string
	_ = json.Unmarshal([]byte(s), &deps)
	return deps
}

// RenderDAG renders a list of root DAGNodes as an ASCII dependency tree.
// Example output:
//
//	[enacted]  auth-api
//	[enacted]  auth-ui
//	[enacted]  auth-db
//	    └─► [working]  integration   (also needs: auth-api, auth-ui)
//	            └─► [draft]  final-review
func RenderDAG(roots []*DAGNode) string {
	if len(roots) == 0 {
		return "  (无议案)\n"
	}
	var sb strings.Builder
	visited := make(map[string]bool)
	for _, root := range roots {
		renderDAGNode(&sb, root, "", true, visited)
	}
	return sb.String()
}

func renderDAGNode(sb *strings.Builder, node *DAGNode, prefix string, isRoot bool, visited map[string]bool) {
	item := node.Item
	label := dagStatusLabel(item.Status)
	title := Truncate(item.Title, 32)

	// Annotation for multi-parent bills: show earlier deps not shown in the tree.
	extra := ""
	if len(node.AllDeps) > 1 {
		others := node.AllDeps[:len(node.AllDeps)-1]
		short := make([]string, 0, len(others))
		for _, dep := range others {
			short = append(short, Truncate(dep, 22))
		}
		extra = fmt.Sprintf("  (also needs: %s)", strings.Join(short, ", "))
	}

	if isRoot {
		sb.WriteString(fmt.Sprintf("%-11s  %s%s\n", label, title, extra))
	} else {
		sb.WriteString(fmt.Sprintf("%s└─► %-11s  %s%s\n", prefix, label, title, extra))
	}

	if visited[item.ID] {
		return
	}
	visited[item.ID] = true

	childPrefix := prefix + "    "
	for _, child := range node.Children {
		renderDAGNode(sb, child, childPrefix, false, visited)
	}
}

func dagStatusLabel(status string) string {
	switch status {
	case "draft":
		return "[draft]"
	case "reading":
		return "[reading]"
	case "committee":
		return "[review]"
	case "enacted":
		return "[enacted]"
	case "royal_assent":
		return "[royal✓]"
	case "failed":
		return "[failed]"
	default:
		return "[" + status + "]"
	}
}
