package store

import "testing"

func TestValidateBillTitle(t *testing.T) {
	// Valid titles
	validTitles := []string{
		"实现 JWT 认证",
		"Build Auth System",
		"修复登录页面的 XSS 漏洞",
		"重构数据库连接池",
	}
	for _, title := range validTitles {
		if err := ValidateBillTitle(title); err != nil {
			t.Errorf("expected valid title %q, got error: %v", title, err)
		}
	}

	// Invalid: too short
	invalidShort := []string{"fix", "ab", "修"}
	for _, title := range invalidShort {
		if err := ValidateBillTitle(title); err == nil {
			t.Errorf("expected error for short title %q", title)
		}
	}

	// Invalid: file paths
	if err := ValidateBillTitle("修复 /Users/foo/bar.go"); err == nil {
		t.Error("expected error for file path in title")
	}

	// Invalid: URLs
	if err := ValidateBillTitle("看看 https://example.com"); err == nil {
		t.Error("expected error for URL in title")
	}

	// Invalid: system metadata
	if err := ValidateBillTitle("Conversation info session"); err == nil {
		t.Error("expected error for system metadata")
	}

	// Invalid: pure punctuation
	if err := ValidateBillTitle("???"); err == nil {
		t.Error("expected error for punctuation-only title")
	}
	if err := ValidateBillTitle("..."); err == nil {
		t.Error("expected error for punctuation-only title")
	}

	// Edge: exactly 4 chars should be valid
	if err := ValidateBillTitle("修复 bug"); err != nil {
		t.Errorf("expected 4+ chars to be valid, got: %v", err)
	}
}
