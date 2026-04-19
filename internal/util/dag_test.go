package util

import (
	"strings"
	"testing"
)

func TestParseDepsJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty string", "", nil},
		{"empty array", "[]", nil},
		{"single", `["bill-a"]`, []string{"bill-a"}},
		{"multiple", `["bill-a","bill-b"]`, []string{"bill-a", "bill-b"}},
		{"malformed returns nil", `["bill-a"`, nil},
		{"not an array returns nil", `"bill-a"`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseDepsJSON(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("ParseDepsJSON(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestBuildDAG_Roots(t *testing.T) {
	items := []*DAGItem{
		{ID: "a", Title: "A", Status: "enacted"},
		{ID: "b", Title: "B", Status: "draft", DependsOn: []string{"a"}},
		{ID: "c", Title: "C", Status: "draft", DependsOn: []string{"a", "b"}},
	}
	roots := BuildDAG(items)
	if len(roots) != 1 || roots[0].Item.ID != "a" {
		t.Fatalf("expected single root 'a', got %+v", roots)
	}
	// b is attached under a (its sole dep).
	aNode := roots[0]
	var bNode *DAGNode
	for _, ch := range aNode.Children {
		if ch.Item.ID == "b" {
			bNode = ch
		}
	}
	if bNode == nil {
		t.Fatalf("expected 'b' as child of 'a'")
	}
	// c has deps [a, b]; attached to LAST (b), not a.
	var cUnderA, cUnderB bool
	for _, ch := range aNode.Children {
		if ch.Item.ID == "c" {
			cUnderA = true
		}
	}
	for _, ch := range bNode.Children {
		if ch.Item.ID == "c" {
			cUnderB = true
		}
	}
	if cUnderA {
		t.Errorf("c should not be under a (attached to last dep)")
	}
	if !cUnderB {
		t.Errorf("c should be under b (last dep)")
	}
}

func TestBuildDAG_MissingDep(t *testing.T) {
	items := []*DAGItem{
		{ID: "a", Title: "A", DependsOn: []string{"ghost"}},
	}
	roots := BuildDAG(items)
	// With all deps missing, the item still becomes a root (no parent attached).
	if len(roots) != 1 || roots[0].Item.ID != "a" {
		t.Fatalf("expected 'a' as root when all deps are missing, got %+v", roots)
	}
}

func TestRenderDAG_Empty(t *testing.T) {
	got := RenderDAG(nil)
	if !strings.Contains(got, "无议案") {
		t.Errorf("expected empty placeholder, got %q", got)
	}
}

func TestRenderDAG_Tree(t *testing.T) {
	items := []*DAGItem{
		{ID: "a", Title: "root-alpha", Status: "enacted"},
		{ID: "b", Title: "child-beta", Status: "draft", DependsOn: []string{"a"}},
		{ID: "c", Title: "gamma", Status: "committee", DependsOn: []string{"a", "b"}},
	}
	got := RenderDAG(BuildDAG(items))
	if !strings.Contains(got, "[enacted]") {
		t.Errorf("expected [enacted] label, got %q", got)
	}
	if !strings.Contains(got, "└─►") {
		t.Errorf("expected child connector, got %q", got)
	}
	if !strings.Contains(got, "also needs") {
		t.Errorf("expected multi-parent annotation, got %q", got)
	}
	// status label unknown fallback
	items2 := []*DAGItem{{ID: "x", Title: "x", Status: "custom-state"}}
	out := RenderDAG(BuildDAG(items2))
	if !strings.Contains(out, "[custom-state]") {
		t.Errorf("expected fallback status label, got %q", out)
	}
}

func TestDAGStatusLabel_AllKnown(t *testing.T) {
	cases := map[string]string{
		"draft":        "[draft]",
		"reading":      "[reading]",
		"committee":    "[review]",
		"enacted":      "[enacted]",
		"royal_assent": "[royal✓]",
		"failed":       "[failed]",
	}
	for status, want := range cases {
		if got := dagStatusLabel(status); got != want {
			t.Errorf("dagStatusLabel(%q) = %q, want %q", status, got, want)
		}
	}
}
