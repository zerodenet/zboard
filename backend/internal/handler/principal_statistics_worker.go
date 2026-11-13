package handler

func (h *handlers) StartPrincipalStatisticsWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startPeriodicJob(h.services.PrincipalStatisticsJob())
	h.startPeriodicJob(h.services.FlowStartStatisticsJob())
	h.startPeriodicJob(h.services.PrincipalScopeRetentionJob())
	h.startPeriodicJob(h.services.FlowDetailRetentionJob())
	h.startPeriodicJob(h.services.SettlementResultJob())
	h.startPeriodicJob(h.services.LedgerRetentionJob())
	h.startPeriodicJob(h.services.ColdIdentityJob())
	h.startPeriodicJob(h.services.FlowCursorCompactionJob())
	h.startPeriodicJob(h.services.LegacyPrincipalRetentionJob())
}
