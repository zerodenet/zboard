package handler

func (h *handlers) StartNodeObservationWorker() {
	h.startPeriodicJob(h.services.NodeObservationJob())
}

// This is an explicit global administrative stop. Process shutdown uses the
// existing local runtime Drain/Close path instead.
func (h *handlers) CloseNodeObservationWorker() { h.closeScheduledJob("node_observation") }
