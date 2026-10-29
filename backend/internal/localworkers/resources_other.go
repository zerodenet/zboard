//go:build !linux

package localworkers

import "time"

type HostResources struct{ StoragePaths []string }

func (*HostResources) Sample(now time.Time) Resources {
	return Resources{At: now, Reason: "local worker resource sampling requires Linux cgroup v2"}
}
