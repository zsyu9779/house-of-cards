package util

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmTyped_ExactMatch(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader("sprint-q1\n"), &out)
	ok, err := p.ConfirmTyped("⚠️  解散会期", "sprint-q1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected confirmation")
	}
	if !strings.Contains(out.String(), "解散会期") {
		t.Errorf("warning not printed, got: %s", out.String())
	}
}

func TestConfirmTyped_Mismatch(t *testing.T) {
	p := NewPrompter(strings.NewReader("wrong-id\n"), &bytes.Buffer{})
	ok, err := p.ConfirmTyped("", "expected-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected rejection for mismatched input")
	}
}

func TestConfirmTyped_TrimsWhitespace(t *testing.T) {
	p := NewPrompter(strings.NewReader("  hello  \n"), &bytes.Buffer{})
	ok, _ := p.ConfirmTyped("", "hello")
	if !ok {
		t.Error("expected whitespace to be trimmed")
	}
}

func TestConfirmTyped_EmptyInput(t *testing.T) {
	p := NewPrompter(strings.NewReader("\n"), &bytes.Buffer{})
	ok, _ := p.ConfirmTyped("", "anything")
	if ok {
		t.Error("empty input should not confirm")
	}
}

func TestConfirmTyped_EOFNoNewline(t *testing.T) {
	p := NewPrompter(strings.NewReader("sprint-q1"), &bytes.Buffer{})
	ok, err := p.ConfirmTyped("", "sprint-q1")
	if err != nil {
		t.Fatalf("EOF should be tolerated, got: %v", err)
	}
	if !ok {
		t.Error("expected match even without trailing newline")
	}
}

func TestConfirmYesNo_Yes(t *testing.T) {
	for _, s := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		p := NewPrompter(strings.NewReader(s), &bytes.Buffer{})
		ok, _ := p.ConfirmYesNo("")
		if !ok {
			t.Errorf("%q should confirm", s)
		}
	}
}

func TestConfirmYesNo_NoByDefault(t *testing.T) {
	for _, s := range []string{"\n", "n\n", "no\n", "anything\n"} {
		p := NewPrompter(strings.NewReader(s), &bytes.Buffer{})
		ok, _ := p.ConfirmYesNo("")
		if ok {
			t.Errorf("%q should NOT confirm", s)
		}
	}
}

func TestConfirmYesNo_PrintsWarning(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader("n\n"), &out)
	_, _ = p.ConfirmYesNo("此操作不可恢复")
	if !strings.Contains(out.String(), "不可恢复") {
		t.Errorf("warning not printed, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "[y/N]") {
		t.Errorf("y/N prompt missing, got: %s", out.String())
	}
}

func TestDefaultPrompter_UsesStdStreams(t *testing.T) {
	p := DefaultPrompter()
	if p.In == nil || p.Out == nil {
		t.Error("default prompter should have non-nil streams")
	}
}
