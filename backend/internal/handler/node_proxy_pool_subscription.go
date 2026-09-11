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
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	proxyPoolSubscriptionMaxBytes        = 512 * 1024
	proxyPoolSubscriptionDefaultAgent    = "Clash.Meta"
	proxyPoolSubscriptionDefaultInterval = 24 * 60 * 60
	proxyPoolSubscriptionMinInterval     = 5 * 60
	proxyPoolSubscriptionMaxInterval     = 7 * 24 * 60 * 60
	proxyPoolSubscriptionPollInterval    = time.Minute
)

var (
	errProxyPoolRevisionConflict = errors.New("proxy pool revision conflict")
	proxyPoolSyncWorkers         sync.Map
	proxyPoolSubscriptionFetcher = fetchProxyPoolSubscription
)

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

type proxyPoolSyncRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
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
		case errors.Is(err, gorm.ErrRecordNotFound):
			NotFound(w)
		case errors.Is(err, errProxyPoolRevisionConflict):
			writeJSON(w, http.StatusConflict, "代理池已更新，请重新加载后再同步。", nil)
		default:
			BadRequest(w, err.Error())
		}
		return
	}
	OK(w, pool)
}

func (h *handlers) syncNodeProxyPoolSubscription(ctx context.Context, id uint, expected *uint64, claims *authClaims) (model.NodeProxyPool, error) {
	var snapshot model.NodeProxyPool
	if err := h.db.First(&snapshot, id).Error; err != nil {
		return snapshot, err
	}
	if expected != nil && snapshot.Revision != *expected {
		return snapshot, errProxyPoolRevisionConflict
	}
	settings, err := h.proxyPoolSubscriptionSettings(snapshot)
	if err != nil {
		return snapshot, err
	}
	if !settings.Configured {
		return snapshot, errors.New("请先配置订阅地址")
	}
	content, err := proxyPoolSubscriptionFetcher(ctx, settings.URL, settings.UserAgent)
	if err != nil {
		h.recordProxyPoolSyncFailure(snapshot, err)
		return snapshot, err
	}
	path, nodeCount, err := parseProxyPoolSubscription(content, settings.Format)
	if err != nil {
		h.recordProxyPoolSyncFailure(snapshot, err)
		return snapshot, err
	}
	raw, err := json.Marshal(path)
	if err != nil {
		return snapshot, err
	}
	if err := h.validateProxyPoolDocument(ctx, raw); err != nil {
		err = fmt.Errorf("订阅内容未通过 Zero 校验：%w", err)
		h.recordProxyPoolSyncFailure(snapshot, err)
		return snapshot, err
	}
	encrypted, err := h.credentialCipher.Encrypt(string(raw))
	if err != nil {
		return snapshot, err
	}
	now := time.Now().UTC()
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var current model.NodeProxyPool
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			return err
		}
		if current.Revision != snapshot.Revision || current.SubscriptionURL != snapshot.SubscriptionURL {
			return errProxyPoolRevisionConflict
		}
		current.Config, current.SubscriptionNodeCount, current.LastSyncAt = encrypted, nodeCount, &now
		current.Revision++
		next := now.Add(time.Duration(settings.SyncIntervalSeconds) * time.Second)
		current.NextSyncAt, current.LastSyncError = &next, ""
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		var entries []model.NetworkEntry
		if err := tx.Where("proxy_pool_id = ?", current.ID).Find(&entries).Error; err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err := h.proxyPoolPath(tx, current.ID, entry.NodeID, entry.Network); err != nil {
				return fmt.Errorf("入口 %s: %w", entry.Name, err)
			}
		}
		requestedBy := uint(0)
		if claims != nil {
			requestedBy = claims.UserID
			if err := createAuditLog(tx, *claims, "node_proxy_pool.subscription.sync", fmt.Sprintf("node_proxy_pool:%d", current.ID), fmt.Sprintf("node=%d revision=%d nodes=%d", current.NodeID, current.Revision, nodeCount)); err != nil {
				return err
			}
		} else if err := tx.Create(&model.AuditLog{Actor: "system:proxy-pool-subscription", Action: "node_proxy_pool.subscription.sync", Target: fmt.Sprintf("node_proxy_pool:%d", current.ID), Detail: fmt.Sprintf("node=%d revision=%d nodes=%d", current.NodeID, current.Revision, nodeCount)}).Error; err != nil {
			return err
		}
		if err := enqueueNodeConfigPublishOnly(tx, current.NodeID, 0, requestedBy); err != nil {
			return err
		}
		snapshot = current
		return nil
	})
	if err != nil && !errors.Is(err, errProxyPoolRevisionConflict) && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.recordProxyPoolSyncFailure(snapshot, err)
	}
	return snapshot, err
}

func (h *handlers) recordProxyPoolSyncFailure(snapshot model.NodeProxyPool, cause error) {
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	next := time.Now().UTC().Add(5 * time.Minute)
	_ = h.db.Model(&model.NodeProxyPool{}).Where("id = ? AND revision = ? AND subscription_url = ?", snapshot.ID, snapshot.Revision, snapshot.SubscriptionURL).
		Updates(map[string]interface{}{"last_sync_error": message, "next_sync_at": next}).Error
}

func (h *handlers) StartProxyPoolSubscriptionWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &proxyPoolSyncRuntime{cancel: cancel, done: make(chan struct{})}
	if _, loaded := proxyPoolSyncWorkers.LoadOrStore(h, runtime); loaded {
		cancel()
		return
	}
	go func() {
		defer close(runtime.done)
		h.syncDueProxyPools(ctx, time.Now().UTC())
		ticker := time.NewTicker(proxyPoolSubscriptionPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				h.syncDueProxyPools(ctx, now.UTC())
			}
		}
	}()
}

func (h *handlers) CloseProxyPoolSubscriptionWorker() {
	value, ok := proxyPoolSyncWorkers.LoadAndDelete(h)
	if !ok {
		return
	}
	runtime := value.(*proxyPoolSyncRuntime)
	runtime.cancel()
	<-runtime.done
}

func (h *handlers) syncDueProxyPools(ctx context.Context, now time.Time) {
	var pools []model.NodeProxyPool
	if err := h.db.Where("auto_sync = ? AND subscription_url <> '' AND (next_sync_at IS NULL OR next_sync_at <= ?)", true, now).Order("next_sync_at, id").Limit(10).Find(&pools).Error; err != nil {
		log.Printf("proxy pool subscription scan failed: %v", err)
		return
	}
	for _, pool := range pools {
		if ctx.Err() != nil {
			return
		}
		leaseUntil := now.Add(5 * time.Minute)
		result := h.db.Model(&model.NodeProxyPool{}).Where("id = ? AND revision = ? AND auto_sync = ? AND (next_sync_at IS NULL OR next_sync_at <= ?)", pool.ID, pool.Revision, true, now).Update("next_sync_at", leaseUntil)
		if result.Error != nil || result.RowsAffected != 1 {
			continue
		}
		expected := pool.Revision
		if _, err := h.syncNodeProxyPoolSubscription(ctx, pool.ID, &expected, nil); err != nil && !errors.Is(err, errProxyPoolRevisionConflict) {
			log.Printf("proxy pool subscription sync failed: pool_id=%d error=%v", pool.ID, err)
		}
	}
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
