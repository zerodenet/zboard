package networkstore

import (
	"context"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Page identities before decorating either kind. Large inventories never load
// every forward entry merely to merge and paginate them in the browser.
func (s Inventory) serviceQuery(ctx context.Context, input network.ProtocolEndpointInventoryQuery) *gorm.DB {
	const identities = `(SELECT 'listener' AS service_kind, endpoint.id, endpoint.id AS endpoint_id, endpoint.node_id, endpoint.name, endpoint.protocol, endpoint.address, endpoint.port, endpoint.public_port, endpoint.is_active, endpoint.multiplier_milli, endpoint.sort_order, endpoint.updated_at FROM protocol_endpoints AS endpoint
 UNION ALL SELECT 'forward' AS service_kind, entry.id, entry.endpoint_id, COALESCE(entry.node_id, endpoint.node_id) AS node_id, entry.name, endpoint.protocol, entry.address, entry.port, entry.public_port, entry.enabled AS is_active, endpoint.multiplier_milli, COALESCE(entry.delivery_sort_order,endpoint.sort_order) AS sort_order, entry.updated_at
 FROM network_entries AS entry JOIN protocol_endpoints AS endpoint ON endpoint.id = entry.endpoint_id)`
	identitySQL := identities
	var identityArgs []any
	if input.GroupID != 0 {
		// Push membership into each branch so SQLite can use the unique group
		// indexes, rather than probing both membership tables per union row.
		identitySQL = strings.Replace(identitySQL, "FROM protocol_endpoints AS endpoint", "FROM protocol_endpoints AS endpoint JOIN node_group_endpoints AS membership ON membership.protocol_endpoint_id = endpoint.id AND membership.node_group_id = ?", 1)
		identitySQL = strings.TrimSuffix(identitySQL, ")") + " JOIN node_group_network_entries AS membership ON membership.network_entry_id = entry.id AND membership.node_group_id = ?)"
		identityArgs = []any{input.GroupID, input.GroupID}
	}
	q := s.DB.WithContext(ctx).Table(identitySQL+" AS service", identityArgs...)
	if input.ServiceKind != "all" {
		q = q.Where("service.service_kind = ?", input.ServiceKind)
	}
	if len(input.IDs) > 0 {
		q = q.Where("service.id IN ?", input.IDs)
	}
	if input.NodeID != 0 {
		q = q.Where("service.node_id = ?", input.NodeID)
	}
	if input.Search != "" {
		p := "%" + input.Search + "%"
		q = q.Where("LOWER(service.name) LIKE ? OR LOWER(service.address) LIKE ?", p, p)
	}
	if input.Protocol != "" {
		q = q.Where("service.protocol = ?", input.Protocol)
	}
	if input.Active != nil {
		q = q.Where("service.is_active = ?", *input.Active)
	}

	return q
}

func (s Inventory) listProtocolServices(ctx context.Context, input network.ProtocolEndpointInventoryQuery) (network.ProtocolEndpointInventoryPage, error) {
	q := s.serviceQuery(ctx, input)
	latest := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("protocol_endpoint_id, MAX(id) AS latest_id").Group("protocol_endpoint_id")
	withDeployment := func(base *gorm.DB) *gorm.DB {
		return base.Session(&gorm.Session{}).Joins("LEFT JOIN (?) AS latest ON latest.protocol_endpoint_id = service.endpoint_id", latest).Joins("LEFT JOIN protocol_deployments AS deployment ON deployment.id = latest.latest_id")
	}
	const status = `COALESCE(deployment.status, 'never')`
	var page network.ProtocolEndpointInventoryPage
	var facets struct{ All, Succeeded, Running, Failed, Never int64 }
	if input.IncludeStatusFacets {
		if err := withDeployment(q).Select("COUNT(*) AS `all`, COALESCE(SUM(CASE WHEN " + status + " = 'succeeded' THEN 1 ELSE 0 END),0) AS succeeded, COALESCE(SUM(CASE WHEN " + status + " = 'running' THEN 1 ELSE 0 END),0) AS running, COALESCE(SUM(CASE WHEN " + status + " = 'failed' THEN 1 ELSE 0 END),0) AS failed, COALESCE(SUM(CASE WHEN " + status + " = 'never' THEN 1 ELSE 0 END),0) AS never").Scan(&facets).Error; err != nil {
			return page, err
		}
		page.Facets = network.ProtocolEndpointStatusFacets{All: facets.All, Succeeded: facets.Succeeded, Running: facets.Running, Failed: facets.Failed, Never: facets.Never}
	}
	if input.DeploymentStatus != "" {
		q = withDeployment(q).Where(status+" = ?", input.DeploymentStatus)
	}
	if input.IncludeStatusFacets {
		page.Total = map[string]int64{"": facets.All, "succeeded": facets.Succeeded, "running": facets.Running, "failed": facets.Failed, "never": facets.Never}[input.DeploymentStatus]
	} else if err := q.Session(&gorm.Session{}).Count(&page.Total).Error; err != nil {
		return page, err
	}
	column := map[string]string{"id": "id", "name": "name", "protocol": "protocol", "node_id": "node_id", "multiplier": "multiplier_milli", "updated_at": "updated_at", "sort_order": "sort_order"}[input.Sort]
	if column == "" {
		column = "sort_order"
	}
	direction := "asc"
	if input.Direction == "desc" {
		direction = "desc"
	}
	q = q.Session(&gorm.Session{}).Select("service.*").Order("service." + column + " " + direction + ", service.service_kind asc, service.id asc")
	if input.Paged {
		q = q.Offset(input.Offset).Limit(input.Limit)
	}
	var identitiesPage []struct {
		ID, EndpointID uint
		ServiceKind    string
	}
	if err := q.Scan(&identitiesPage).Error; err != nil {
		return page, err
	}
	page.Items = []network.ProtocolEndpointInventoryItem{}
	if len(identitiesPage) == 0 {
		return page, nil
	}
	var endpointIDs, entryIDs []uint
	for _, row := range identitiesPage {
		endpointIDs = append(endpointIDs, row.EndpointID)
		if row.ServiceKind == "forward" {
			entryIDs = append(entryIDs, row.ID)
		}
	}
	var endpoints []model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).Where("id IN ?", endpointIDs).Find(&endpoints).Error; err != nil {
		return page, err
	}
	decorated, err := s.decorateEndpoints(ctx, endpoints, input.Now, true)
	if err != nil {
		return page, err
	}
	byEndpoint := map[uint]network.ProtocolEndpointInventoryItem{}
	for _, item := range decorated {
		byEndpoint[item.Endpoint.ID] = item
	}
	byEntry := map[uint]network.NetworkEntryListItem{}
	if len(entryIDs) > 0 {
		entries, err := (NetworkEntryQueries{DB: s.DB}).loadEntries(ctx, entryIDs)
		if err != nil {
			return page, err
		}
		for _, entry := range entries {
			byEntry[entry.ID] = entry
		}
	}
	for _, identity := range identitiesPage {
		item := byEndpoint[identity.EndpointID]
		if identity.ServiceKind == "forward" {
			entry := byEntry[identity.ID]
			item.Forward = &entry
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}
