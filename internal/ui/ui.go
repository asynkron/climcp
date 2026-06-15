// Package ui provides small, dependency-free terminal styling helpers. Colors
// are emitted only when stdout is a real terminal, NO_COLOR is unset, and TERM
// is not "dumb"; otherwise every styler is the identity function so piped or
// redirected output stays clean.
package ui

import (
	"os"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
)

// enabled reports whether ANSI styling should be emitted. It is resolved once
// at package init based on the environment and the nature of stdout.
var enabled = detectColor()

func detectColor() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// SetEnabled overrides color detection (used by tests).
func SetEnabled(v bool) { enabled = v }

// Enabled reports whether styling is currently on.
func Enabled() bool { return enabled }

func wrap(code, s string) string {
	if !enabled {
		return s
	}
	return code + s + reset
}

// Bold renders s in bold.
func Bold(s string) string { return wrap(bold, s) }

// Dim renders s dimmed.
func Dim(s string) string { return wrap(dim, s) }

// Red renders s in red (errors).
func Red(s string) string { return wrap(red, s) }

// Green renders s in green (success / values).
func Green(s string) string { return wrap(green, s) }

// Yellow renders s in yellow (warnings / optional markers).
func Yellow(s string) string { return wrap(yellow, s) }

// Blue renders s in blue.
func Blue(s string) string { return wrap(blue, s) }

// Cyan renders s in cyan (headings / identifiers).
func Cyan(s string) string { return wrap(cyan, s) }
