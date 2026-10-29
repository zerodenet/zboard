package handler

func (h *handlers) StartPrincipalStatisticsWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startPeriodicJob(h.services.PrincipalStatisticsJob())
	h.startPeriodicJob(h.services.FlowStartStatisticsJob())
	h.startPeriodicJob(h.services.PrincipalScopeRetentionJob())
}
