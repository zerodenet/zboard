//go:build linux

package localworkers

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLocalWorkerCgroupResolutionUsesActualServiceMembership(t *testing.T) {
	dir := t.TempDir()
	mount := filepath.Join(dir, "cgroup")
	service := filepath.Join(mount, "system.slice", "zboard.service")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "cgroup.procs"), []byte("1\n2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	info := "31 29 0:32 / " + mount + " rw - cgroup2 cgroup rw\n"
	got, root, err := resolveCurrentCgroup("0::/system.slice/zboard.service\n", info, os.Getpid())
	if err != nil || got != service || root != mount {
		t.Fatal("host root was mistaken for service quota", got, root, err)
	}
	if _, _, err := resolveCurrentCgroup("0::/\n", info, os.Getpid()); err == nil {
		t.Fatal("nonmember root became local capacity evidence")
	}
}

func TestLocalWorkerCgroupResolutionHandlesNamespacedMountAndEscapes(t *testing.T) {
	mount := filepath.Join(t.TempDir(), "group with space")
	if err := os.MkdirAll(mount, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(mount, " ", `\040`)
	info := "31 29 0:32 /docker/owned " + escaped + " rw - cgroup2 cgroup rw\n"
	for _, group := range []string{"/", "/docker/owned"} {
		got, root, err := resolveCurrentCgroup("0::"+group+"\n", info, os.Getpid())
		if err != nil || got != mount || root != mount {
			t.Fatal("namespace membership unresolved", group, got, root, err)
		}
	}
	for _, group := range []string{"/../../other", "relative", "//noncanonical"} {
		if _, _, err := resolveCurrentCgroup("0::"+group+"\n", info, os.Getpid()); err == nil {
			t.Fatal("invalid group path accepted", group)
		}
	}
}

func TestLocalWorkerFiniteParentQuotaRemainsUnknown(t *testing.T) {
	mount := t.TempDir()
	parent := filepath.Join(mount, "slice")
	service := filepath.Join(parent, "service")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{mount, parent} {
		for name, value := range map[string]string{"cpu.max": "max 100000", "memory.max": "max", "memory.high": "max"} {
			if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := verifyUnlimitedVisibleAncestors(service, mount); err != nil {
		t.Fatal("ordinary unlimited ancestors unsupported", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "cpu.max"), []byte("50000 100000"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyUnlimitedVisibleAncestors(service, mount); err == nil {
		t.Fatal("sibling-constrained parent quota looked like spare leaf capacity")
	}
}

func TestLocalWorkerCgroupMemoryHighIsNotSpareMemory(t *testing.T) {
	dir := t.TempDir()
	for name, value := range map[string]string{"cpu.stat": "usage_usec 10", "cpu.max": "max 100000", "memory.max": "1000", "memory.high": "500", "memory.current": "400"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, _, available, err := cgroupResources(dir)
	if err != nil || available != 100 {
		t.Fatal("memory.high throttle budget ignored", available, err)
	}
}
