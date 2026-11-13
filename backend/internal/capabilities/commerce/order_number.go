package commerce

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/businessref"
)

func (o Order) MarshalJSON() ([]byte, error) {
	type raw Order
	return json.Marshal(struct {
		raw
		OrderNo string `json:"order_no"`
	}{raw(o), businessref.Order(o.ID, o.CreatedAt)})
}
