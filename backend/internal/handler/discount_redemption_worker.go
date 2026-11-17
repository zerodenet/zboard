package handler

func (h *handlers) StartDiscountRedemptionWorker() {
	if h == nil || h.services == nil {
		return
	}
	job := h.services.DiscountRedemptionJob()
	job.Ready = h.jobReadiness(job.ID)
	h.startPeriodicJob(job)
}
