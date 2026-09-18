package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

// ObserveDNS never creates, updates or deletes a provider record. The stored
// identity is mandatory; matching a name alone cannot establish ownership.
func (h *handlers) ObserveDNS(ctx context.Context, scope network.DNSReconciliationScope) (network.DNSObservation, error) {
	var out network.DNSObservation
	credential, err := h.services.DNSReconciliation(h).ObservationCredential(ctx, scope)
	if err != nil {
		return out, err
	}
	token, err := h.credentialCipher.Decrypt(credential)
	if err != nil {
		return out, err
	}
	if scope.ZoneID == "" || scope.RemoteID == "" {
		return out, network.ErrDNSUnverified
	}
	remote, err := cloudflareRequest[cloudflareRecord](ctx, http.MethodGet, "/zones/"+url.PathEscape(scope.ZoneID)+"/dns_records/"+url.PathEscape(scope.RemoteID), token, nil)
	if err != nil {
		return out, err
	}
	return network.DNSObservation{ZoneID: scope.ZoneID, RemoteID: remote.ID, Domain: remote.Name, RecordType: remote.Type, Hash: network.DNSRecordHash(remote.Type, remote.Name, remote.Content, remote.TTL, remote.Proxied)}, nil
}

func (h *handlers) AdminDNSReconcileHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	scope, err := h.services.DNSReconciliation(h).Prepare(r.Context(), claims.UserID, pathvar.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusConflict, "DNS 核验暂不可执行，请确认原任务待核验、远端标识完整且供应商账户可用。", nil)
		return
	}
	payload, _ := json.Marshal(nativeJobInput{Revision: "1", OperationID: scope.OperationID, ReviewRunID: scope.RunID, Reviewer: claims.UserID})
	h.backgroundJobs()
	run, err := h.services.JobSubmissions.Submit(r.Context(), jobs.Submission{Owner: "system", Key: "dns-reconcile:" + uuid.NewString(), Handler: "dns_reconcile", ExecutionGroup: "external", Resource: fmt.Sprintf("dns-inspection:%d", scope.RecordID), Payload: string(payload), Timeout: nativeJobTimeout("dns_reconcile")})
	if err != nil {
		ServiceUnavailable(w, "DNS 核验任务提交失败")
		return
	}
	writeJSON(w, http.StatusAccepted, "DNS 核验已加入执行队列", map[string]any{"id": run.ID, "source_run_id": scope.RunID})
}
