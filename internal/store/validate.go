package store

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidateBillTitle checks a bill title against quality rules.
// Returns nil if valid, or a descriptive error.
func ValidateBillTitle(title string) error {
	title = strings.TrimSpace(title)

	// Length checks (Unicode-aware)
	charCount := utf8.RuneCountInString(title)
	if charCount < 4 {
		return fmt.Errorf("议案标题过短（最少 4 个字符，当前 %d 个）", charCount)
	}
	if charCount > 120 {
		return fmt.Errorf("议案标题过长（最多 120 个字符，当前 %d 个）", charCount)
	}

	// Reject file paths
	pathPatterns := []string{"/Users/", "/home/", "/tmp/", "./", "../", "C:\\", "D:\\"}
	for _, p := range pathPatterns {
		if strings.Contains(title, p) {
			return fmt.Errorf("议案标题不应包含文件路径（检测到 %q）", p)
		}
	}

	// Reject URLs
	if strings.Contains(title, "http://") || strings.Contains(title, "https://") {
		return fmt.Errorf("议案标题不应包含 URL")
	}

	// Reject system metadata keywords
	metaKeywords := []string{"Conversation info", "session info", "System prompt"}
	lower := strings.ToLower(title)
	for _, kw := range metaKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return fmt.Errorf("议案标题不应包含系统元数据关键词（检测到 %q）", kw)
		}
	}

	// Reject pure punctuation/whitespace
	// A title with only punctuation and spaces is not meaningful
	var punctOnly = regexp.MustCompile(`^[\p{P}\p{S}\s]+$`)
	if punctOnly.MatchString(title) {
		return fmt.Errorf("议案标题不能仅包含标点符号或空白")
	}

	return nil
}
