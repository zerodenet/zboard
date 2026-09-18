package handler

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

// ConfigureTrafficReads is called once during server startup, after migrations
// and before serving requests. The returned close function owns only the extra
// read pool; it never closes the primary accounting connection.
func (h *handlers) ConfigureTrafficReads() (func() error, error) {
	closeView, err := h.services.ConfigureTrafficReads()
	if err != nil {
		return nil, err
	}
	if h.services.TrafficReadsUseSQLite() {
		h.trafficIncrementalStats = &meteringstore.IncrementalCache{}
	}
	return closeView, nil
}
