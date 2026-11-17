package commercestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestOrderReferenceDoesNotChangeLegacyFinancialEvidenceHash(t *testing.T) {
	rows := []model.Order{{ID: 48, TradeNo: "old-provider-bound-uuid", UserID: 1, AmountCents: 600, PayableAmount: 600, Currency: "CNY"}}
	type oldOrder model.Order
	old := []oldOrder{oldOrder(rows[0])}
	original, e := json.Marshal(old)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(original)
	if legacyValueHash(rows) != hex.EncodeToString(digest[:]) {
		t.Fatal("business reference rewrote historical valuation proof")
	}
	body, e := json.Marshal(rows[0])
	if e != nil {
		t.Fatal(e)
	}
	var view map[string]any
	json.Unmarshal(body, &view)
	if view["order_no"] != "DD0000000048" || view["trade_no"] != "old-provider-bound-uuid" {
		t.Fatal("public alias replaced provider identity", view)
	}
}
