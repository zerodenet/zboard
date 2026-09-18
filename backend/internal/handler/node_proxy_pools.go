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

	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type nodeProxyPoolMutationRequest struct {
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

type proxyPoolMutationInspector struct{ h *handlers }

func (i proxyPoolMutationInspector) InspectProxyPoolConfiguration(ctx context.Context, raw string, validateNative bool) (networkcap.ProxyPoolConfigurationFacts, error) {
	if validateNative {
		if err := i.h.validateProxyPoolDocument(ctx, json.RawMessage(raw)); err != nil {
			return networkcap.ProxyPoolConfigurationFacts{}, err
		}
	}
	return zeroadapter.InspectProxyPoolConfiguration(raw)
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
		result, err := h.services.ProxyPoolQueries.List(r.Context(), claims.UserID, uint(nodeID))
		if err != nil {
			if errors.Is(err, networkcap.ErrProxyPoolQueryPermission) {
				Forbidden(w, "管理员权限已失效。")
				return
			}
			ServerError(w, err)
			return
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
	var req nodeProxyPoolMutationRequest
	var initialSyncAt *time.Time
	var initialNodeCount int
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
	}
	command := networkcap.ProxyPoolMutationRequest{
		ID: id, NodeID: req.NodeID, Name: req.Name, ExpectedRevision: req.Revision,
		Delete: r.Method == http.MethodDelete, SubscriptionURL: req.SubscriptionURL,
		SubscriptionFormat: req.SubscriptionFormat, SubscriptionUserAgent: req.SubscriptionUserAgent,
		AutoSync: req.AutoSync, SyncIntervalSeconds: req.SyncIntervalSeconds,
		InitialSyncAt: initialSyncAt, InitialNodeCount: initialNodeCount,
	}
	if req.Config != nil {
		value := string(bytes.TrimSpace(*req.Config))
		command.Config = &value
	}
	row, err := h.services.ProxyPoolMutations(h.credentialCipher, proxyPoolMutationInspector{h: h}).Save(r.Context(), claims.UserID, command)
	if err != nil {
		var validation *networkcap.ProxyPoolMutationValidation
		switch {
		case errors.As(err, &validation):
			BadRequest(w, validation.Error())
		case errors.Is(err, networkcap.ErrProxyPoolConflict):
			writeJSON(w, http.StatusConflict, "代理池已更新或节点不匹配，请重新加载。", nil)
		case errors.Is(err, networkcap.ErrProxyPoolNotFound):
			NotFound(w)
		case errors.Is(err, networkcap.ErrProxyPoolMutationPermission):
			Forbidden(w, "管理员权限已失效。")
		default:
			ServerError(w, err)
		}
		return
	}
	OK(w, row)
}
