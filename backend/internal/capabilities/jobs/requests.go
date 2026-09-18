package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// RequestRegistered submits only the host's registered definition. The caller
// cannot choose capacity, resource, handler, timeout or execution payload.
func (r *Runtime) RequestRegistered(ctx context.Context, owner, id, revision, key string) (Run, error) {
	if owner == "" || key == "" || len(key) > 128 {
		return Run{}, ErrInvalid
	}
	r.mu.Lock()
	entry := r.entries[id]
	r.mu.Unlock()
	if entry == nil || entry.definition.Owner != owner || entry.definition.Revision != revision || entry.maintenance {
		return Run{}, ErrConflict
	}
	entry.scheduling.Lock()
	defer entry.scheduling.Unlock()
	if entry.ctx.Err() != nil {
		return Run{}, ErrConflict
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(entry.ctx, cancel)
	defer stop()
	d := entry.definition
	payload, _ := json.Marshal(map[string]string{"revision": d.Revision})
	// Scope caller idempotency to the definition and revision as well as owner.
	digest := sha256.Sum256([]byte(id + "\x00" + revision + "\x00" + key))
	return r.store.Submit(call, Submission{Owner: owner, Key: "plugin-request:" + hex.EncodeToString(digest[:]), Handler: d.Handler, Resource: d.Resource, ExecutionGroup: d.ExecutionGroup, Timeout: d.Timeout, Payload: string(payload)})
}
func (r *Runtime) OwnerRuns(ctx context.Context, owner string, limit, offset int) ([]Run, error) {
	return r.store.List(ctx, owner, limit, offset)
}
