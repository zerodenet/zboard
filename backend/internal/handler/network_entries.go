package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type networkEntryRequest struct {
	ProxyPoolID       *uint                                       `json:"proxy_pool_id"`
	ParentProtocolID  uint                                        `json:"parent_protocol_id"`
	MembershipChanges []protocolEndpointNodeGroupMembershipChange `json:"node_group_membership_changes"`
	Network           string                                      `json:"network"`
	Name              string                                      `json:"name"`
	NodeID            uint                                        `json:"node_id"`
	EndpointID        uint                                        `json:"endpoint_id"`
	Address           string                                      `json:"address"`
	Port              int                                         `json:"port"`
	PublicPort        int                                         `json:"public_port"`
	Enabled           bool                                        `json:"enabled"`
	PathConfig        *json.RawMessage                            `json:"path_config"`
	Revision          uint64                                      `json:"revision"`
}

type networkEntryView struct {
	ServiceKind      string                                `json:"service_kind"`
	ParentProtocolID uint                                  `json:"parent_protocol_id"`
	Memberships      []protocolEndpointNodeGroupMembership `json:"node_group_memberships"`
	LandingNodeID    uint                                  `json:"landing_node_id"`
	NodeGroupNames   []string                              `json:"node_group_names"`
	model.NetworkEntry
	HasPath      bool   `json:"has_path"`
	NodeName     string `json:"node_name"`
	EndpointName string `json:"endpoint_name"`
	Pending      bool   `json:"pending"`
	LastError    string `json:"last_error"`
}

func (h *handlers) NetworkEntriesHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method == http.MethodGet {
		var rows []model.NetworkEntry
		if err := h.db.Order("id DESC").Find(&rows).Error; err != nil {
			ServerError(w, err)
			return
		}
		result := make([]networkEntryView, 0, len(rows))
		for _, row := range rows {
			var node model.Node
			var endpoint model.ProtocolEndpoint
			var pending model.NodeConfigPublish
			if err := h.db.First(&node, row.NodeID).Error; err != nil {
				ServerError(w, err)
				return
			}
			if err := h.db.First(&endpoint, row.EndpointID).Error; err != nil {
				ServerError(w, err)
				return
			}
			query := h.db.Where("node_id IN ?", []uint{row.NodeID, endpoint.NodeID}).Order("last_error DESC, node_id").Limit(1).Find(&pending)
			if query.Error != nil {
				ServerError(w, query.Error)
				return
			}
			names := []string{}
			if err := h.db.Table("node_groups").Select("node_groups.name").Joins("JOIN node_group_network_entries membership ON membership.node_group_id = node_groups.id").Where("membership.network_entry_id = ?", row.ID).Order("node_groups.name, node_groups.id").Pluck("name", &names).Error; err != nil {
				ServerError(w, err)
				return
			}
			memberships, err := loadNetworkEntryMemberships(h.db, row.ID)
			if err != nil {
				ServerError(w, err)
				return
			}
			result = append(result, networkEntryView{ServiceKind: "forward", ParentProtocolID: row.EndpointID, Memberships: memberships, NetworkEntry: row, NodeGroupNames: names, LandingNodeID: endpoint.NodeID, HasPath: row.PathConfig != "", NodeName: node.Name, EndpointName: endpoint.Name, Pending: query.RowsAffected > 0, LastError: pending.LastError})
		}
		writeJSON(w, http.StatusOK, "ok", result)
		return
	}
	var id uint
	if r.Method != http.MethodPost {
		id, err = parsePathID(r.URL.Path, "/api/v1/admin/network-entries/")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	var request networkEntryRequest
	if r.Method != http.MethodDelete {
		if err := decodeBody(r, &request); err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	var row model.NetworkEntry
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
	if r.Method == http.MethodDelete {
		err = h.db.Transaction(func(tx *gorm.DB) error {
			if _, err := deleteNetworkEntryRecords(tx, 0, claims.UserID, "id = ?", row.ID); err != nil {
				return err
			}
			if err := createAuditLog(tx, claims, "network_entry.delete", fmt.Sprintf("network_entry:%d", row.ID), fmt.Sprintf("node=%d endpoint=%d", row.NodeID, row.EndpointID)); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			var validation *requestValidationError
			if errors.As(err, &validation) {
				BadRequestError(w, err)
			} else {
				ServerError(w, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, "入口已删除，已排队撤除节点配置。", nil)
		return
	}
	if request.ParentProtocolID != 0 {
		if request.EndpointID != 0 && request.EndpointID != request.ParentProtocolID {
			BadRequest(w, "父协议与落地协议不一致")
			return
		}
		request.EndpointID = request.ParentProtocolID
	}
	changes, err := normalizeProtocolEndpointNodeGroupMembershipChanges(request.MembershipChanges, id == 0)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	fields := map[string]string{}
	if request.Network == "" {
		request.Network = "tcp_udp"
	}
	if request.Network != "tcp" && request.Network != "tcp_udp" {
		fields["network"] = "请选择 TCP 或 TCP/UDP 转发。"
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Address = strings.TrimSpace(request.Address)
	if request.Name == "" || len(request.Name) > 80 {
		fields["name"] = "请输入 1–80 字节的入口名称。"
	}
	if !networkEntryAddressValid(request.Address) {
		fields["address"] = "请输入客户端连接 A 的 IP 或域名，不包含端口或 URL。"
	}
	if request.Port < 1 || request.Port > 65535 {
		fields["port"] = "监听端口必须为 1–65535。"
	}
	if request.PublicPort == 0 {
		request.PublicPort = request.Port
	}
	if request.PublicPort < 1 || request.PublicPort > 65535 {
		fields["public_port"] = "对外端口必须为 1–65535。"
	}
	if request.NodeID == 0 {
		fields["node_id"] = "请选择入口节点 A。"
	}
	if request.EndpointID == 0 {
		fields["endpoint_id"] = "请选择 B 的落地协议。"
	}
	if id != 0 && request.Revision != row.Revision {
		writeJSON(w, http.StatusConflict, "入口已更新，请刷新后重试。", nil)
		return
	}
	if len(fields) > 0 {
		BadRequestFields(w, "网络前置配置不完整。", fields)
		return
	}
	encryptedPath := row.PathConfig
	if request.PathConfig != nil {
		raw := bytes.TrimSpace(*request.PathConfig)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("{}")) {
			encryptedPath = ""
		} else {
			var path networkEntryPath
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&path); err != nil {
				BadRequestFields(w, "代理路径格式错误。", map[string]string{"path_config": "只接受 outbounds、outbound_groups 和 target。"})
				return
			}
			if err := decoder.Decode(new(interface{})); err != io.EOF {
				BadRequest(w, "代理路径只能包含一个 JSON 对象。")
				return
			}
			config := map[string]interface{}{"inbounds": []interface{}{map[string]interface{}{"tag": "entry-1", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 12345}, "protocol": map[string]interface{}{"type": "direct", "target": "192.0.2.1", "port": 443}}}, "mode": map[string]interface{}{"type": "rule"}, "route": map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}}}
			if err := path.appendTo(config, model.NetworkEntry{ID: 1, Network: request.Network}); err != nil {
				BadRequestFields(w, "代理路径无效。", map[string]string{"path_config": err.Error()})
				return
			}
			config["inbounds"].([]interface{})[0].(map[string]interface{})["udp"] = map[string]interface{}{"enabled": request.Network != "tcp"}
			payload, _ := json.Marshal(config)
			if err := managedZeroSubscriptionValidator(r.Context(), h.zeroArtifactDir, h.zeroLocalVersion, payload); err != nil {
				BadRequestFields(w, "代理路径未通过 Zero 校验。", map[string]string{"path_config": "请检查协议字段、引用关系以及代理链的 TCP/UDP 能力，并确认面板已安装 Zero 校验内核。"})
				return
			}
			encryptedPath, err = h.credentialCipher.Encrypt(string(raw))
			if err != nil {
				ServerError(w, err)
				return
			}
		}
	}
	// A retained TCP path also needs compatibility checking when enabling UDP.
	if encryptedPath != "" && request.Network != "tcp" && (request.ProxyPoolID == nil || *request.ProxyPoolID == 0) {
		raw, err := h.credentialCipher.Decrypt(encryptedPath)
		if err != nil {
			ServerError(w, err)
			return
		}
		var path networkEntryPath
		if err := json.Unmarshal([]byte(raw), &path); err != nil {
			ServerError(w, err)
			return
		}
		if err := path.validateDatagramPath(); err != nil {
			BadRequestFields(w, "代理路径与 UDP 转发不兼容。", map[string]string{"path_config": err.Error()})
			return
		}
	}
	poolID := row.ProxyPoolID
	if request.ProxyPoolID != nil {
		poolID = request.ProxyPoolID
		if *poolID == 0 {
			poolID = nil
		}
	}
	if poolID != nil {
		if request.PathConfig != nil && encryptedPath != "" {
			BadRequest(w, "共享代理池和独立代理路径只能选择一个")
			return
		}
		encryptedPath = ""
	}
	var tasks []model.Task
	previous := row
	row = model.NetworkEntry{DeliverySortOrder: previous.DeliverySortOrder, ProxyPoolID: poolID, Network: request.Network, ID: id, Name: request.Name, NodeID: request.NodeID, EndpointID: request.EndpointID, Address: request.Address, Port: request.Port, PublicPort: request.PublicPort, Enabled: request.Enabled, PathConfig: encryptedPath, Revision: previous.Revision + 1, CreatedAt: previous.CreatedAt}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		// Serialize against other entry/endpoint mutations on the same nodes.
		var endpoint model.ProtocolEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, row.EndpointID).Error; err != nil {
			return fmt.Errorf("请选择存在的落地协议")
		}
		var nodes []model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", []uint{row.NodeID, endpoint.NodeID}).Order("id").Find(&nodes).Error; err != nil {
			return err
		}
		if len(nodes) != 2 {
			return fmt.Errorf("A 与 B 必须是两个存在的不同节点")
		}
		for _, node := range nodes {
			if row.Enabled && (!node.IsEnabled || node.LifecycleStatus == resourceStatusDeleting) {
				return fmt.Errorf("A、B 节点必须启用且未在删除")
			}
		}
		if row.ProxyPoolID != nil {
			if _, err := h.proxyPoolPath(tx, *row.ProxyPoolID, row.NodeID, row.Network); err != nil {
				return err
			}
		}
		if request.Network == "tcp" && endpoint.Protocol == "hysteria2" {
			return fmt.Errorf("Hysteria2 使用 UDP，请选择 TCP/UDP 转发")
		}
		if row.Enabled && !endpoint.IsActive {
			return fmt.Errorf("落地协议必须启用")
		}
		var legacy int64
		if err := tx.Model(&model.ProtocolCredential{}).Where("protocol_endpoint_id = ? AND status = ? AND listen_port <> ?", endpoint.ID, protocolCredentialStatusActive, endpoint.Port).Count(&legacy).Error; err != nil {
			return err
		}
		if legacy > 0 {
			return fmt.Errorf("请先重新发布 B 并将旧凭据迁移到统一协议端口")
		}
		var duplicateName int64
		if err := tx.Model(&model.NetworkEntry{}).Where("endpoint_id = ? AND name = ? AND id <> ?", row.EndpointID, row.Name, row.ID).Count(&duplicateName).Error; err != nil {
			return err
		}
		if duplicateName > 0 {
			return fmt.Errorf("同一个落地协议的入口名称不能重复")
		}
		if err := networkEntryPortAvailable(tx, row.NodeID, row.Port, row.ID); err != nil {
			return err
		}
		if id == 0 {
			if err := createNetworkEntry(tx, &row, claims.UserID); err != nil {
				return err
			}
			if err := h.applyNetworkEntryMembershipChanges(tx, row, changes, claims, &tasks); err != nil {
				return err
			}
			return createAuditLog(tx, claims, "network_entry.create", fmt.Sprintf("network_entry:%d", row.ID), fmt.Sprintf("node=%d endpoint=%d", row.NodeID, row.EndpointID))
		}
		update := tx.Model(&model.NetworkEntry{}).Where("id = ? AND revision = ?", id, previous.Revision).Select("*").Omit("delivery_sort_order").Updates(&row)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("入口已更新，请刷新后重试")
		}
		if previous.EndpointID != row.EndpointID {
			if err := enqueueNetworkEntryPublishes(tx, previous, claims.UserID); err != nil {
				return err
			}
		}
		if previous.NodeID != row.NodeID {
			if err := enqueueNodeConfigPublish(tx, previous.NodeID, 0, claims.UserID); err != nil {
				return err
			}
		}
		if err := h.applyNetworkEntryMembershipChanges(tx, row, changes, claims, &tasks); err != nil {
			return err
		}
		if err := createAuditLog(tx, claims, "network_entry.update", fmt.Sprintf("network_entry:%d", row.ID), fmt.Sprintf("node=%d endpoint=%d", row.NodeID, row.EndpointID)); err != nil {
			return err
		}
		return enqueueNetworkEntryPublishes(tx, row, claims.UserID)
	})
	if err != nil {
		BadRequestFields(w, "网络前置保存失败。", map[string]string{"configuration": err.Error()})
		return
	}
	for i := range tasks {
		_ = h.startPersistedAdminTask(&tasks[i])
	}
	writeJSON(w, http.StatusOK, "已保存，正在排队发布 A 的转发配置。", networkEntryView{NetworkEntry: row, HasPath: row.PathConfig != "", Pending: true})
}

func createNetworkEntry(tx *gorm.DB, row *model.NetworkEntry, userID uint) error {
	if err := tx.Create(row).Error; err != nil {
		return err
	}
	return enqueueNetworkEntryPublishes(tx, *row, userID)
}

func enqueueNetworkEntryPublishes(tx *gorm.DB, entry model.NetworkEntry, userID uint) error {
	var endpoint model.ProtocolEndpoint
	if err := tx.First(&endpoint, entry.EndpointID).Error; err != nil {
		return err
	}
	if err := enqueueNodeConfigPublish(tx, endpoint.NodeID, endpoint.ID, userID); err != nil {
		return err
	}
	return enqueueNodeConfigPublishOnly(tx, entry.NodeID, 0, userID)
}
