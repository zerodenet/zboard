package model

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/businessref"
)

// Old orders acquire the same stable public reference without rewriting a
// provider-bound trade number or running a historical backfill at startup.
func (o Order) MarshalJSON() ([]byte, error) {
	type raw Order
	return json.Marshal(struct {
		raw
		OrderNo string `json:"order_no"`
	}{raw(o), businessref.Order(o.ID, o.CreatedAt)})
}
