package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

// The final annotation is reserved for ZBoard; the preceding operator comment
// is preserved verbatim. IDs refer to the panel's records, not provider IDs.
var dnsSourceSuffix = regexp.MustCompile(`(?:^| \| )\[ZBoard dns=[0-9]+ node=[0-9]+\]$`)

func managedDNSComment(existing string, record network.ManagedDNSRecord) (string, error) {
	comment := dnsSourceSuffix.ReplaceAllString(existing, "")
	if comment != "" {
		comment += " | "
	}
	comment += fmt.Sprintf("[ZBoard dns=%d node=%d]", record.ID, record.NodeID)
	// Cloudflare's maximum is 500 characters; the Free plan allows only 100.
	// Let the provider enforce plan-specific limits without truncating user text.
	if utf8.RuneCountInString(comment) > 500 {
		return "", errors.New("添加 ZBoard 来源后，DNS 备注超过 500 字符；请缩短供应商侧的人工备注后重试")
	}
	return comment, nil
}

func applyCloudflareManagedDNSRecord(ctx context.Context, token, zoneID string, record network.ManagedDNSRecord, existing *cloudflareRecord) (cloudflareRecord, error) {
	previousComment := ""
	if existing != nil {
		previousComment = existing.Comment
	}
	comment, err := managedDNSComment(previousComment, record)
	if err != nil {
		return cloudflareRecord{}, err
	}
	payload := map[string]interface{}{
		"type": record.RecordType, "name": record.DomainName, "content": record.RecordValue,
		"ttl": record.TTL, "proxied": record.Proxied, "comment": comment,
	}
	path, method := "/zones/"+url.PathEscape(zoneID)+"/dns_records", http.MethodPost
	if existing != nil {
		// PATCH leaves provider-owned settings and tags outside our payload intact.
		path, method = path+"/"+url.PathEscape(existing.ID), http.MethodPatch
	}
	applied, err := cloudflareRequest[cloudflareRecord](ctx, method, path, token, payload)
	if err != nil {
		var requestErr *cloudflareRequestError
		if errors.As(err, &requestErr) && requestErr.StatusCode == http.StatusBadRequest {
			return cloudflareRecord{}, fmt.Errorf("供应商拒绝 DNS 同步；备注会保留人工内容并附加 ZBoard 来源，请检查字段及套餐备注长度限制：%w", err)
		}
		return cloudflareRecord{}, err
	}
	return applied, nil
}
