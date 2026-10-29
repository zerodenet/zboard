package handler

func (h *handlers) StartTrafficDiagnosticRetentionWorker() {
	h.startPeriodicJob(h.services.DiagnosticMaterialRetentionJob())
}
func (h *handlers) CloseTrafficDiagnosticRetentionWorker() {
	h.closeScheduledJob("traffic_diagnostic_retention")
}
