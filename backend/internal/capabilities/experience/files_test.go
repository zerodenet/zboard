package experience

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestUploadContentRejectsActiveAndMislabeledFiles(t *testing.T) {
	for _, input := range []struct{ name, body string }{
		{"logo.png", "<html>unsafe</html>"},
		{"logo.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`},
		{"logo.svg", `<svg onload="alert(1)"/>`},
		{"logo.svg", `<svg><image href="https://remote.example/track"/></svg>`},
		{"logo.svg", `<!DOCTYPE svg [<!ENTITY x "secret">]><svg>&x;</svg>`},
		{"logo.svg", `<svg><foreignObject/></svg>`},
		{"logo.svg", `<svg><animate attributeName="href" values="javascript:alert(1)"/></svg>`},
		{"logo.svg", `<svg/><svg/>`},
		{"page.html", "<html>page</html>"},
	} {
		if _, err := UploadContentType("site", input.name, []byte(input.body), http.DetectContentType([]byte(input.body))); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %s: %v", input.body, err)
		}
	}
	if kind, err := UploadContentType("site", "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M0 0h10v10" fill="#123456"/></svg>`), "text/xml"); err != nil || kind != "image/svg+xml" {
		t.Fatalf("safe SVG rejected: %s %v", kind, err)
	}
	if _, err := UploadContentType("site", "log.txt", []byte("plain text"), "text/plain"); err == nil {
		t.Fatal("site accepted text")
	}
	if kind, err := UploadContentType("ticket", "log.txt", []byte("plain text"), "text/plain"); err != nil || kind != "text/plain; charset=utf-8" {
		t.Fatal(kind, err)
	}
}

func TestUploadSizeAndPurposeValidationRunsBeforeStorage(t *testing.T) {
	service := Files{}
	for _, input := range []struct {
		actor   TicketActor
		purpose string
		data    []byte
	}{
		{TicketActor{}, "ticket", []byte("text")},
		{TicketActor{ID: 1}, "site", []byte("text")},
		{TicketActor{ID: 1}, "unknown", []byte("text")},
		{TicketActor{ID: 1}, "ticket", nil},
		{TicketActor{ID: 1}, "ticket", bytes.Repeat([]byte("a"), int(MaxUploadBytes)+1)},
		{TicketActor{ID: 1, IsAdmin: true}, "site", bytes.Repeat([]byte("a"), int(MaxSiteImageBytes)+1)},
	} {
		if _, err := service.Upload(context.Background(), input.actor, input.purpose, "test.txt", bytes.NewReader(input.data), time.Now()); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
}

func TestTicketAttachmentReferencesAreUnambiguousAndBounded(t *testing.T) {
	for _, items := range [][]TicketAttachment{
		{{URL: "javascript:alert(1)"}}, {{URL: "//remote.example/a"}}, {{URL: "https://user:pass@remote.example/a"}},
		{{FileID: "not-an-id"}}, {{FileID: "11111111-2222-3333-4444-555555555555", URL: "https://example.com/a"}},
		{{URL: "https://example.com/a"}, {URL: "https://example.com/a"}}, make([]TicketAttachment, 6),
	} {
		if _, err := NormalizeTicketAttachments(items); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v: %v", items, err)
		}
	}
}
