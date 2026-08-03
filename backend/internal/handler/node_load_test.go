package handler

import "testing"

func TestParseNodeLoadSnapshot(t *testing.T) {
	output := `cpu_cores 4
load_average 0.75 1.25 2.50 1/100 123
MemTotal:       8192000 kB
MemAvailable:   4096000 kB
root_filesystem /dev/vda1 20971520 5242880 15728640 25% /
uptime 90061.25 123.00`
	snapshot, err := parseNodeLoadSnapshot(output)
	if err != nil {
		t.Fatalf("parseNodeLoadSnapshot() error = %v", err)
	}
	if snapshot.CPUCoreCount != 4 || snapshot.LoadAverage1 != 0.75 || snapshot.LoadAverage15 != 2.5 {
		t.Fatalf("load snapshot = %#v", snapshot)
	}
	if snapshot.MemoryTotalBytes != 8192000*1024 || snapshot.MemoryAvailableBytes != 4096000*1024 {
		t.Fatalf("memory snapshot = %#v", snapshot)
	}
	if snapshot.RootTotalBytes != 20971520*1024 || snapshot.RootAvailableBytes != 15728640*1024 || snapshot.UptimeSeconds != 90061 {
		t.Fatalf("capacity snapshot = %#v", snapshot)
	}
}

func TestParseNodeLoadSnapshotRejectsIncompleteOutput(t *testing.T) {
	if _, err := parseNodeLoadSnapshot("cpu_cores 2\nload_average 0 0 0"); err == nil {
		t.Fatal("parseNodeLoadSnapshot() accepted incomplete output")
	}
}
