package handler

import (
	"github.com/zerodenet/zboard/backend/internal/nodecleanup"
	"net/http"
)

func (h *handlers) NodeCleanupScriptHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cleanup-zero-node.sh"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(nodecleanup.Script)
}
