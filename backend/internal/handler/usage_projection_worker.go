package handler

func (h *handlers) StartUsageProjectionWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startPeriodicJob(h.services.UsageProjectionJob())
}
