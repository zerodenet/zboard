package networkstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type Inventory struct{ DB *gorm.DB }

func nodeKernelRecord(row model.NodeKernelState) network.NodeKernelRecord {
	return network.NodeKernelRecord{NodeID: row.NodeID, Status: row.Status, Phase: row.Phase, RecommendedAction: row.RecommendedAction,
		PlatformOS: row.PlatformOS, Architecture: row.Architecture, Libc: row.Libc, DesiredVersion: row.DesiredVersion, InstalledVersion: row.InstalledVersion,
		DesiredSHA256: row.DesiredSHA256, InstalledSHA256: row.InstalledSHA256, DesiredConfigSHA256: row.DesiredConfigSHA256, AppliedConfigSHA256: row.AppliedConfigSHA256,
		ServiceStatus: row.ServiceStatus, ControlStatus: row.ControlStatus, LastError: row.LastError, ActiveOperationID: row.ActiveOperationID,
		LastDetectedAt: row.LastDetectedAt, LastHealthyAt: row.LastHealthyAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s Inventory) ListNodes(ctx context.Context, input network.NodeInventoryQuery) (network.NodeInventoryPage, error) {
	query := s.DB.WithContext(ctx).Model(&model.Node{})
	if input.ID != 0 {
		query = query.Where("nodes.id = ?", input.ID)
	}
	if input.Search != "" {
		p := "%" + input.Search + "%"
		query = query.Where("LOWER(nodes.name) LIKE ? OR LOWER(nodes.address) LIKE ? OR LOWER(nodes.region) LIKE ?", p, p, p)
	}
	if input.Region != "" {
		query = query.Where("nodes.region = ?", input.Region)
	}
	if input.LifecycleStatus != "" {
		query = query.Where("nodes.lifecycle_status = ?", input.LifecycleStatus)
	}
	if input.Enabled != nil {
		query = query.Where("nodes.is_enabled = ?", *input.Enabled)
	}
	if input.ConnectorOnline != nil {
		if *input.ConnectorOnline {
			query = query.Where("nodes.connector_last_seen_at >= ?", input.OnlineCutoff)
		} else {
			query = query.Where("nodes.connector_last_seen_at IS NULL OR nodes.connector_last_seen_at < ?", input.OnlineCutoff)
		}
	}
	if input.KernelStatus != "" {
		query = query.Joins("JOIN node_kernel_states ON node_kernel_states.node_id = nodes.id").Where("node_kernel_states.status = ?", input.KernelStatus)
	}
	page := network.NodeInventoryPage{}
	if input.Paged {
		if err := query.Count(&page.Total).Error; err != nil {
			return page, err
		}
	}
	sortColumn := map[string]string{"id": "nodes.id", "name": "nodes.name", "region": "nodes.region", "updated_at": "nodes.updated_at", "last_seen_at": "nodes.connector_last_seen_at"}[input.Sort]
	if sortColumn == "" {
		sortColumn = "nodes.id"
	}
	direction := "desc"
	if input.Direction == "asc" {
		direction = "asc"
	}
	query = query.Order(sortColumn + " " + direction)
	if input.Paged {
		query = query.Offset(input.Offset).Limit(input.Limit)
	}
	var rows []model.Node
	if err := query.Find(&rows).Error; err != nil {
		return page, err
	}
	if !input.Paged {
		page.Total = int64(len(rows))
	}
	var err error
	page.Items, err = s.decorateNodes(ctx, rows, input.Paged)
	if err != nil {
		return page, err
	}
	return page, nil
}

func (s Inventory) decorateNodes(ctx context.Context, rows []model.Node, counts bool) ([]network.NodeInventoryItem, error) {
	items := make([]network.NodeInventoryItem, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	var states []model.NodeKernelState
	if err := s.DB.WithContext(ctx).Where("node_id IN ?", ids).Find(&states).Error; err != nil {
		return nil, err
	}
	stateByID := map[uint]network.NodeKernelRecord{}
	for _, r := range states {
		stateByID[r.NodeID] = nodeKernelRecord(r)
	}
	countByID := map[uint]int64{}
	if counts {
		type countRow struct {
			NodeID uint
			Count  int64
		}
		var endpointValues []countRow
		if err := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{}).Select("node_id, COUNT(*) AS count").Where("node_id IN ? AND is_active = ?", ids, true).Group("node_id").Scan(&endpointValues).Error; err != nil {
			return nil, err
		}
		for _, r := range endpointValues {
			countByID[r.NodeID] += r.Count
		}
		var entryValues []countRow
		if err := s.DB.WithContext(ctx).Model(&model.NetworkEntry{}).Select("node_id, COUNT(*) AS count").Where("node_id IN ? AND enabled = ?", ids, true).Group("node_id").Scan(&entryValues).Error; err != nil {
			return nil, err
		}
		for _, r := range entryValues {
			countByID[r.NodeID] += r.Count
		}
	}
	for _, r := range rows {
		item := network.NodeInventoryItem{Node: nodeAdministrationRecord(r), EnabledProtocolCount: countByID[r.ID]}
		if state, ok := stateByID[r.ID]; ok {
			copy := state
			item.KernelState = &copy
		}
		items = append(items, item)
	}
	return items, nil
}

func (s Inventory) Node(ctx context.Context, id uint) (network.NodeInventoryItem, error) {
	var row model.Node
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		return network.NodeInventoryItem{}, inventoryError(err)
	}
	items, err := s.decorateNodes(ctx, []model.Node{row}, true)
	if err != nil {
		return network.NodeInventoryItem{}, err
	}
	return items[0], nil
}
func (s Inventory) RuntimeNode(ctx context.Context, id uint) (network.NodeRuntimeRecord, error) {
	var row model.Node
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		return network.NodeRuntimeRecord{}, inventoryError(err)
	}
	return runtimeNodeRecord(row), nil
}

func (s Inventory) RuntimeNodes(ctx context.Context, ids []uint) (map[uint]network.NodeRuntimeRecord, error) {
	result := make(map[uint]network.NodeRuntimeRecord, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var rows []model.Node
	if err := s.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = runtimeNodeRecord(row)
	}
	return result, nil
}

func (s Inventory) Endpoint(ctx context.Context, id uint) (network.ProtocolEndpointRecord, error) {
	var row model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		return network.ProtocolEndpointRecord{}, inventoryError(err)
	}
	return protocolEndpointMutationRecord(row), nil
}

func (s Inventory) NodeDiagnosticEndpoints(ctx context.Context, nodeID uint) ([]network.NodeDiagnosticEndpoint, error) {
	var rows []network.NodeDiagnosticEndpoint
	err := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{}).
		Select("id, node_id, name, protocol, port").Where("node_id = ? AND is_active = ?", nodeID, true).
		Order("sort_order asc, id asc").Scan(&rows).Error
	return rows, err
}

func (s Inventory) AuthorizedProtocolEndpoints(ctx context.Context, userID uint, now time.Time) ([]network.ProtocolEndpointRecord, error) {
	var rows []model.ProtocolEndpoint
	err := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{}).
		Select("DISTINCT protocol_endpoints.*").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Joins("JOIN subscriptions ON subscriptions.node_group_id = node_group_endpoints.node_group_id").
		Joins("JOIN nodes ON nodes.id = protocol_endpoints.node_id").
		Where("subscriptions.user_id = ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", userID, "active", now).
		Where("protocol_endpoints.is_active = ? AND nodes.is_enabled = ? AND nodes.last_seen_at >= ?", true, true, now.Add(-2*time.Minute)).
		Order("protocol_endpoints.sort_order asc, protocol_endpoints.id asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]network.ProtocolEndpointRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, protocolEndpointMutationRecord(row))
	}
	return out, nil
}

func (s Inventory) SubscriptionDeliveryRelations(ctx context.Context, groupIDs []uint) ([]network.SubscriptionDeliveryRelation, error) {
	relations := make([]network.SubscriptionDeliveryRelation, 0)
	if err := s.DB.WithContext(ctx).Table("node_group_endpoints").
		Select("node_group_endpoints.node_group_id, node_group_endpoints.protocol_endpoint_id, node_group_endpoints.sort_order AS group_sort_order, protocol_endpoints.sort_order AS global_sort_order").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("node_group_endpoints.node_group_id IN ?", groupIDs).Scan(&relations).Error; err != nil {
		return nil, err
	}
	entries := make([]network.SubscriptionDeliveryRelation, 0)
	if err := s.DB.WithContext(ctx).Table("node_group_network_entries membership").
		Select("membership.node_group_id, network_entries.endpoint_id AS protocol_endpoint_id, network_entries.id AS network_entry_id, membership.sort_order AS group_sort_order, COALESCE(network_entries.delivery_sort_order, protocol_endpoints.sort_order) AS global_sort_order").
		Joins("JOIN network_entries ON network_entries.id = membership.network_entry_id").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = network_entries.endpoint_id").
		Where("membership.node_group_id IN ?", groupIDs).Scan(&entries).Error; err != nil {
		return nil, err
	}
	return append(relations, entries...), nil
}

func runtimeNodeRecord(row model.Node) network.NodeRuntimeRecord {
	snapshot := nodeAdministrationSnapshot(row)
	return network.NodeRuntimeRecord{Node: snapshot.Node, NodeCredentialCiphertext: snapshot.NodeCredentialCiphertext, SSHPwdCiphertext: snapshot.SSHPwdCiphertext, SSHPrivateKeyPassphraseCiphertext: snapshot.SSHPassphraseCiphertext, SSHPrivilegePasswordCiphertext: snapshot.SSHPrivilegeCiphertext, TrafficSecretCiphertext: snapshot.TrafficSecretCiphertext}
}

func (s Inventory) endpointQuery(ctx context.Context, input network.ProtocolEndpointInventoryQuery) *gorm.DB {
	q := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{})
	if len(input.IDs) > 0 {
		q = q.Where("protocol_endpoints.id IN ?", input.IDs)
	}
	if input.NodeID != 0 {
		q = q.Where("protocol_endpoints.node_id = ?", input.NodeID)
	}
	if input.Search != "" {
		p := "%" + input.Search + "%"
		q = q.Where("LOWER(protocol_endpoints.name) LIKE ? OR LOWER(protocol_endpoints.address) LIKE ?", p, p)
	}
	if input.Protocol != "" {
		q = q.Where("protocol_endpoints.protocol = ?", input.Protocol)
	}
	if input.Active != nil {
		q = q.Where("protocol_endpoints.is_active = ?", *input.Active)
	}
	if input.DeploymentStatus != "" {
		latest := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("MAX(id)").Group("protocol_endpoint_id")
		if input.DeploymentStatus == "never" {
			deployed := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("DISTINCT protocol_endpoint_id")
			q = q.Where("protocol_endpoints.id NOT IN (?)", deployed)
		} else {
			matching := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("protocol_endpoint_id").Where("id IN (?) AND status = ?", latest, input.DeploymentStatus)
			q = q.Where("protocol_endpoints.id IN (?)", matching)
		}
	}
	return q
}

func (s Inventory) ListProtocolEndpoints(ctx context.Context, input network.ProtocolEndpointInventoryQuery) (network.ProtocolEndpointInventoryPage, error) {
	q := s.endpointQuery(ctx, input)
	page := network.ProtocolEndpointInventoryPage{}
	if input.Paged && input.IncludeStatusFacets {
		facets, err := s.protocolEndpointStatusFacets(ctx, input)
		if err != nil {
			return page, err
		}
		page.Facets = facets
		switch input.DeploymentStatus {
		case "succeeded":
			page.Total = facets.Succeeded
		case "running":
			page.Total = facets.Running
		case "failed":
			page.Total = facets.Failed
		case "never":
			page.Total = facets.Never
		case "":
			page.Total = facets.All
		default:
			if err := q.Count(&page.Total).Error; err != nil {
				return page, err
			}
		}
	} else if err := q.Count(&page.Total).Error; err != nil {
		return page, err
	}
	sortColumn := map[string]string{"sort_order": "protocol_endpoints.sort_order", "id": "protocol_endpoints.id", "name": "protocol_endpoints.name", "protocol": "protocol_endpoints.protocol", "node_id": "protocol_endpoints.node_id", "multiplier": "protocol_endpoints.multiplier_milli", "updated_at": "protocol_endpoints.updated_at"}[input.Sort]
	if sortColumn == "" {
		sortColumn = "protocol_endpoints.sort_order"
	}
	direction := "asc"
	if input.Direction == "desc" {
		direction = "desc"
	}
	q = q.Order(sortColumn + " " + direction + ", protocol_endpoints.id asc")
	if input.Paged {
		q = q.Offset(input.Offset).Limit(input.Limit)
	}
	var rows []model.ProtocolEndpoint
	if err := q.Find(&rows).Error; err != nil {
		return page, err
	}
	items, err := s.decorateEndpoints(ctx, rows, input.Now, false)
	page.Items = items
	return page, err
}

func (s Inventory) protocolEndpointStatusFacets(ctx context.Context, input network.ProtocolEndpointInventoryQuery) (network.ProtocolEndpointStatusFacets, error) {
	input.DeploymentStatus = ""
	latest := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).
		Select("protocol_endpoint_id, MAX(id) AS latest_id").
		Group("protocol_endpoint_id")
	var row struct {
		Total, Succeeded, Running, Failed, Never int64
	}
	err := s.endpointQuery(ctx, input).
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN protocol_endpoint_deployment.status = 'succeeded' THEN 1 ELSE 0 END), 0) AS succeeded,
			COALESCE(SUM(CASE WHEN protocol_endpoint_deployment.status = 'running' THEN 1 ELSE 0 END), 0) AS running,
			COALESCE(SUM(CASE WHEN protocol_endpoint_deployment.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed,
			COALESCE(SUM(CASE WHEN protocol_endpoint_deployment.id IS NULL THEN 1 ELSE 0 END), 0) AS never`).
		Joins("LEFT JOIN (?) AS protocol_endpoint_latest ON protocol_endpoint_latest.protocol_endpoint_id = protocol_endpoints.id", latest).
		Joins("LEFT JOIN protocol_deployments AS protocol_endpoint_deployment ON protocol_endpoint_deployment.id = protocol_endpoint_latest.latest_id").
		Scan(&row).Error
	if err != nil {
		return network.ProtocolEndpointStatusFacets{}, err
	}
	return network.ProtocolEndpointStatusFacets{
		All: row.Total, Succeeded: row.Succeeded, Running: row.Running, Failed: row.Failed, Never: row.Never,
	}, nil
}

func (s Inventory) ProtocolEndpoint(ctx context.Context, id uint, now time.Time) (network.ProtocolEndpointInventoryItem, error) {
	var row model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		return network.ProtocolEndpointInventoryItem{}, inventoryError(err)
	}
	items, err := s.decorateEndpoints(ctx, []model.ProtocolEndpoint{row}, now, true)
	if err != nil {
		return network.ProtocolEndpointInventoryItem{}, err
	}
	return items[0], nil
}

func (s Inventory) decorateEndpoints(ctx context.Context, rows []model.ProtocolEndpoint, now time.Time, detail bool) ([]network.ProtocolEndpointInventoryItem, error) {
	items := make([]network.ProtocolEndpointInventoryItem, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	ids, nodeIDs := make([]uint, 0, len(rows)), make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		nodeIDs = append(nodeIDs, r.NodeID)
	}
	var usage map[uint]network.ProtocolUsageRecord
	var latest map[uint]network.ProtocolDeploymentRecord
	var nodes []model.Node
	var bindings []model.CertificateProtocolEndpoint
	var usageErr, latestErr, nodesErr, bindingsErr error
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		usage, usageErr = s.protocolUsage(ctx, ids, now)
	}()
	go func() {
		defer wg.Done()
		latest, latestErr = s.latestDeployments(ctx, ids)
	}()
	go func() {
		defer wg.Done()
		nodesErr = s.DB.WithContext(ctx).Where("id IN ?", nodeIDs).Find(&nodes).Error
	}()
	go func() {
		defer wg.Done()
		bindingsErr = s.DB.WithContext(ctx).Where("protocol_endpoint_id IN ?", ids).Find(&bindings).Error
	}()
	wg.Wait()
	for _, err := range []error{usageErr, latestErr, nodesErr, bindingsErr} {
		if err != nil {
			return nil, err
		}
	}
	nodeByID := map[uint]network.NodeAdministrationRecord{}
	for _, r := range nodes {
		nodeByID[r.ID] = nodeAdministrationRecord(r)
	}
	certificateByEndpoint := map[uint]uint{}
	for _, r := range bindings {
		certificateByEndpoint[r.ProtocolEndpointID] = r.ManagedCertificateID
	}
	membershipByEndpoint := map[uint][]network.ProtocolEndpointMembership{}
	if detail {
		var links []model.NodeGroupEndpoint
		if err := s.DB.WithContext(ctx).Where("protocol_endpoint_id IN ?", ids).Order("sort_order asc, id asc").Find(&links).Error; err != nil {
			return nil, err
		}
		groupIDs := make([]uint, 0, len(links))
		for _, r := range links {
			groupIDs = append(groupIDs, r.NodeGroupID)
		}
		var groups []model.NodeGroup
		if len(groupIDs) > 0 {
			if err := s.DB.WithContext(ctx).Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
				return nil, err
			}
		}
		groupByID := map[uint]model.NodeGroup{}
		for _, r := range groups {
			groupByID[r.ID] = r
		}
		for _, r := range links {
			g := groupByID[r.NodeGroupID]
			membershipByEndpoint[r.ProtocolEndpointID] = append(membershipByEndpoint[r.ProtocolEndpointID], network.ProtocolEndpointMembership{NodeGroupID: g.ID, Name: g.Name, Code: g.Code, Description: g.Description, IsEnabled: g.IsEnabled, Revision: g.Revision, SortOrder: r.SortOrder})
		}
	}
	for _, r := range rows {
		item := network.ProtocolEndpointInventoryItem{Endpoint: protocolEndpointMutationRecord(r), Node: nodeByID[r.NodeID], Usage: usage[r.ID], Memberships: membershipByEndpoint[r.ID]}
		if id, ok := certificateByEndpoint[r.ID]; ok {
			copy := id
			item.ManagedCertificateID = &copy
		}
		if dep, ok := latest[r.ID]; ok {
			copy := dep
			item.LatestDeployment = &copy
		}
		items = append(items, item)
	}
	return items, nil
}

func (s Inventory) protocolUsage(ctx context.Context, ids []uint, now time.Time) (map[uint]network.ProtocolUsageRecord, error) {
	result := map[uint]network.ProtocolUsageRecord{}
	for _, id := range ids {
		result[id] = network.ProtocolUsageRecord{}
	}
	type principalRow struct {
		ProtocolEndpointID                         uint
		ActiveFlows, ActiveUsers, ObservationCount int64
		LastObservedAt                             *time.Time
	}
	var principal []principalRow
	type credentialRow struct {
		ProtocolEndpointID uint
		ActiveCredentials  int64
		LastUsedAt         *time.Time
	}
	var credentials []credentialRow
	type trafficRow struct {
		ProtocolEndpointID             uint
		UsedBytesToday, UsedBytesTotal int64
	}
	var traffic []trafficRow
	day := now.UTC().Format("2006-01-02")
	var principalErr, credentialErr, trafficErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		principalErr = s.DB.WithContext(ctx).Table("principal_flow_currents").Select("protocol_endpoint_id, COALESCE(SUM(active_flows),0) AS active_flows, COUNT(DISTINCT CASE WHEN active_flows > 0 AND user_id > 0 THEN user_id END) AS active_users, COUNT(*) AS observation_count, MAX(observed_at) AS last_observed_at").Where("protocol_endpoint_id IN ?", ids).Group("protocol_endpoint_id").Scan(&principal).Error
	}()
	go func() {
		defer wg.Done()
		credentialErr = s.DB.WithContext(ctx).Model(&model.ProtocolCredential{}).Select("protocol_endpoint_id, SUM(CASE WHEN status = ? AND revoked_at IS NULL AND expires_at > ? THEN 1 ELSE 0 END) AS active_credentials, MAX(last_used_at) AS last_used_at", "active", now).Where("protocol_endpoint_id IN ?", ids).Group("protocol_endpoint_id").Scan(&credentials).Error
	}()
	go func() {
		defer wg.Done()
		trafficErr = s.DB.WithContext(ctx).Model(&model.ProtocolEndpointUsageDaily{}).Select("protocol_endpoint_id, COALESCE(SUM(used_bytes),0) AS used_bytes_total, COALESCE(SUM(CASE WHEN usage_date = ? THEN used_bytes ELSE 0 END),0) AS used_bytes_today", day).Where("protocol_endpoint_id IN ?", ids).Group("protocol_endpoint_id").Scan(&traffic).Error
	}()
	wg.Wait()
	for _, err := range []error{principalErr, credentialErr, trafficErr} {
		if err != nil {
			return nil, err
		}
	}
	covered := map[uint]struct{}{}
	for _, r := range principal {
		if r.ObservationCount == 0 {
			continue
		}
		covered[r.ProtocolEndpointID] = struct{}{}
		v := result[r.ProtocolEndpointID]
		v.ActiveFlows = r.ActiveFlows
		v.ActiveUsers = r.ActiveUsers
		v.LastUsedAt = r.LastObservedAt
		result[r.ProtocolEndpointID] = v
	}
	legacy := make([]uint, 0)
	for _, id := range ids {
		if _, ok := covered[id]; !ok {
			legacy = append(legacy, id)
		}
	}
	if len(legacy) > 0 {
		type row struct {
			ProtocolEndpointID       uint
			ActiveFlows, ActiveUsers int64
		}
		var values []row
		if err := s.DB.WithContext(ctx).Model(&model.FlowUsage{}).Select("flow_usages.protocol_endpoint_id, COUNT(*) AS active_flows, COUNT(DISTINCT subscriptions.user_id) AS active_users").Joins("JOIN subscriptions ON subscriptions.id = flow_usages.subscription_id").Where("flow_usages.protocol_endpoint_id IN ? AND flow_usages.status = ? AND flow_usages.last_seen_at >= ?", legacy, "active", now.Add(-2*time.Minute)).Group("flow_usages.protocol_endpoint_id").Scan(&values).Error; err != nil {
			return nil, err
		}
		for _, r := range values {
			v := result[r.ProtocolEndpointID]
			v.ActiveFlows = r.ActiveFlows
			v.ActiveUsers = r.ActiveUsers
			result[r.ProtocolEndpointID] = v
		}
	}
	for _, r := range credentials {
		v := result[r.ProtocolEndpointID]
		v.ActiveCredentials = r.ActiveCredentials
		if r.LastUsedAt != nil && (v.LastUsedAt == nil || r.LastUsedAt.After(*v.LastUsedAt)) {
			v.LastUsedAt = r.LastUsedAt
		}
		result[r.ProtocolEndpointID] = v
	}
	for _, r := range traffic {
		v := result[r.ProtocolEndpointID]
		v.UsedBytesToday = r.UsedBytesToday
		v.UsedBytesTotal = r.UsedBytesTotal
		result[r.ProtocolEndpointID] = v
	}
	return result, nil
}

func (s Inventory) ProtocolUsage(ctx context.Context, ids []uint, now time.Time) (map[uint]network.ProtocolUsageRecord, error) {
	return s.protocolUsage(ctx, ids, now)
}

func (s Inventory) latestDeployments(ctx context.Context, ids []uint) (map[uint]network.ProtocolDeploymentRecord, error) {
	result := map[uint]network.ProtocolDeploymentRecord{}
	latest := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("MAX(id)").Where("protocol_endpoint_id IN ?", ids).Group("protocol_endpoint_id")
	var rows []model.ProtocolDeployment
	if err := s.DB.WithContext(ctx).Where("id IN (?)", latest).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.ProtocolEndpointID] = deploymentRecord(r)
	}
	return result, nil
}
func deploymentRecord(r model.ProtocolDeployment) network.ProtocolDeploymentRecord {
	return network.ProtocolDeploymentRecord{ID: r.ID, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, ConfigRevision: r.ConfigRevision, DesiredConfigSHA256: r.DesiredConfigSHA256, AppliedConfigSHA256: r.AppliedConfigSHA256, Status: r.Status, RequestedBy: r.RequestedBy, Error: r.Error, Output: r.Output, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (s Inventory) SelectProtocolEndpointIDs(ctx context.Context, input network.ProtocolEndpointInventoryQuery) ([]uint, int64, error) {
	q := s.endpointQuery(ctx, input)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []uint
	if err := q.Order("protocol_endpoints.id asc").Pluck("protocol_endpoints.id", &ids).Error; err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}
func (s Inventory) ListProtocolDeployments(ctx context.Context, input network.ProtocolDeploymentQuery) (network.ProtocolDeploymentPage, error) {
	q := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{})
	if input.NodeID != 0 {
		q = q.Where("node_id = ?", input.NodeID)
	}
	if input.ProtocolEndpointID != 0 {
		q = q.Where("protocol_endpoint_id = ?", input.ProtocolEndpointID)
	}
	if input.Status != "" {
		q = q.Where("status = ?", input.Status)
	}
	page := network.ProtocolDeploymentPage{}
	if err := q.Count(&page.Total).Error; err != nil {
		return page, err
	}
	var rows []model.ProtocolDeployment
	if err := q.Order("id desc").Offset(input.Offset).Limit(input.Limit).Find(&rows).Error; err != nil {
		return page, err
	}
	for _, r := range rows {
		page.Items = append(page.Items, deploymentRecord(r))
	}
	return page, nil
}

func (s Inventory) ListNodeGroups(ctx context.Context, input network.NodeGroupInventoryQuery) (network.NodeGroupInventoryPage, error) {
	q := s.DB.WithContext(ctx).Model(&model.NodeGroup{})
	if input.ID != 0 {
		q = q.Where("id = ?", input.ID)
	}
	if input.Search != "" {
		p := "%" + input.Search + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(description) LIKE ?", p, p, p)
	}
	if input.Enabled != nil {
		q = q.Where("is_enabled = ?", *input.Enabled)
	}
	page := network.NodeGroupInventoryPage{}
	if err := q.Count(&page.Total).Error; err != nil {
		return page, err
	}
	q = q.Order("name asc, id asc")
	if input.Paged {
		q = q.Offset(input.Offset).Limit(input.Limit)
	}
	var groups []model.NodeGroup
	if err := q.Find(&groups).Error; err != nil {
		return page, err
	}
	ids := make([]uint, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	endpointCounts := map[uint]int64{}
	type countRow struct {
		NodeGroupID uint
		Count       int64
	}
	var counts []countRow
	if len(ids) > 0 {
		if err := s.DB.WithContext(ctx).Model(&model.NodeGroupEndpoint{}).Select("node_group_id, COUNT(*) AS count").Where("node_group_id IN ?", ids).Group("node_group_id").Scan(&counts).Error; err != nil {
			return page, err
		}
		for _, r := range counts {
			endpointCounts[r.NodeGroupID] = r.Count
		}
		var endpointLinks []model.NodeGroupEndpoint
		if !input.Paged {
			if err := s.DB.WithContext(ctx).Where("node_group_id IN ?", ids).Order("node_group_id asc, sort_order asc, id asc").Find(&endpointLinks).Error; err != nil {
				return page, err
			}
		}
		var entryLinks []model.NodeGroupNetworkEntry
		if err := s.DB.WithContext(ctx).Where("node_group_id IN ?", ids).Order("sort_order, id").Find(&entryLinks).Error; err != nil {
			return page, err
		}
		var plans []countRow
		if err := s.DB.WithContext(ctx).Model(&model.Plan{}).Select("node_group_id, COUNT(*) AS count").Where("node_group_id IN ?", ids).Group("node_group_id").Scan(&plans).Error; err != nil {
			return page, err
		}
		byID := map[uint]*model.NodeGroup{}
		for i := range groups {
			byID[groups[i].ID] = &groups[i]
		}
		for _, r := range endpointLinks {
			byID[r.NodeGroupID].ProtocolEndpointIDs = append(byID[r.NodeGroupID].ProtocolEndpointIDs, r.ProtocolEndpointID)
		}
		for _, r := range entryLinks {
			byID[r.NodeGroupID].NetworkEntryIDs = append(byID[r.NodeGroupID].NetworkEntryIDs, r.NetworkEntryID)
		}
		for _, r := range plans {
			byID[r.NodeGroupID].PlanCount = r.Count
		}
	}
	for _, g := range groups {
		page.Items = append(page.Items, network.NodeGroupInventoryItem{Group: nodeGroupRecord(g), ProtocolEndpointCount: endpointCounts[g.ID]})
	}
	return page, nil
}
func (s Inventory) NodeGroup(ctx context.Context, id uint) (network.NodeGroupRecord, error) {
	var g model.NodeGroup
	if err := s.DB.WithContext(ctx).First(&g, id).Error; err != nil {
		return network.NodeGroupRecord{}, inventoryError(err)
	}
	if err := s.DB.WithContext(ctx).Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", id).Order("sort_order asc, id asc").Pluck("protocol_endpoint_id", &g.ProtocolEndpointIDs).Error; err != nil {
		return network.NodeGroupRecord{}, err
	}
	if err := s.DB.WithContext(ctx).Model(&model.NodeGroupNetworkEntry{}).Where("node_group_id = ?", id).Order("sort_order asc, id asc").Pluck("network_entry_id", &g.NetworkEntryIDs).Error; err != nil {
		return network.NodeGroupRecord{}, err
	}
	if err := s.DB.WithContext(ctx).Model(&model.Plan{}).Where("node_group_id = ?", id).Count(&g.PlanCount).Error; err != nil {
		return network.NodeGroupRecord{}, err
	}
	return nodeGroupRecord(g), nil
}
func inventoryError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return network.ErrInventoryNotFound
	}
	return err
}
