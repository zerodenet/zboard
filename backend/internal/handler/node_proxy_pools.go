package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type nodeProxyPoolView struct {
	model.NodeProxyPool
	EntryCount             int64 `json:"entry_count"`
	SubscriptionConfigured bool  `json:"subscription_configured"`
}

func (h *handlers) proxyPoolPath(db *gorm.DB, id, nodeID uint, network string) (networkEntryPath, error) {
	var pool model.NodeProxyPool
	var path networkEntryPath
	if err := db.First(&pool, id).Error; err != nil || pool.NodeID != nodeID {
		return path, fmt.Errorf("请选择入口节点 A 自己的共享代理池")
	}
	raw, err := h.credentialCipher.Decrypt(pool.Config)
	if err != nil {
		return path, fmt.Errorf("代理池配置无法解密")
	}
	if err := json.Unmarshal([]byte(raw), &path); err != nil {
		return path, fmt.Errorf("代理池配置无效")
	}
	if network != "tcp" {
		if err := path.validateDatagramPath(); err != nil {
			return path, err
		}
	}
	return path, nil
}

func (h *handlers) validateProxyPoolDocument(ctx context.Context, raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 128*1024 {
		return fmt.Errorf("请填写不超过 128 KiB 的代理池配置")
	}
	var path networkEntryPath
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&path); err != nil {
		return fmt.Errorf("代理池需要 outbounds、outbound_groups 和 target")
	}
	config := map[string]interface{}{
		"inbounds": []interface{}{map[string]interface{}{"tag": "entry-1", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 12345}, "protocol": map[string]interface{}{"type": "direct", "target": "192.0.2.1", "port": 443}}},
		"route":    map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}},
	}
	if err := path.appendTo(config, model.NetworkEntry{ID: 1, Network: "tcp"}); err != nil {
		return err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := managedZeroSubscriptionValidator(ctx, h.zeroArtifactDir, h.zeroLocalVersion, payload); err != nil {
		if strings.Contains(err.Error(), "only supports `http://` probe urls") {
			return fmt.Errorf("测速组使用了不支持的地址：当前 Zero 仅支持 HTTP 测速，请改用 HTTP 测速地址或留空使用默认地址")
		}
		if strings.HasPrefix(err.Error(), "resolve Zero preview validator:") || strings.HasPrefix(err.Error(), "load Zero preview validator:") {
			return fmt.Errorf("Zero 校验内核无法加载，请检查面板校验内核的版本配置和安装文件")
		}
		return fmt.Errorf("代理池未通过 Zero 校验，请检查协议字段、代理组和引用关系")
	}
	return nil
}

func (h *handlers) NodeProxyPoolsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method == http.MethodGet {
		nodeID, err := strconv.ParseUint(r.URL.Query().Get("node_id"), 10, 64)
		if err != nil || nodeID == 0 {
			BadRequest(w, "请选择节点")
			return
		}
		var rows []model.NodeProxyPool
		if err := h.db.Where("node_id = ?", nodeID).Order("id").Find(&rows).Error; err != nil {
			ServerError(w, err)
			return
		}
		result := []nodeProxyPoolView{}
		for _, row := range rows {
			var count int64
			if err := h.db.Model(&model.NetworkEntry{}).Where("proxy_pool_id = ?", row.ID).Count(&count).Error; err != nil {
				ServerError(w, err)
				return
			}
			result = append(result, nodeProxyPoolView{NodeProxyPool: row, EntryCount: count, SubscriptionConfigured: row.SubscriptionURL != ""})
		}
		OK(w, result)
		return
	}
	var id uint
	if r.Method != http.MethodPost {
		id, err = parsePathID(r.URL.Path, "/api/v1/admin/node-proxy-pools/")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	var row model.NodeProxyPool
	if id != 0 {
		if err := h.db.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				NotFound(w)
			} else {
				ServerError(w, err)
			}
			return
		}
	}
	var req struct {
		NodeID                uint             `json:"node_id"`
		Name                  string           `json:"name"`
		Config                *json.RawMessage `json:"config"`
		Revision              uint64           `json:"revision"`
		SubscriptionURL       *string          `json:"subscription_url"`
		SubscriptionFormat    string           `json:"subscription_format"`
		SubscriptionUserAgent string           `json:"subscription_user_agent"`
		AutoSync              *bool            `json:"auto_sync"`
		SyncIntervalSeconds   int              `json:"sync_interval_seconds"`
	}
	var initialSyncAt *time.Time
	var initialNodeCount int
	var subscriptionSourceChanged bool
	if r.Method != http.MethodDelete {
		if err := decodeBody(r, &req); err != nil {
			BadRequest(w, err.Error())
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.NodeID == 0 || req.Name == "" || len(req.Name) > 80 {
			BadRequest(w, "请选择节点并填写 1–80 字节的代理池名称")
			return
		}
		if id != 0 && (req.NodeID != row.NodeID || req.Revision != row.Revision) {
			writeJSON(w, http.StatusConflict, "代理池已更新或节点不匹配，请重新加载。", nil)
			return
		}
		if req.SubscriptionURL != nil {
			value := strings.TrimSpace(*req.SubscriptionURL)
			req.SubscriptionURL = &value
			format, formatErr := normalizeProxyPoolSubscriptionFormat(req.SubscriptionFormat)
			if formatErr != nil {
				BadRequest(w, formatErr.Error())
				return
			}
			req.SubscriptionFormat = format
			interval, intervalErr := proxyPoolSyncInterval(req.SyncIntervalSeconds)
			if intervalErr != nil {
				BadRequest(w, intervalErr.Error())
				return
			}
			req.SyncIntervalSeconds = interval
			req.SubscriptionUserAgent = strings.TrimSpace(req.SubscriptionUserAgent)
			if len(req.SubscriptionUserAgent) > 255 || strings.ContainsAny(req.SubscriptionUserAgent, "\r\n") {
				BadRequest(w, "订阅 User-Agent 不能超过 255 字节或包含换行")
				return
			}
			if value != "" {
				if _, err := validateProxyPoolSubscriptionURL(value); err != nil {
					BadRequest(w, err.Error())
					return
				}
			} else if req.AutoSync != nil && *req.AutoSync {
				BadRequest(w, "启用自动同步前请填写订阅地址")
				return
			}
			if id != 0 {
				settings, err := h.proxyPoolSubscriptionSettings(row)
				if err != nil {
					ServerError(w, err)
					return
				}
				subscriptionSourceChanged = settings.URL != value
			}
		}
		if id == 0 && req.Config == nil && (req.SubscriptionURL == nil || *req.SubscriptionURL == "") {
			BadRequest(w, "请配置代理池成员或订阅地址")
			return
		}
		if id == 0 && req.Config == nil {
			content, err := proxyPoolSubscriptionFetcher(r.Context(), *req.SubscriptionURL, req.SubscriptionUserAgent)
			if err != nil {
				BadRequest(w, err.Error())
				return
			}
			path, count, err := parseProxyPoolSubscription(content, req.SubscriptionFormat)
			if err != nil {
				BadRequest(w, err.Error())
				return
			}
			raw, err := json.Marshal(path)
			if err != nil {
				ServerError(w, err)
				return
			}
			message := json.RawMessage(raw)
			req.Config = &message
			now := time.Now().UTC()
			initialSyncAt = &now
			initialNodeCount = count
		}
		if req.Config != nil {
			if err := h.validateProxyPoolDocument(r.Context(), *req.Config); err != nil {
				BadRequest(w, err.Error())
				return
			}
		}
	}
	nodeID := row.NodeID
	if id == 0 {
		nodeID = req.NodeID
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, nodeID).Error; err != nil {
			return fmt.Errorf("节点不存在")
		}
		if node.LifecycleStatus == resourceStatusDeleting {
			return fmt.Errorf("节点正在删除")
		}
		if id != 0 {
			var current model.NodeProxyPool
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
				return err
			}
			if current.Revision != row.Revision {
				return fmt.Errorf("代理池已更新，请重新加载")
			}
		}
		if r.Method == http.MethodDelete {
			var count int64
			if err := tx.Model(&model.NetworkEntry{}).Where("proxy_pool_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("仍有 %d 条前置服务引用该池，请先切换转发路径", count)
			}
			if err := tx.Delete(&row).Error; err != nil {
				return err
			}
		} else {
			row.NodeID = req.NodeID
			row.Name = req.Name
			row.Revision++
			if req.Config != nil {
				encrypted, err := h.credentialCipher.Encrypt(string(*req.Config))
				if err != nil {
					return err
				}
				row.Config = encrypted
			}
			if req.SubscriptionURL != nil {
				if *req.SubscriptionURL == "" {
					row.SubscriptionURL = ""
					row.SubscriptionFormat = ""
					row.SubscriptionUserAgent = ""
					row.AutoSync = false
					row.SyncIntervalSeconds = proxyPoolSubscriptionDefaultInterval
					row.SubscriptionNodeCount = 0
					row.LastSyncAt = nil
					row.NextSyncAt = nil
					row.LastSyncError = ""
				} else {
					encrypted, err := h.credentialCipher.Encrypt(*req.SubscriptionURL)
					if err != nil {
						return err
					}
					row.SubscriptionURL = encrypted
					row.SubscriptionFormat = req.SubscriptionFormat
					row.SubscriptionUserAgent = req.SubscriptionUserAgent
					row.SyncIntervalSeconds = req.SyncIntervalSeconds
					if subscriptionSourceChanged {
						row.SubscriptionNodeCount = 0
						row.LastSyncAt = nil
						row.LastSyncError = ""
					}
					if req.AutoSync != nil {
						row.AutoSync = *req.AutoSync
					}
					if initialSyncAt != nil {
						row.LastSyncAt = initialSyncAt
						row.SubscriptionNodeCount = initialNodeCount
						next := initialSyncAt.Add(time.Duration(row.SyncIntervalSeconds) * time.Second)
						row.NextSyncAt = &next
						row.LastSyncError = ""
					} else if row.AutoSync {
						now := time.Now().UTC()
						row.NextSyncAt = &now
					} else {
						row.NextSyncAt = nil
					}
				}
			}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			var entries []model.NetworkEntry
			if err := tx.Where("proxy_pool_id = ?", row.ID).Find(&entries).Error; err != nil {
				return err
			}
			for _, entry := range entries {
				if _, err := h.proxyPoolPath(tx, row.ID, entry.NodeID, entry.Network); err != nil {
					return fmt.Errorf("入口 %s: %w", entry.Name, err)
				}
			}
		}
		if err := createAuditLog(tx, claims, "node_proxy_pool.update", fmt.Sprintf("node_proxy_pool:%d", row.ID), fmt.Sprintf("node=%d revision=%d", nodeID, row.Revision)); err != nil {
			return err
		}
		return enqueueNodeConfigPublishOnly(tx, nodeID, 0, claims.UserID)
	})
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, row)
}
