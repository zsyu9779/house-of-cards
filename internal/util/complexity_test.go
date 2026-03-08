package util_test

import (
	"testing"

	"github.com/house-of-cards/hoc/internal/util"
)

func TestEstimateBillComplexity(t *testing.T) {
	tests := []struct {
		title       string
		description string
		wantLevel   util.Complexity
	}{
		// Simple cases
		{"Fix typo in README", "", util.ComplexitySimple},
		{"Update config values", "minor config change", util.ComplexitySimple},
		{"Bump version to v1.2.3", "", util.ComplexitySimple},
		{"Rename variable in auth module", "", util.ComplexitySimple},

		// Complex cases
		{"Design distributed messaging architecture", "multi-service orchestration", util.ComplexityComplex},
		{"Database migration to PostgreSQL", "full schema migration", util.ComplexityComplex},
		{"Refactor authentication pipeline", "overhaul the entire auth flow", util.ComplexityComplex},
		{"Microservice infrastructure design", "", util.ComplexityComplex},

		// Medium cases (no strong keywords)
		{"Add user profile endpoint", "implement REST endpoint for user profile", util.ComplexityMedium},
		{"Implement JWT validation", "add token validation middleware", util.ComplexityMedium},
		{"Create dashboard component", "build the main dashboard UI", util.ComplexityMedium},
	}

	for _, tc := range tests {
		level, conf := util.EstimateBillComplexity(tc.title, tc.description)
		if level != tc.wantLevel {
			t.Errorf("EstimateBillComplexity(%q, %q): got %s, want %s (conf=%.2f)",
				tc.title, tc.description, level, tc.wantLevel, conf)
		}
		if conf < 0 || conf > 1 {
			t.Errorf("EstimateBillComplexity(%q): confidence %.2f out of [0,1]", tc.title, conf)
		}
	}
}

func TestEstimateBillComplexity_ConfidenceBounds(t *testing.T) {
	cases := []struct {
		title string
		desc  string
	}{
		{"Fix typo", ""},
		{"Design architecture migration", "refactor microservice pipeline"},
		{"Add endpoint", "handle user registration"},
	}
	for _, tc := range cases {
		_, conf := util.EstimateBillComplexity(tc.title, tc.desc)
		if conf < 0 || conf > 1 {
			t.Errorf("confidence out of range for %q: %.3f", tc.title, conf)
		}
	}
}

func TestComplexityIcon(t *testing.T) {
	if util.ComplexityIcon(util.ComplexitySimple) == "" {
		t.Error("simple icon should not be empty")
	}
	if util.ComplexityIcon(util.ComplexityMedium) == "" {
		t.Error("medium icon should not be empty")
	}
	if util.ComplexityIcon(util.ComplexityComplex) == "" {
		t.Error("complex icon should not be empty")
	}
}
