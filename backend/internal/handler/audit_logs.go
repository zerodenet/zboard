package handler

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const auditDetailMaxBytes = 16 * 1024

var (
	auditSensitiveAssignment = regexp.MustCompile(`(?i)((?:"?(?:password|passwd|secret|token|private[_-]?key|authorization|cookie)"?)\s*[:=]\s*)(?:"(?:\\.|[^"])*"|'[^']*'|[^,\s;}\]}]+)`)
	auditBearerCredential    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
)

type auditLogSummary struct {
	ID        uint      `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	HasDetail bool      `json:"has_detail"`
	CreatedAt time.Time `json:"created_at"`
}

type auditLogDetail struct {
	auditLogSummary
	UserID *uint  `json:"user_id,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func auditUserID(value uint) *uint {
	return &value
}

func newAuditLogSummary(item model.AuditLog) auditLogSummary {
	return auditLogSummary{
		ID:        item.ID,
		Actor:     item.Actor,
		Action:    item.Action,
		Target:    item.Target,
		HasDetail: strings.TrimSpace(item.Detail) != "",
		CreatedAt: item.CreatedAt,
	}
}

func newAuditLogDetail(item model.AuditLog) auditLogDetail {
	return auditLogDetail{
		auditLogSummary: newAuditLogSummary(item),
		UserID:          item.UserID,
		Detail:          sanitizeAuditDetail(item.Detail),
	}
}

func auditLogSummaries(items []model.AuditLog) []auditLogSummary {
	result := make([]auditLogSummary, 0, len(items))
	for _, item := range items {
		result = append(result, newAuditLogSummary(item))
	}
	return result
}

func sanitizeAuditDetail(raw string) string {
	value := strings.ReplaceAll(raw, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, value)
	value = auditSensitiveAssignment.ReplaceAllString(value, `${1}"[redacted]"`)
	value = auditBearerCredential.ReplaceAllString(value, "Bearer [redacted]")
	value = strings.TrimSpace(value)
	if len(value) <= auditDetailMaxBytes {
		return value
	}
	cut := auditDetailMaxBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "…"
}

func (h *handlers) AuditLogDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/audit-logs/")
	if err != nil {
		BadRequest(w, "invalid audit log id")
		return
	}
	var item model.AuditLog
	if err := h.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, newAuditLogDetail(item))
}
