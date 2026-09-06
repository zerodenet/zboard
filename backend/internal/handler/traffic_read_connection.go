package handler

import (
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

// ConfigureTrafficReads is called once during server startup, after migrations
// and before serving requests. The returned close function owns only the extra
// read pool; it never closes the primary accounting connection.
func (h *handlers) ConfigureTrafficReads() (func() error, error) {
	view, closeView, err := datastore.OpenReadView(h.db)
	if err != nil {
		return nil, err
	}
	h.trafficReadDB = view
	if datastore.IsSQLite(h.db) {
		h.trafficIncrementalStats = &trafficIncrementalCache{}
	}
	return closeView, nil
}

func (h *handlers) trafficQueryDB() *gorm.DB {
	if h.trafficReadDB != nil {
		return h.trafficReadDB
	}
	return h.db
}
