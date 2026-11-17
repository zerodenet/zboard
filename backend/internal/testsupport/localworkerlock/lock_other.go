//go:build !linux

package localworkerlock

import "testing"

// Production local supervision is unavailable on non-Linux platforms.
func Hold(t testing.TB) { t.Helper() }
