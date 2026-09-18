package networkstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProtocolEndpointMutations struct{ DB *gorm.DB }

func (s ProtocolEndpointMutations) LoadProtocolEndpointMutation(ctx context.Context, actor, id uint) (out network.ProtocolEndpointMutationSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrProtocolEndpointMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := protocolEndpointAdministrator(tx, actor); err != nil {
			return err
		}
		var row model.ProtocolEndpoint
		if err := tx.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProtocolEndpointNotFound
			}
			return err
		}
		certificateID, err := protocolEndpointCertificateID(tx, row.ID)
		if err != nil {
			return err
		}
		out = protocolEndpointMutationSnapshot(row, certificateID)
		return nil
	})
	return
}

func (s ProtocolEndpointMutations) CommitProtocolEndpointMutation(ctx context.Context, actor uint, before *network.ProtocolEndpointMutationSnapshot, change network.ProtocolEndpointMutationChange) (out network.ProtocolEndpointMutationResult, err error) {
	if s.DB == nil {
		return out, network.ErrProtocolEndpointMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProtocolEndpointMutationPermission
			}
			return err
		}

		nodeIDs := []uint{change.Endpoint.NodeID}
		if before != nil {
			nodeIDs = append(nodeIDs, before.Endpoint.NodeID)
		}
		if err := lockAvailableProtocolEndpointNodes(tx, nodeIDs); err != nil {
			return err
		}

		var previous model.ProtocolEndpoint
		if before != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, before.Endpoint.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return network.ErrProtocolEndpointNotFound
				}
				return err
			}
			certificateID, err := protocolEndpointCertificateID(tx, previous.ID)
			if err != nil {
				return err
			}
			if before.Version == "" || protocolEndpointSnapshotVersion(previous, certificateID) != before.Version {
				return network.ErrProtocolEndpointConflict
			}
		}

		if err := validateProtocolEndpointParent(tx, change.Endpoint.ID, change.Endpoint.NodeID, change.Endpoint.ParentProtocolID); err != nil {
			return err
		}
		if err := validateProtocolEndpointTopology(tx, change.Endpoint); err != nil {
			return err
		}
		if err := validateProtocolEndpointCertificate(tx, change.ManagedCertificateID, change.Endpoint.NodeID, change.Endpoint.Protocol, change.Now); err != nil {
			return err
		}

		row := protocolEndpointMutationModel(change.Endpoint)
		if before == nil {
			var last model.ProtocolEndpoint
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "sort_order").Order("sort_order desc, id desc").First(&last)
			if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			if result.Error == nil {
				row.SortOrder = last.SortOrder + 1
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			row.ID, row.SortOrder, row.CreatedAt = previous.ID, previous.SortOrder, previous.CreatedAt
			if err := validateAndMigrateProtocolEndpointCredentials(tx, previous, row); err != nil {
				return err
			}
			update := tx.Model(&model.ProtocolEndpoint{}).Where("id = ?", previous.ID).
				Select("node_id", "name", "runtime_key", "protocol", "address", "port", "public_port", "cipher", "parent_protocol_id", "multiplier_milli", "managed_principal_ready", "mieru_principal_ready", "server_config", "egress_protocol", "egress_config", "client_config", "optional_config", "tags", "is_active").
				Updates(&row)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return network.ErrProtocolEndpointConflict
			}
		}

		removed := removedProtocolEndpointMemberships(change.MembershipChanges)
		if !row.IsActive {
			if err := validateProtocolEndpointDeactivation(tx, row.ID, removed); err != nil {
				return err
			}
		}
		membershipMutation, err := applyProtocolEndpointMembershipChanges(tx, admin, row, change)
		if err != nil {
			return err
		}
		if err := tx.Where("protocol_endpoint_id = ?", row.ID).Delete(&model.CertificateProtocolEndpoint{}).Error; err != nil {
			return err
		}
		if change.ManagedCertificateID != 0 {
			if err := tx.Create(&model.CertificateProtocolEndpoint{ManagedCertificateID: change.ManagedCertificateID, ProtocolEndpointID: row.ID}).Error; err != nil {
				return err
			}
		}

		membershipNodes := []uint(nil)
		if membershipMutation != nil {
			membershipNodes = membershipMutation.AffectedNodeIDs
		}
		for _, nodeID := range network.DirectProtocolEndpointPublishNodeIDs(change.Effects.AffectedNodeIDs, membershipNodes) {
			if err := EnqueueNodePublication(tx, nodeID, row.ID, admin.ID); err != nil {
				return err
			}
		}
		action := "protocol_endpoint.create"
		if before != nil {
			action = "protocol_endpoint.update"
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: action, Target: fmt.Sprintf("protocol_endpoint:%d", row.ID), Detail: protocolEndpointMutationAuditDetail(row, previous, change, membershipMutation)}).Error; err != nil {
			return err
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		memberships, err := loadProtocolEndpointMutationMemberships(tx, row.ID)
		if err != nil {
			return err
		}
		record := protocolEndpointMutationRecord(row)
		record.ServerConfig = change.Endpoint.ServerConfig
		record.EgressConfig = change.Endpoint.EgressConfig
		out = network.ProtocolEndpointMutationResult{
			ProtocolEndpoint: record, Memberships: memberships, MembershipMutation: membershipMutation,
			ProtocolEndpointChangeEffects: change.Effects,
		}
		return nil
	})
	return
}

func protocolEndpointAdministrator(tx *gorm.DB, actor uint) error {
	if _, err := providerAdmin(tx, actor); err != nil {
		if errors.Is(err, network.ErrProviderPermission) {
			return network.ErrProtocolEndpointMutationPermission
		}
		return err
	}
	return nil
}

func protocolEndpointMutationSnapshot(row model.ProtocolEndpoint, certificateID uint) network.ProtocolEndpointMutationSnapshot {
	return network.ProtocolEndpointMutationSnapshot{Endpoint: protocolEndpointMutationRecord(row), ManagedCertificateID: certificateID, Version: protocolEndpointSnapshotVersion(row, certificateID)}
}

func protocolEndpointMutationRecord(row model.ProtocolEndpoint) network.ProtocolEndpointRecord {
	return network.ProtocolEndpointRecord{
		ID: row.ID, NodeID: row.NodeID, Name: row.Name, RuntimeKey: row.RuntimeKey,
		Protocol: row.Protocol, Address: row.Address, Port: row.Port, PublicPort: row.PublicPort,
		Cipher: row.Cipher, ParentProtocolID: row.ParentProtocolID, MultiplierMilli: row.MultiplierMilli,
		ManagedPrincipalReady: row.ManagedPrincipalReady, MieruPrincipalReady: row.MieruPrincipalReady,
		ServerCiphertext: row.ServerConfig, EgressProtocol: row.EgressProtocol, EgressCiphertext: row.EgressConfig,
		ClientConfig: row.ClientConfig, OptionalConfig: row.OptionalConfig,
		Tags: row.Tags, IsActive: row.IsActive, SortOrder: row.SortOrder, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func protocolEndpointMutationModel(row network.ProtocolEndpointRecord) model.ProtocolEndpoint {
	return model.ProtocolEndpoint{
		ID: row.ID, NodeID: row.NodeID, Name: row.Name, RuntimeKey: row.RuntimeKey,
		Protocol: row.Protocol, Address: row.Address, Port: row.Port, PublicPort: row.PublicPort,
		Cipher: row.Cipher, ParentProtocolID: row.ParentProtocolID, MultiplierMilli: row.MultiplierMilli,
		ManagedPrincipalReady: row.ManagedPrincipalReady, MieruPrincipalReady: row.MieruPrincipalReady,
		ServerConfig: row.ServerCiphertext, EgressProtocol: row.EgressProtocol, EgressConfig: row.EgressCiphertext,
		ClientConfig: row.ClientConfig, OptionalConfig: row.OptionalConfig,
		Tags: row.Tags, IsActive: row.IsActive, SortOrder: row.SortOrder, CreatedAt: row.CreatedAt,
	}
}

func protocolEndpointSnapshotVersion(row model.ProtocolEndpoint, certificateID uint) string {
	payload, _ := json.Marshal(struct {
		Endpoint      model.ProtocolEndpoint
		CertificateID uint
	}{row, certificateID})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func protocolEndpointCertificateID(tx *gorm.DB, endpointID uint) (uint, error) {
	var link model.CertificateProtocolEndpoint
	err := tx.Where("protocol_endpoint_id = ?", endpointID).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return link.ManagedCertificateID, err
}

func lockAvailableProtocolEndpointNodes(tx *gorm.DB, ids []uint) error {
	ids = uniqueSortedIDs(ids)
	var nodes []model.Node
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&nodes).Error; err != nil {
		return err
	}
	if len(nodes) != len(ids) {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"node_id": "所选承载节点不存在。"}}
	}
	for _, node := range nodes {
		if node.LifecycleStatus == "deleting" {
			return network.ErrProtocolEndpointResourceDeleting
		}
	}
	return nil
}

func validateProtocolEndpointTopology(tx *gorm.DB, endpoint network.ProtocolEndpointRecord) error {
	if endpoint.ID != 0 {
		query := tx.Model(&model.NetworkEntry{}).Where("endpoint_id = ?", endpoint.ID)
		if strings.EqualFold(endpoint.Protocol, "hysteria2") {
			query = query.Where("node_id = ? OR network = ?", endpoint.NodeID, "tcp")
		} else {
			query = query.Where("node_id = ?", endpoint.NodeID)
		}
		var invalid int64
		if err := query.Count(&invalid).Error; err != nil {
			return err
		}
		if invalid > 0 {
			return &network.ProtocolEndpointMutationValidation{Message: "协议服务变更与网络前置冲突。", Fields: map[string]string{"node_id": "请先调整前置入口：入口和落地必须是不同节点，Hysteria2 必须使用 TCP/UDP 转发。"}}
		}
	}
	var entryPorts int64
	if err := tx.Model(&model.NetworkEntry{}).Where("node_id = ? AND port = ?", endpoint.NodeID, endpoint.Port).Count(&entryPorts).Error; err != nil {
		return err
	}
	if entryPorts > 0 {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"port": "该端口已被网络前置入口占用。"}}
	}
	return nil
}

func validateProtocolEndpointParent(tx *gorm.DB, endpointID, nodeID uint, parentID *uint) error {
	if parentID == nil || *parentID == 0 {
		return nil
	}
	if endpointID != 0 && *parentID == endpointID {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"parent_protocol_id": "协议不能将自身设为父协议。"}}
	}
	visited := map[uint]bool{endpointID: endpointID != 0}
	current := *parentID
	for depth := 0; depth < 128 && current != 0; depth++ {
		if visited[current] {
			return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"parent_protocol_id": "父协议关系不能形成循环。"}}
		}
		visited[current] = true
		var parent model.ProtocolEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "node_id", "parent_protocol_id").First(&parent, current).Error; err != nil {
			return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"parent_protocol_id": "所选父协议不存在。"}}
		}
		if parent.NodeID != nodeID {
			return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"parent_protocol_id": "父协议必须与当前服务属于同一节点。"}}
		}
		if parent.ParentProtocolID == nil {
			return nil
		}
		current = *parent.ParentProtocolID
	}
	if current != 0 {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"parent_protocol_id": "父协议层级过深。"}}
	}
	return nil
}

func validateProtocolEndpointCertificate(tx *gorm.DB, id, nodeID uint, protocol string, now time.Time) error {
	if id == 0 {
		return nil
	}
	supported := strings.EqualFold(protocol, "vless") || strings.EqualFold(protocol, "vmess") || strings.EqualFold(protocol, "trojan") || strings.EqualFold(protocol, "hysteria2")
	if !supported {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"managed_certificate_id": "当前协议不使用 TLS 证书。"}}
	}
	var certificate model.ManagedCertificate
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&certificate, id).Error; err != nil {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"managed_certificate_id": "所选证书不存在。"}}
	}
	if certificate.Status == "deleting" {
		return network.ErrProtocolEndpointResourceDeleting
	}
	if certificate.NodeID != nodeID {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"managed_certificate_id": "证书与协议服务必须属于同一节点。"}}
	}
	if certificate.NotAfter == nil || !certificate.NotAfter.After(now) || certificate.Status != "active" && certificate.Status != "failed" {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"managed_certificate_id": "证书尚未成功签发或已经过期。"}}
	}
	return nil
}

func validateAndMigrateProtocolEndpointCredentials(tx *gorm.DB, previous, next model.ProtocolEndpoint) error {
	var credentials []model.ProtocolCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("protocol_endpoint_id = ?", next.ID).Order("id").Find(&credentials).Error; err != nil {
		return err
	}
	if len(credentials) == 0 {
		return nil
	}
	if !strings.EqualFold(previous.Protocol, next.Protocol) || strings.EqualFold(previous.Protocol, "shadowsocks") && (previous.Port != next.Port || previous.PublicPort != next.PublicPort) {
		return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"protocol": "该服务已有订阅凭证；请创建新服务后迁移，不能直接更换协议或 Shadowsocks 端口。"}}
	}
	return tx.Model(&model.ProtocolCredential{}).Where("protocol_endpoint_id = ?", next.ID).Updates(map[string]any{"node_id": next.NodeID, "listen_port": next.Port, "public_port": next.PublicPort}).Error
}

func removedProtocolEndpointMemberships(changes []network.ProtocolEndpointMembershipChange) map[uint]bool {
	result := map[uint]bool{}
	for _, change := range changes {
		if !change.Member {
			result[change.NodeGroupID] = true
		}
	}
	return result
}

func validateProtocolEndpointDeactivation(tx *gorm.DB, endpointID uint, removed map[uint]bool) error {
	var memberships []struct {
		NodeGroupID uint
		Name        string
		IsEnabled   bool
	}
	query := tx.Table("node_group_endpoints AS membership").Select("membership.node_group_id, node_groups.name, node_groups.is_enabled").Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").Where("membership.protocol_endpoint_id = ?", endpointID)
	if len(removed) > 0 {
		ids := make([]uint, 0, len(removed))
		for id := range removed {
			ids = append(ids, id)
		}
		query = query.Where("membership.node_group_id NOT IN ?", ids)
	}
	if err := query.Scan(&memberships).Error; err != nil {
		return err
	}
	for _, membership := range memberships {
		var otherActive int64
		if err := tx.Table("node_group_endpoints").Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.id <> ? AND protocol_endpoints.is_active = ?", membership.NodeGroupID, endpointID, true).Count(&otherActive).Error; err != nil {
			return err
		}
		if otherActive > 0 {
			continue
		}
		var activePlans int64
		if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", membership.NodeGroupID, true).Count(&activePlans).Error; err != nil {
			return err
		}
		if membership.IsEnabled || activePlans > 0 {
			return &network.ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: map[string]string{"is_active": fmt.Sprintf("节点组“%s”仍依赖此服务；请同时移除该关联或先加入其他可用服务。", membership.Name)}}
		}
	}
	return nil
}

func applyProtocolEndpointMembershipChanges(tx *gorm.DB, admin model.User, endpoint model.ProtocolEndpoint, change network.ProtocolEndpointMutationChange) (*network.ProtocolEndpointMembershipMutation, error) {
	if len(change.MembershipChanges) == 0 {
		return nil, nil
	}
	groupIDs := make([]uint, 0, len(change.MembershipChanges))
	for _, item := range change.MembershipChanges {
		groupIDs = append(groupIDs, item.NodeGroupID)
	}
	var groups []model.NodeGroup
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", groupIDs).Order("id").Find(&groups).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]*model.NodeGroup, len(groups))
	for index := range groups {
		byID[groups[index].ID] = &groups[index]
	}
	for _, id := range groupIDs {
		if byID[id] == nil {
			return nil, &network.ProtocolEndpointMutationValidation{Message: "节点组关联校验失败。", Fields: map[string]string{"node_group_membership_changes": fmt.Sprintf("节点组 #%d 不存在，请重新选择。", id)}}
		}
	}
	conflicts := make([]network.ProtocolEndpointMembershipConflict, 0)
	for _, item := range change.MembershipChanges {
		group := byID[item.NodeGroupID]
		if group.Revision != item.ExpectedRevision {
			conflicts = append(conflicts, network.ProtocolEndpointMembershipConflict{NodeGroupID: group.ID, ExpectedRevision: item.ExpectedRevision, CurrentRevision: group.Revision})
		}
	}
	if len(conflicts) > 0 {
		return nil, &network.ProtocolEndpointMembershipConflictError{Conflicts: conflicts}
	}
	var links []model.NodeGroupEndpoint
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("protocol_endpoint_id = ? AND node_group_id IN ?", endpoint.ID, groupIDs).Find(&links).Error; err != nil {
		return nil, err
	}
	linkByGroup := make(map[uint]model.NodeGroupEndpoint, len(links))
	for _, link := range links {
		linkByGroup[link.NodeGroupID] = link
	}
	result := &network.ProtocolEndpointMembershipMutation{PublishStatus: network.ProtocolEndpointPublishNotRequired}
	affected := map[uint]bool{}
	for _, item := range change.MembershipChanges {
		group := byID[item.NodeGroupID]
		link, exists := linkByGroup[group.ID]
		if item.Member == exists {
			continue
		}
		if item.Member {
			if !endpoint.IsActive {
				return nil, &network.ProtocolEndpointMutationValidation{Message: "节点组关联校验失败。", Fields: map[string]string{"node_group_membership_changes": "只能将已启用的协议服务加入节点组。"}}
			}
			var maxSort int
			if err := tx.Model(&model.NodeGroupEndpoint{}).Select("COALESCE(MAX(sort_order), -1)").Where("node_group_id = ?", group.ID).Row().Scan(&maxSort); err != nil {
				return nil, err
			}
			if err := tx.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID, SortOrder: maxSort + 1}).Error; err != nil {
				return nil, err
			}
			result.AddedNodeGroupIDs = append(result.AddedNodeGroupIDs, group.ID)
		} else {
			if err := tx.Delete(&model.NodeGroupEndpoint{}, link.ID).Error; err != nil {
				return nil, err
			}
			result.RemovedNodeGroupIDs = append(result.RemovedNodeGroupIDs, group.ID)
		}
		if err := validateProtocolEndpointGroupAvailability(tx, *group); err != nil {
			return nil, err
		}
		group.Revision++
		update := tx.Model(&model.NodeGroup{}).Where("id = ? AND revision = ?", group.ID, item.ExpectedRevision).Update("revision", group.Revision)
		if update.Error != nil {
			return nil, update.Error
		}
		if update.RowsAffected != 1 {
			return nil, network.ErrProtocolEndpointConflict
		}
		receipt, err := persistNodeGroupMutationReconcileTask(tx, admin, *group, []uint{endpoint.ID}, change.CredentialProtocols, change.Now)
		if err != nil {
			return nil, err
		}
		result.ReconcileTasks = append(result.ReconcileTasks, receipt.BatchReceipt)
		for _, nodeID := range protocolEndpointReconcileNodeIDs(receipt.BatchReceipt) {
			affected[nodeID] = true
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "node_group.membership.update", Target: fmt.Sprintf("node_group:%d", group.ID), Detail: fmt.Sprintf("endpoint=%d member=%t revision=%d", endpoint.ID, item.Member, group.Revision)}).Error; err != nil {
			return nil, err
		}
	}
	if len(result.AddedNodeGroupIDs) == 0 && len(result.RemovedNodeGroupIDs) == 0 {
		return nil, nil
	}
	for nodeID := range affected {
		result.AffectedNodeIDs = append(result.AffectedNodeIDs, nodeID)
	}
	sort.Slice(result.AffectedNodeIDs, func(i, j int) bool { return result.AffectedNodeIDs[i] < result.AffectedNodeIDs[j] })
	if len(result.AffectedNodeIDs) > 0 {
		result.PublishStatus = network.ProtocolEndpointPublishQueued
	}
	return result, nil
}

func validateProtocolEndpointGroupAvailability(tx *gorm.DB, group model.NodeGroup) error {
	err := validateNodeGroupMutationAvailability(tx, group)
	var validation *network.NodeGroupMutationValidation
	if errors.As(err, &validation) {
		return &network.ProtocolEndpointMutationValidation{Message: validation.Message, Fields: validation.Fields}
	}
	return err
}

func protocolEndpointReconcileNodeIDs(receipt jobs.BatchReceipt) []uint {
	var content network.BatchOperationContent
	if json.Unmarshal([]byte(receipt.Content), &content) != nil {
		return nil
	}
	ids := make([]uint, 0, len(content.EndpointIDsByNode))
	for raw := range content.EndpointIDsByNode {
		var id uint
		if _, err := fmt.Sscanf(raw, "%d", &id); err == nil && id != 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func loadProtocolEndpointMutationMemberships(tx *gorm.DB, endpointID uint) ([]network.ProtocolEndpointMembership, error) {
	result := make([]network.ProtocolEndpointMembership, 0)
	err := tx.Table("node_group_endpoints AS membership").Select("membership.node_group_id, node_groups.name, node_groups.code, node_groups.description, node_groups.is_enabled, node_groups.revision, membership.sort_order").Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").Where("membership.protocol_endpoint_id = ?", endpointID).Order("node_groups.name, node_groups.id").Scan(&result).Error
	return result, err
}

func protocolEndpointMutationAuditDetail(row, previous model.ProtocolEndpoint, change network.ProtocolEndpointMutationChange, membership *network.ProtocolEndpointMembershipMutation) string {
	detail := fmt.Sprintf("node=%d protocol=%s multiplier_milli=%d", row.NodeID, row.Protocol, row.MultiplierMilli)
	if change.ManagedCertificateID != 0 {
		detail += fmt.Sprintf(" managed_certificate=%d", change.ManagedCertificateID)
	}
	if previous.NodeID != 0 && previous.NodeID != row.NodeID {
		detail += fmt.Sprintf(" previous_node=%d", previous.NodeID)
	}
	detail += fmt.Sprintf(" effect=%s publish_status=%s", change.Effects.Effect, change.Effects.PublishStatus)
	if membership != nil {
		detail += fmt.Sprintf(" node_groups_added=%d node_groups_removed=%d membership_publish_status=%s", len(membership.AddedNodeGroupIDs), len(membership.RemovedNodeGroupIDs), membership.PublishStatus)
	}
	return detail
}
