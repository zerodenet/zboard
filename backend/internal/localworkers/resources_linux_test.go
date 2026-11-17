//go:build linux

package localworkers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalWorkerResourceCountersCountGuestOnceAndIncludeIOWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stat")
	if err := os.WriteFile(path, []byte("cpu 10 20 30 100 40 2 3 5 7 11\n"), 0600); err != nil {
		t.Fatal(err)
	}
	busy, total, ioWait, err := hostCPUCounters(path)
	if err != nil || total != 210 || busy != 70 || ioWait != 40 {
		t.Fatal("host counters double counted guest or lost IO pressure", busy, total, ioWait, err)
	}
}

func TestLocalWorkerCgroupQuotaIncludesPluginAndChildUsage(t *testing.T) {
	dir := t.TempDir()
	for name, value := range map[string]string{"cpu.stat": "usage_usec 9000000\n", "cpu.max": "50000 100000\n", "memory.max": "1000", "memory.current": "800", "cpuset.cpus.effective": "0-3,8"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	usage, cpus, available, err := cgroupResources(dir)
	if err != nil || usage != 9000000 || cpus != .5 || available != 200 {
		t.Fatal("cgroup resource quota not honored", usage, cpus, available, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.current"), []byte("2000"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, available, err = cgroupResources(dir)
	if err != nil || available != 0 {
		t.Fatal("memory overage underflowed into spare capacity", available, err)
	}
	for _, set := range []string{"0-3,2", "3-1", "-1", "0,,2", "0-99999999"} {
		if _, err := cpuSetCount(set); err == nil {
			t.Fatal("invalid cgroup set accepted", set)
		}
	}
}
