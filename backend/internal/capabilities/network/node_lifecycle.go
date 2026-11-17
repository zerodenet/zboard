package network

// NodeManagementClosed is the irreversible boundary for current management and
// accounting. Ordinary disabled/retired nodes can still settle historical usage.
func NodeManagementClosed(status string) bool {
	return status == "deleting" || status == "archived"
}
