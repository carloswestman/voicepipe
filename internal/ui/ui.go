// Package ui provides minimal terminal styling — bold, dim, and a few accent
// colors — with no dependencies. Styling is disabled automatically when output
// isn't a terminal (piped, redirected, CI) or when NO_COLOR is set, so output
// stays clean everywhere.
package ui

import "os"

var enabled = detect()

func detect() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

const (
	reset   = "\x1b[0m"
	cBold   = "\x1b[1m"
	cDim    = "\x1b[2m"
	cCyan   = "\x1b[36m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
	cOrange = "\x1b[38;5;208m" // 256-color orange, echoing the macOS mic indicator
)

func wrap(code, s string) string {
	if !enabled || s == "" {
		return s
	}
	return code + s + reset
}

// Bold renders text in bold.
func Bold(s string) string { return wrap(cBold, s) }

// Dim renders text dimmed (for secondary/hint text).
func Dim(s string) string { return wrap(cDim, s) }

// Accent renders text in the accent color (cyan) for the thing that matters.
func Accent(s string) string { return wrap(cCyan, s) }

// Green / Yellow / Red render status colors.
func Green(s string) string  { return wrap(cGreen, s) }
func Yellow(s string) string { return wrap(cYellow, s) }
func Red(s string) string    { return wrap(cRed, s) }

// Orange renders text in orange — used for the "listening" mic indicator, to
// echo the macOS orange mic dot.
func Orange(s string) string { return wrap(cOrange, s) }
