package cmd

import (
	"strings"

	"github.com/house-of-cards/hoc/internal/util"
)

// orDash delegates to util.OrDash.
func orDash(s string) string { return util.OrDash(s) }

// truncate delegates to util.Truncate.
func truncate(s string, max int) string { return util.Truncate(s, max) }

// repeat returns s repeated n times.
func repeat(s string, n int) string { return strings.Repeat(s, n) }
