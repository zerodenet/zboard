package handler

import (
	"bytes"
	"github.com/zerodenet/zboard/backend/internal/nodecleanup"
	"net/http/httptest"
	"testing"
)

func TestCleanupScriptDownloadRequiresAdminAndContainsNoCredentials(t *testing.T) {
	f := newTrafficReadFixture(t)
	for _, tc := range []struct {
		token  string
		status int
	}{{f.token, 403}, {f.admin, 200}} {
		w := httptest.NewRecorder()
		f.h.NodeCleanupScriptHandler(w, announcementRequest("GET", "/api/v1/admin/node-cleanup-script", tc.token, ""))
		if w.Code != tc.status {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if tc.status == 200 {
			if !bytes.Equal(w.Body.Bytes(), nodecleanup.Script) {
				t.Fatal("script modified during delivery")
			}
			if w.Header().Get("Content-Disposition") != `attachment; filename="cleanup-zero-node.sh"` {
				t.Fatal("download filename missing")
			}
			if bytes.Contains(w.Body.Bytes(), []byte(f.admin)) {
				t.Fatal("credential leaked")
			}
		}
	}
}
