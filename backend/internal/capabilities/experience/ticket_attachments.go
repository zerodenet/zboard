package experience

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

type TicketAttachment struct {
	ID          uint   `json:"id,omitempty"`
	FileID      string `json:"file_id,omitempty"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

func NormalizeTicketAttachments(items []TicketAttachment) ([]TicketAttachment, error) {
	if len(items) > 5 {
		return nil, fmt.Errorf("%w: at most 5 attachments per message", ErrInvalid)
	}
	seen := map[string]bool{}
	for i := range items {
		item := &items[i]
		item.Name, item.URL, item.FileID = strings.TrimSpace(item.Name), strings.TrimSpace(item.URL), strings.TrimSpace(item.FileID)
		if item.FileID != "" {
			parsed, err := uuid.Parse(item.FileID)
			if err != nil || parsed.String() != item.FileID || item.URL != "" {
				return nil, fmt.Errorf("%w: attachment requires either file_id or URL", ErrInvalid)
			}
		} else {
			parsed, err := url.ParseRequestURI(item.URL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" || len(item.URL) > 2048 {
				return nil, fmt.Errorf("%w: attachment URL must be an absolute HTTP or HTTPS address", ErrInvalid)
			}
			if item.Name == "" {
				item.Name = "链接附件"
			}
			if len(item.Name) > 255 || strings.IndexFunc(item.Name, unicode.IsControl) >= 0 {
				return nil, fmt.Errorf("%w: invalid attachment name", ErrInvalid)
			}
		}
		key := item.FileID + "|" + item.URL
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate attachment", ErrInvalid)
		}
		seen[key] = true
	}
	return items, nil
}
