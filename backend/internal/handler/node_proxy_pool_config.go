package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Configuration reads are explicit, administrator-only and audited. List and
// mutation responses continue to omit pool credentials.
func (h *handlers) NodeProxyPoolConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/config")
	id, err := parsePathID(path, "/api/v1/admin/node-proxy-pools/")
	if err != nil {
		BadRequest(w, "无效的代理池")
		return
	}
	var pool model.NodeProxyPool
	if err := h.db.First(&pool, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
		} else {
			ServerError(w, err)
		}
		return
	}
	raw, err := h.credentialCipher.Decrypt(pool.Config)
	if err != nil {
		ServerError(w, fmt.Errorf("代理池配置无法解密"))
		return
	}
	var graph networkEntryPath
	if json.Unmarshal([]byte(raw), &graph) != nil {
		ServerError(w, fmt.Errorf("代理池配置无效"))
		return
	}
	compiled := map[string]interface{}{}
	target, err := graph.appendGraph(compiled, fmt.Sprintf("pool-%d/", pool.ID))
	if err != nil {
		ServerError(w, fmt.Errorf("代理池配置无法编译"))
		return
	}
	compiled["target"] = target
	if err := createAuditLog(h.db, claims, "node_proxy_pool.config.read", fmt.Sprintf("node_proxy_pool:%d", id), fmt.Sprintf("node=%d revision=%d", pool.NodeID, pool.Revision)); err != nil {
		ServerError(w, err)
		return
	}
	subscription, err := h.proxyPoolSubscriptionSettings(pool)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, struct {
		Pool         model.NodeProxyPool           `json:"pool"`
		Config       json.RawMessage               `json:"config"`
		Compiled     map[string]interface{}        `json:"compiled"`
		Subscription proxyPoolSubscriptionSettings `json:"subscription"`
	}{pool, json.RawMessage(raw), compiled, subscription})
}

const poolRuntimeBegin = "__ZBOARD_POOL_CONFIG_BEGIN__\n"
const poolRuntimeEnd = "\n__ZBOARD_POOL_CONFIG_END__"
const poolRuntimeReadCommand = `set -eu
test -r /etc/zerodenet/current.json
printf '__ZBOARD_POOL_CONFIG_BEGIN__\n'
head -c 8388609 /etc/zerodenet/current.json
printf '\n__ZBOARD_POOL_CONFIG_END__\n'`

type nodeProxyPoolRuntimeSnapshot struct {
	Source  string                 `json:"source"`
	NodeID  uint                   `json:"node_id"`
	SHA256  string                 `json:"sha256"`
	ReadAt  time.Time              `json:"read_at"`
	Present bool                   `json:"present"`
	Config  map[string]interface{} `json:"config"`
}

func (h *handlers) NodeProxyPoolRuntimeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/runtime"), "/api/v1/admin/node-proxy-pools/")
	if err != nil {
		BadRequest(w, "无效的代理池")
		return
	}
	var pool model.NodeProxyPool
	if err := h.db.First(&pool, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
		} else {
			ServerError(w, err)
		}
		return
	}
	node, err := h.loadNode(pool.NodeID)
	if err != nil {
		NotFound(w)
		return
	}
	if err := createAuditLog(h.db, claims, "node_proxy_pool.runtime.read", fmt.Sprintf("node_proxy_pool:%d", id), fmt.Sprintf("node=%d", pool.NodeID)); err != nil {
		ServerError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	output, _, err := h.execSSHCommandWithPrivilegeContext(ctx, node, poolRuntimeReadCommand, normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) != sshPrivilegeNone)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, "无法读取节点当前配置，请检查 SSH 连接和读取权限；编辑草稿仍可继续。", nil)
		return
	}
	snapshot, err := extractNodeProxyPoolRuntime(output, pool.ID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, "节点当前配置无法解析或超过 8 MiB。", nil)
		return
	}
	snapshot.NodeID = pool.NodeID
	OK(w, snapshot)
}

// Never expose the full node document: it can contain connector secrets and
// credentials for unrelated pools or protocol listeners.
func extractNodeProxyPoolRuntime(output string, poolID uint) (nodeProxyPoolRuntimeSnapshot, error) {
	result := nodeProxyPoolRuntimeSnapshot{Source: "/etc/zerodenet/current.json", ReadAt: time.Now().UTC()}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	start := strings.Index(output, poolRuntimeBegin)
	if start < 0 {
		return result, fmt.Errorf("missing configuration")
	}
	raw := output[start+len(poolRuntimeBegin):]
	end := strings.LastIndex(raw, poolRuntimeEnd)
	if end < 0 || end > 8*1024*1024 {
		return result, fmt.Errorf("invalid configuration size")
	}
	raw = raw[:end]
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return result, fmt.Errorf("invalid configuration object")
	}
	var document struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
		Groups    []map[string]interface{} `json:"outbound_groups"`
		Inbounds  []map[string]interface{} `json:"inbounds"`
		Route     struct {
			Rules []map[string]interface{} `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return result, err
	}
	digest := sha256.Sum256([]byte(raw))
	result.SHA256 = hex.EncodeToString(digest[:])
	prefix := fmt.Sprintf("pool-%d/", poolID)
	owned := func(v interface{}) bool { s, ok := v.(string); return ok && strings.HasPrefix(s, prefix) }
	outbounds, groups, rules, inbounds := []map[string]interface{}{}, []map[string]interface{}{}, []map[string]interface{}{}, []map[string]interface{}{}
	for _, item := range document.Outbounds {
		if owned(item["tag"]) {
			outbounds = append(outbounds, item)
		}
	}
	for _, item := range document.Groups {
		if owned(item["tag"]) {
			groups = append(groups, item)
		}
	}
	entryTags := map[string]bool{}
	for _, rule := range document.Route.Rules {
		action, _ := rule["action"].(map[string]interface{})
		if !owned(action["outbound"]) {
			continue
		}
		rules = append(rules, rule)
		condition, _ := rule["condition"].(map[string]interface{})
		if condition["type"] == "inbound" {
			values, _ := condition["values"].([]interface{})
			for _, value := range values {
				if tag, ok := value.(string); ok {
					entryTags[tag] = true
				}
			}
		}
	}
	for _, item := range document.Inbounds {
		tag, _ := item["tag"].(string)
		protocol, _ := item["protocol"].(map[string]interface{})
		if entryTags[tag] && protocol["type"] == "direct" {
			inbounds = append(inbounds, item)
		}
	}
	result.Present = len(outbounds)+len(groups) > 0
	result.Config = map[string]interface{}{"outbounds": outbounds, "outbound_groups": groups, "inbounds": inbounds, "route": map[string]interface{}{"rules": rules}}
	return result, nil
}
