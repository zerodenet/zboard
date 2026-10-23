package handler

func (h *handlers) StartNodeObservationMaterialRetentionWorker() {
	h.startPeriodicJob(h.services.NodeObservationMaterialRetentionJob())
}

// Explicit global stop; local process drain keeps the existing executor policy.
func (h *handlers) CloseNodeObservationMaterialRetentionWorker() {
	h.closeScheduledJob("node_observation_material_retention")
}
