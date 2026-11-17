//go:build linux

package localworkers

import (
	"bufio"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HostResources observes the complete host and the current cgroup subtree,
// including native plugin processes and supervised descendants. It never
// modifies cgroups, resource limits, security settings or filesystem contents.
type HostResources struct {
	StoragePaths []string
	mu           sync.Mutex
	previous     resourceCounters
}

type resourceCounters struct {
	at          time.Time
	hostBusy    uint64
	hostTotal   uint64
	hostIOWait  uint64
	cgroupUsage uint64
}

func (s *HostResources) Sample(now time.Time) Resources {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Resources{At: now, Reason: "resource_observation_unknown"}
	hostBusy, hostTotal, hostIOWait, err := hostCPUCounters("/proc/stat")
	if err != nil {
		return out
	}
	available, err := hostAvailableMemory("/proc/meminfo")
	if err != nil {
		return out
	}
	// A missing v2 view is intentionally unknown: host totals alone could hide
	// a container's quota. An unsupported layout never silently grants capacity.
	group, mount, err := currentCgroupDirectory()
	if err != nil {
		out.Reason = "current_cgroup_unverifiable"
		return out
	}
	if err := verifyUnlimitedVisibleAncestors(group, mount); err != nil {
		out.Reason = "ancestor_cgroup_budget_unverifiable"
		return out
	}
	usage, cpus, memory, err := cgroupResources(group)
	if err != nil {
		out.Reason = "cgroup_resource_observation_unknown"
		return out
	}
	available = min(available, memory)
	if len(s.StoragePaths) == 0 {
		out.Reason = "database_or_spool_storage_unknown"
		return out
	}
	free := uint64(math.MaxUint64)
	for _, path := range s.StoragePaths {
		if path == "" {
			out.Reason = "database_or_spool_storage_unknown"
			return out
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil || st.Bsize <= 0 || uint64(st.Bsize) > math.MaxUint64/uint64(max(st.Bavail, 1)) {
			out.Reason = "storage_observation_unknown"
			return out
		}
		free = min(free, uint64(st.Bavail)*uint64(st.Bsize))
	}
	previous := s.previous
	s.previous = resourceCounters{at: now, hostBusy: hostBusy, hostTotal: hostTotal, hostIOWait: hostIOWait, cgroupUsage: usage}
	if previous.at.IsZero() || !now.After(previous.at) || hostTotal <= previous.hostTotal || hostBusy < previous.hostBusy || hostIOWait < previous.hostIOWait || usage < previous.cgroupUsage {
		out.Reason = "CPU_sample_not_ready"
		return out
	}
	hostFraction := float64(hostBusy-previous.hostBusy) / float64(hostTotal-previous.hostTotal)
	groupFraction := float64(usage-previous.cgroupUsage) / (now.Sub(previous.at).Seconds() * 1e6 * cpus)
	if hostFraction < 0 || hostFraction > 1 || groupFraction < 0 || math.IsNaN(groupFraction) || math.IsInf(groupFraction, 0) {
		out.Reason = "CPU_observation_invalid"
		return out
	}
	// Sampling/CPU accounting granularity can briefly exceed quota. Treat it
	// as saturated, never as a negative or spare-capacity observation.
	out.Known, out.CPUFraction, out.CPUCapacity = true, min(1, max(hostFraction, groupFraction)), cpus
	out.IOWaitFraction = float64(hostIOWait-previous.hostIOWait) / float64(hostTotal-previous.hostTotal)
	out.AvailableMemoryBytes, out.MinFreeDiskBytes, out.Reason = available, free, "observed"
	return out
}

func hostCPUCounters(path string) (busy, total, ioWait uint64, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 9 || fields[0] != "cpu" {
		return 0, 0, 0, errors.New("invalid host CPU counters")
	}
	var idle uint64
	// Linux's guest counters are already included in user/nice: sum only the
	// first eight counters to avoid double accounting plugin/VM activity.
	for i, value := range fields[1:9] {
		n, e := strconv.ParseUint(value, 10, 64)
		if e != nil || total > math.MaxUint64-n {
			return 0, 0, 0, errors.New("invalid host CPU counters")
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
		if i == 4 {
			ioWait = n
		}
	}
	return total - idle, total, ioWait, nil
}

func hostAvailableMemory(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) == 3 && fields[0] == "MemAvailable:" && fields[2] == "kB" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil || value > math.MaxUint64/1024 {
				break
			}
			return value * 1024, nil
		}
	}
	return 0, errors.New("host available memory unknown")
}

func cgroupResources(path string) (usage uint64, cpus float64, memory uint64, err error) {
	data, e := os.ReadFile(filepath.Join(path, "cpu.stat"))
	if e != nil {
		return 0, 0, 0, e
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "usage_usec" {
			usage, e = strconv.ParseUint(f[1], 10, 64)
			found = e == nil
		}
	}
	if !found {
		return 0, 0, 0, errors.New("cgroup CPU usage unknown")
	}
	data, e = os.ReadFile(filepath.Join(path, "cpu.max"))
	fields := strings.Fields(string(data))
	if e != nil || len(fields) != 2 {
		return 0, 0, 0, errors.New("cgroup CPU quota unknown")
	}
	cpus = float64(runtime.NumCPU())
	period, e := strconv.ParseUint(fields[1], 10, 64)
	if e != nil || period == 0 {
		return 0, 0, 0, errors.New("invalid cgroup CPU period")
	}
	if fields[0] != "max" {
		quota, e := strconv.ParseUint(fields[0], 10, 64)
		if e != nil || quota == 0 {
			return 0, 0, 0, errors.New("invalid cgroup CPU quota")
		}
		cpus = min(cpus, float64(quota)/float64(period))
	}
	if data, e := os.ReadFile(filepath.Join(path, "cpuset.cpus.effective")); e == nil && strings.TrimSpace(string(data)) != "" {
		count, err := cpuSetCount(strings.TrimSpace(string(data)))
		if err != nil {
			return 0, 0, 0, err
		}
		cpus = min(cpus, float64(count))
	}
	limit, e := os.ReadFile(filepath.Join(path, "memory.max"))
	if e != nil {
		return 0, 0, 0, e
	}
	maximum := uint64(math.MaxUint64)
	if strings.TrimSpace(string(limit)) != "max" {
		maximum, e = strconv.ParseUint(strings.TrimSpace(string(limit)), 10, 64)
		if e != nil || maximum == 0 {
			return 0, 0, 0, errors.New("invalid cgroup memory quota")
		}
	}
	// memory.high can throttle the group before memory.max. Respect that
	// smaller actual budget instead of interpreting its reclaim stalls as spare.
	high, highErr := os.ReadFile(filepath.Join(path, "memory.high"))
	if highErr != nil && !os.IsNotExist(highErr) {
		return 0, 0, 0, highErr
	}
	if highErr == nil && strings.TrimSpace(string(high)) != "max" {
		value, e := strconv.ParseUint(strings.TrimSpace(string(high)), 10, 64)
		if e != nil {
			return 0, 0, 0, errors.New("invalid cgroup memory high budget")
		}
		maximum = min(maximum, value)
	}
	memory = math.MaxUint64
	if maximum != math.MaxUint64 {
		current, e := os.ReadFile(filepath.Join(path, "memory.current"))
		if e != nil {
			return 0, 0, 0, e
		}
		used, e := strconv.ParseUint(strings.TrimSpace(string(current)), 10, 64)
		if e != nil {
			return 0, 0, 0, e
		}
		memory = maximum - min(maximum, used)
	}

	return usage, cpus, memory, nil
}

func cpuSetCount(value string) (int, error) {
	count, previous := 0, int64(-1)
	for _, item := range strings.Split(value, ",") {
		first, last, ranged := strings.Cut(item, "-")
		a, e := strconv.ParseInt(first, 10, 32)
		if e != nil || a < 0 || a <= previous {
			return 0, errors.New("invalid cgroup CPU set")
		}
		z := a
		if ranged {
			z, e = strconv.ParseInt(last, 10, 32)
			if e != nil || z < a {
				return 0, errors.New("invalid cgroup CPU set")
			}
		}
		if z-a > 1<<20 || count > 1<<20-int(z-a+1) {
			return 0, errors.New("cgroup CPU set exceeds bound")
		}
		count += int(z - a + 1)
		previous = z
	}
	return count, nil
}

// Resolve membership instead of assuming the mount root is the service group.
// cgroup namespaces may expose an absolute hierarchy path or a namespace-relative
// path; accept a candidate only if its direct cgroup.procs contains this PID.
func currentCgroupDirectory() (string, string, error) {
	membership, err := boundedResourceFile("/proc/self/cgroup")
	if err != nil {
		return "", "", err
	}
	mounts, err := boundedResourceFile("/proc/self/mountinfo")
	if err != nil {
		return "", "", err
	}
	return resolveCurrentCgroup(string(membership), string(mounts), os.Getpid())
}

func boundedResourceFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("resource metadata exceeds observation bound")
	}
	return data, nil
}

func resolveCurrentCgroup(membership, mounts string, pid int) (string, string, error) {
	group := ""
	for _, line := range strings.Split(membership, "\n") {
		if strings.HasPrefix(line, "0::") {
			group = strings.TrimPrefix(line, "0::")
		}
	}
	if !filepath.IsAbs(group) || filepath.Clean(group) != group || pid <= 0 {
		return "", "", errors.New("invalid current cgroup membership")
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i, value := range fields {
			if value == "-" {
				separator = i
				break
			}
		}
		if len(fields) < 7 || separator < 6 || separator+1 >= len(fields) || fields[separator+1] != "cgroup2" {
			continue
		}
		root, mount := unescape.Replace(fields[3]), unescape.Replace(fields[4])
		if !filepath.IsAbs(root) || !filepath.IsAbs(mount) || filepath.Clean(root) != root || filepath.Clean(mount) != mount {
			continue
		}
		candidates := []string{}
		relative, err := filepath.Rel(root, group)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			candidates = append(candidates, filepath.Join(mount, relative))
		}
		candidates = append(candidates, filepath.Join(mount, strings.TrimPrefix(group, "/")))
		for _, candidate := range candidates {
			relative, err := filepath.Rel(mount, candidate)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				continue
			}
			data, err := boundedResourceFile(filepath.Join(candidate, "cgroup.procs"))
			if err != nil {
				continue
			}
			for _, value := range strings.Fields(string(data)) {
				current, err := strconv.Atoi(value)
				if err == nil && current == pid {
					return candidate, mount, nil
				}
			}
		}
	}
	return "", "", errors.New("current cgroup cannot be verified in the mounted hierarchy")
}

// The current group's own counters cover its descendants. A separate finite
// visible parent budget can also constrain sibling load; without observing that
// parent's full time series, do not invent spare capacity from the leaf alone.
// Standard root/unlimited ancestors remain supported. Unsupported nested finite
// budgets leave the optional supervisor unavailable, never the core service.
func verifyUnlimitedVisibleAncestors(group, mount string) error {
	current := group
	for depth := 0; current != mount && depth < 64; depth++ {
		current = filepath.Dir(current)
		relative, err := filepath.Rel(mount, current)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("cgroup parent outside mounted hierarchy")
		}
		for _, name := range []string{"cpu.max", "memory.max", "memory.high"} {
			data, err := boundedResourceFile(filepath.Join(current, name))
			if os.IsNotExist(err) && current == mount {
				continue
			} // real hierarchy root has no local limit interface
			if err != nil {
				return err
			}
			values := strings.Fields(string(data))
			if len(values) == 0 || values[0] != "max" {
				return errors.New("finite parent cgroup budget needs additional observation")
			}
		}
	}
	if current != mount {
		return errors.New("cgroup hierarchy exceeds observation bound")
	}
	return nil
}
