package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteNativeBatchDiagnosticsUseActualFinancialTransactionContext(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertNativeBatchDiagnostics(t, h, c)
}
func TestMySQLNativeBatchDiagnosticsUseActualFinancialTransactionContext(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertNativeBatchDiagnostics(t, h, c)
}
func assertNativeBatchDiagnostics(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	before, e := h.runtimeDiagnostics()
	if e != nil {
		t.Fatal(e)
	}
	if before.NativeBatchSQL.Known || before.NativeBatchSQL.Batches != 0 || before.JobStore.Scope != "this_process" {
		t.Fatal("diagnostics invented prior native samples", before)
	}
	events := accountingBenchmarkEvents(c, 0, 2)
	var payloadBytes uint64
	for _, event := range events {
		payloadBytes += uint64(len(event.Payload))
	}
	if e := h.projectZeroNodeEvents(t.Context(), events); e != nil {
		t.Fatal(e)
	}
	after, e := h.runtimeDiagnostics()
	if e != nil {
		t.Fatal(e)
	}
	sql := after.NativeBatchSQL
	if !sql.Known || sql.Phase != "native_authority_transaction" || sql.Scope != "this_process" || sql.FreshBatchCount < 1 || sql.Totals.Statements == 0 || sql.Totals.Writes == 0 || sql.Totals.TextBlobArgumentBytes == 0 || sql.Totals.SourcePayloadBytes != payloadBytes || sql.PhysicalBytesMeasured || sql.DriverTransactionsIncluded {
		t.Fatal("native call did not expose bounded actual statements", sql)
	}
	for _, sample := range sql.Samples {
		if sample.CommitConfirmed == nil || !*sample.CommitConfirmed || !sample.StatisticsComplete {
			t.Fatal(sample)
		}
	}
	raw, _ := json.Marshal(sql)
	for _, secret := range []string{c.PrincipalKey, c.CredentialID} {
		if secret != "" && strings.Contains(string(raw), secret) {
			t.Fatal("diagnostics leaked a credential or subject")
		}
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 60, subStatusActive)
}
