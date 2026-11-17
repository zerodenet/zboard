package hostmetrics

// Missing observations and counter resets break a differential sample. A known
// zero remains a real zero; it is never used to stand in for a failed read.
func counterRate(value, previous uint64, elapsed float64, previousValid bool) *float64 {
	if !previousValid || elapsed <= 0 || value < previous {
		return nil
	}
	v := float64(value-previous) / elapsed
	return &v
}
func cpuPercent(total, idle, previousTotal, previousIdle uint64, previousValid bool) *float64 {
	if !previousValid || total <= previousTotal || idle < previousIdle || total-previousTotal < idle-previousIdle {
		return nil
	}
	v := 100 * (1 - float64(idle-previousIdle)/float64(total-previousTotal))
	return &v
}
