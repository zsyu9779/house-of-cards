// Package util provides shared utilities for the House of Cards CLI.
package util

import "strings"

// Complexity represents the estimated complexity of a bill.
type Complexity string

const (
	ComplexitySimple  Complexity = "simple"  // single-file / config changes
	ComplexityMedium  Complexity = "medium"  // multi-module / 2-4 hour work
	ComplexityComplex Complexity = "complex" // cross-system / architecture design
)

// simpleKeywords indicate low-complexity tasks.
var simpleKeywords = []string{
	"fix", "typo", "rename", "bump version", "update config",
	"hotfix", "patch", "cleanup", "fmt", "lint", "comment",
	"minor", "trivial", "small", "documentation", "readme",
}

// complexKeywords indicate high-complexity tasks.
var complexKeywords = []string{
	"design", "architecture", "migration", "refactor", "multi-",
	"distributed", "scalab", "microservice", "overhaul", "rewrite",
	"framework", "integration", "pipeline", "orchestrat", "infrastructure",
}

// EstimateBillComplexity scores a bill by keyword patterns in its title and description.
// Returns the complexity level and a confidence score [0.0, 1.0].
func EstimateBillComplexity(title, description string) (Complexity, float64) {
	combined := strings.ToLower(title + " " + description)

	complexHits := 0
	for _, kw := range complexKeywords {
		if strings.Contains(combined, kw) {
			complexHits++
		}
	}

	simpleHits := 0
	for _, kw := range simpleKeywords {
		if strings.Contains(combined, kw) {
			simpleHits++
		}
	}

	// Confidence is proportional to the number of matching keywords.
	total := len(complexKeywords) + len(simpleKeywords)

	switch {
	case complexHits > 0 && complexHits >= simpleHits:
		conf := clampConf(float64(complexHits) / float64(len(complexKeywords)))
		return ComplexityComplex, conf

	case simpleHits > 0:
		conf := clampConf(float64(simpleHits) / float64(len(simpleKeywords)))
		return ComplexitySimple, conf

	default:
		// No strong signal — medium with low confidence.
		_ = total
		return ComplexityMedium, 0.50
	}
}

func clampConf(v float64) float64 {
	if v < 0.30 {
		return 0.30
	}
	if v > 0.95 {
		return 0.95
	}
	return v
}

// ComplexityIcon returns a short emoji label for the complexity level.
func ComplexityIcon(c Complexity) string {
	switch c {
	case ComplexitySimple:
		return "🟢"
	case ComplexityComplex:
		return "🔴"
	default:
		return "🟡"
	}
}
