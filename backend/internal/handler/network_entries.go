package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
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

type networkEntryView = networkcap.NetworkEntryListItem

func (h *handlers) NetworkEntriesHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method == http.MethodGet {
		result, err := h.services.NetworkEntryQueries.List(r.Context(), claims.UserID)
		if errors.Is(err, networkcap.ErrNetworkEntryQueryPermission) {
			Forbidden(w, "管理员权限已失效。")
			return
		}
		if err != nil {
			ServerError(w, err)
			return
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
	entryMutations := h.services.NetworkEntryMutations(h.credentialCipher, proxyPoolMutationInspector{h: h})
	if id != 0 && r.Method != http.MethodDelete {
		if err := entryMutations.CheckRevision(r.Context(), claims.UserID, id, request.Revision); err != nil {
			switch {
			case errors.Is(err, networkcap.ErrNetworkEntryConflict):
				writeJSON(w, http.StatusConflict, "入口已更新，请刷新后重试。", nil)
			case errors.Is(err, networkcap.ErrNetworkEntryNotFound):
				NotFound(w)
			case errors.Is(err, networkcap.ErrNetworkEntryPermission):
				Forbidden(w, "管理员权限已失效。")
			default:
				ServerError(w, err)
			}
			return
		}
	}
	if r.Method == http.MethodDelete {
		if err := h.services.TopologyRemoval.Entry(r.Context(), claims.UserID, id); err != nil {
			resourceRemovalError(w, err)
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
	if len(fields) > 0 {
		BadRequestFields(w, "网络前置配置不完整。", fields)
		return
	}
	pathConfig := ""
	if request.PathConfig != nil {
		raw := bytes.TrimSpace(*request.PathConfig)
		if len(raw) != 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte("{}")) {
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
			pathConfig = string(raw)
		}
	}
	poolID := request.ProxyPoolID
	if poolID != nil && *poolID == 0 {
		poolID = nil
	}
	if poolID != nil && request.PathConfig != nil && pathConfig != "" {
		BadRequest(w, "共享代理池和独立代理路径只能选择一个")
		return
	}
	membershipChanges := make([]networkcap.NetworkEntryMembershipChange, 0, len(changes))
	for _, change := range changes {
		membershipChanges = append(membershipChanges, networkcap.NetworkEntryMembershipChange{
			NodeGroupID: change.NodeGroupID, ExpectedRevision: change.ExpectedRevision, Member: change.Member,
		})
	}
	result, err := entryMutations.Save(r.Context(), claims.UserID, networkcap.NetworkEntryMutationRequest{
		ID: id, ExpectedRevision: request.Revision, NodeID: request.NodeID, EndpointID: request.EndpointID,
		Name: request.Name, Network: request.Network, Address: request.Address, Port: request.Port,
		PublicPort: request.PublicPort, Enabled: request.Enabled, ReplaceProxyPool: request.ProxyPoolID != nil,
		ProxyPoolID: poolID, ReplacePath: request.PathConfig != nil, Path: pathConfig,
		MembershipChanges: membershipChanges, CredentialProtocols: h.storedSubscriptionCredentialProtocols(),
	})
	if err != nil {
		var validation *networkcap.NetworkEntryMutationValidation
		switch {
		case errors.As(err, &validation):
			BadRequestFields(w, "网络前置保存失败。", map[string]string{"configuration": validation.Error()})
		case errors.Is(err, networkcap.ErrNetworkEntryConflict):
			writeJSON(w, http.StatusConflict, "入口或节点组已更新，请刷新后重试。", nil)
		case errors.Is(err, networkcap.ErrNetworkEntryNotFound):
			NotFound(w)
		case errors.Is(err, networkcap.ErrNetworkEntryPermission):
			Forbidden(w, "管理员权限已失效。")
		default:
			ServerError(w, err)
		}
		return
	}
	if len(result.ReconcileTaskIDs) > 0 {
		h.StartAdminTaskWorker()
	}
	writeJSON(w, http.StatusOK, "已保存，正在排队发布 A 的转发配置。", networkEntryView{NetworkEntryRecord: result.Entry, HasPath: result.HasPath, Pending: true})
}
