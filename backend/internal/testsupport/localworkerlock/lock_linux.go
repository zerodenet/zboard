//go:build linux

// Package localworkerlock coordinates tests of the production fixed namespace
// owner across Go packages. It is imported only by _test.go files; production
// ownership has no configurable anchor or environment bypass.
package localworkerlock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Hold serializes only tests that actually exercise the fixed OS anchor. It
// does not acquire that anchor itself, so production AcquireLock is tested.
func Hold(t testing.TB) {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("zboard-projection-supervisor-tests-%d.lock", os.Geteuid()))
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		t.Fatal("open test coordination lock:", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		t.Fatal("unsafe test coordination lock")
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			t.Cleanup(func() { file.Close() })
			return
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN || time.Now().After(deadline) {
			file.Close()
			t.Fatal("test coordination lock unavailable:", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
