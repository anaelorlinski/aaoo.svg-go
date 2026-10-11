// Package logger holds the *slog.Logger the SVG library reports through:
// ignored elements, invalid attributes, limits reached, and the like.
//
// By default records go to [slog.Default], resolved at call time, so an
// application controls them with [slog.SetDefault]. [Set] installs a
// dedicated logger instead.
package logger

import (
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
)

var current atomic.Pointer[slog.Logger]

// Set installs the logger used by the SVG library. nil restores the default.
// It is safe to call concurrently with rendering.
func Set(l *slog.Logger) { current.Store(l) }

// Swap is [Set], returning the logger installed before (nil for the
// default), to restore it later.
func Swap(l *slog.Logger) (previous *slog.Logger) { return current.Swap(l) }

// L returns the installed logger, or [slog.Default] at call time.
func L() *slog.Logger {
	if l := current.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// Warnf reports a warning, formatted as with [fmt.Sprintf].
func Warnf(format string, args ...any) {
	L().Warn(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"))
}

// Warn reports a warning, formatted as with [fmt.Sprintln].
func Warn(args ...any) {
	L().Warn(strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
}
