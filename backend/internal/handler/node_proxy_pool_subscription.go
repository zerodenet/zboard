package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	proxyPoolSubscriptionMaxBytes        = 512 * 1024
	proxyPoolSubscriptionDefaultAgent    = "Clash.Meta"
	proxyPoolSubscriptionDefaultInterval = 24 * 60 * 60
	proxyPoolSubscriptionMinInterval     = 5 * 60
	proxyPoolSubscriptionMaxInterval     = 7 * 24 * 60 * 60
	proxyPoolSubscriptionPollInterval    = time.Minute
)

var proxyPoolSubscriptionFetcher = fetchProxyPoolSubscription

type proxyPoolSubscriptionSettings struct {
	Configured          bool       `json:"configured"`
	URL                 string     `json:"url,omitempty"`
	Format              string     `json:"format"`
	UserAgent           string     `json:"user_agent"`
	AutoSync            bool       `json:"auto_sync"`
	SyncIntervalSeconds int        `json:"sync_interval_seconds"`
	NodeCount           int        `json:"node_count"`
	LastSyncAt          *time.Time `json:"last_sync_at,omitempty"`
	NextSyncAt          *time.Time `json:"next_sync_at,omitempty"`
	LastSyncError       string     `json:"last_sync_error,omitempty"`
}

func normalizeProxyPoolSubscriptionFormat(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto", nil
	case "zero", "zero-base64-json", "base64-json", "znet-sink":
		return "zero", nil
	case "zero-json":
		return "zero-json", nil
	case "clash", "clash-yaml", "yaml":
		return "clash", nil
	case "sing-box", "singbox":
		return "sing-box", nil
	case "links", "uri":
		return "links", nil
	default:
		return "", errors.New("不支持的订阅格式")
	}
}

func (h *handlers) proxyPoolSubscriptionSettings(pool model.NodeProxyPool) (proxyPoolSubscriptionSettings, error) {
	settings := proxyPoolSubscriptionSettings{
		Configured: pool.SubscriptionURL != "", Format: pool.SubscriptionFormat,
		UserAgent: pool.SubscriptionUserAgent, AutoSync: pool.AutoSync,
		SyncIntervalSeconds: pool.SyncIntervalSeconds, NodeCount: pool.SubscriptionNodeCount,
		LastSyncAt: pool.LastSyncAt, NextSyncAt: pool.NextSyncAt, LastSyncError: pool.LastSyncError,
	}
	if settings.Format == "" {
		settings.Format = "auto"
	}
	if settings.UserAgent == "" {
		settings.UserAgent = proxyPoolSubscriptionDefaultAgent
	}
	if settings.SyncIntervalSeconds == 0 {
		settings.SyncIntervalSeconds = proxyPoolSubscriptionDefaultInterval
	}
	if pool.SubscriptionURL != "" {
		value, err := h.credentialCipher.Decrypt(pool.SubscriptionURL)
		if err != nil {
			return settings, errors.New("代理池订阅地址无法解密")
		}
		settings.URL = value
	}
	return settings, nil
}

func (h *handlers) NodeProxyPoolSyncHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/sync"), "/api/v1/admin/node-proxy-pools/")
	if err != nil {
		BadRequest(w, "无效的代理池")
		return
	}
	var req struct {
		ExpectedRevision *uint64 `json:"expected_revision"`
	}
	if err := decodeBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
		BadRequest(w, err.Error())
		return
	}
	pool, err := h.syncNodeProxyPoolSubscription(r.Context(), id, req.ExpectedRevision, &claims)
	if err != nil {
		switch {
		case errors.Is(err, networkcap.ErrProxyPoolNotFound):
			NotFound(w)
		case errors.Is(err, networkcap.ErrProxyPoolConflict):
			writeJSON(w, http.StatusConflict, "代理池已更新，请重新加载后再同步。", nil)
		case errors.Is(err, networkcap.ErrProxyPoolMutationPermission):
			Forbidden(w, "管理员权限已失效。")
		default:
			BadRequest(w, err.Error())
		}
		return
	}
	OK(w, pool)
}

func (h *handlers) syncNodeProxyPoolSubscription(ctx context.Context, id uint, expected *uint64, claims *authClaims) (networkcap.ProxyPoolRecord, error) {
	actor := networkcap.ProxyPoolSubscriptionActor{System: true}
	if claims != nil {
		actor = networkcap.ProxyPoolSubscriptionActor{AccountID: claims.UserID}
	}
	service := h.services.ProxyPoolSubscriptions(h.credentialCipher, proxyPoolMutationInspector{h: h})
	prepared, err := service.Prepare(ctx, actor, id, expected)
	if err != nil {
		return networkcap.ProxyPoolRecord{}, err
	}
	recordFailure := func(cause error) {
		_ = service.RecordFailure(ctx, actor, prepared, cause)
	}
	content, err := proxyPoolSubscriptionFetcher(ctx, prepared.Settings.URL, prepared.Settings.UserAgent)
	if err != nil {
		recordFailure(err)
		return prepared.Snapshot.Pool, err
	}
	path, nodeCount, err := parseProxyPoolSubscription(content, prepared.Settings.Format)
	if err != nil {
		recordFailure(err)
		return prepared.Snapshot.Pool, err
	}
	raw, err := json.Marshal(path)
	if err != nil {
		return prepared.Snapshot.Pool, err
	}
	if err := h.validateProxyPoolDocument(ctx, raw); err != nil {
		err = fmt.Errorf("订阅内容未通过 Zero 校验：%w", err)
		recordFailure(err)
		return prepared.Snapshot.Pool, err
	}
	updated, err := service.Commit(ctx, actor, prepared, string(raw), nodeCount)
	if err != nil && !errors.Is(err, networkcap.ErrProxyPoolConflict) && !errors.Is(err, networkcap.ErrProxyPoolNotFound) && !errors.Is(err, networkcap.ErrProxyPoolMutationPermission) {
		recordFailure(err)
	}
	return updated, err
}

func (h *handlers) StartProxyPoolSubscriptionWorker() {
	h.startScheduledJob("proxy_pool_sync", proxyPoolSubscriptionPollInterval, func(ctx context.Context) error { return h.syncDueProxyPools(ctx, time.Now().UTC()) })
}

func (h *handlers) CloseProxyPoolSubscriptionWorker() { h.closeScheduledJob("proxy_pool_sync") }

func (h *handlers) syncDueProxyPools(ctx context.Context, now time.Time) error {
	service := h.services.ProxyPoolSubscriptions(h.credentialCipher, proxyPoolMutationInspector{h: h})
	claims, err := service.ClaimDue(ctx, now, 10)
	if err != nil {
		log.Printf("proxy pool subscription scan failed: %v", err)
		return err
	}
	var failures []error
	for _, claim := range claims {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		expected := claim.Revision
		if _, err := h.syncNodeProxyPoolSubscription(ctx, claim.ID, &expected, nil); err != nil && !errors.Is(err, networkcap.ErrProxyPoolConflict) {
			log.Printf("proxy pool subscription sync failed: pool_id=%d error=%v", claim.ID, err)
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func proxyPoolSyncInterval(value int) (int, error) {
	if value == 0 {
		return proxyPoolSubscriptionDefaultInterval, nil
	}
	if value < proxyPoolSubscriptionMinInterval || value > proxyPoolSubscriptionMaxInterval {
		return 0, errors.New("自动同步间隔必须在 5 分钟到 7 天之间")
	}
	return value, nil
}
