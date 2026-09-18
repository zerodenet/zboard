package networkstore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type NetworkEntryQueries struct {
	DB *gorm.DB
}

type networkEntryQueryRow struct {
	ID                uint
	ProxyPoolID       *uint
	DeliverySortOrder *int
	Network           string
	Name              string
	NodeID            uint
	EndpointID        uint
	Address           string
	Port              int
	PublicPort        int
	Enabled           bool
	Revision          uint64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ServiceKind       string
	ParentProtocolID  uint
	LandingNodeID     uint
	HasPath           bool
	NodeName          string
	EndpointName      string
}

type networkEntryMembershipRow struct {
	NetworkEntryID uint
	NodeGroupID    uint
	Name           string
	Code           string
	Description    string
	IsEnabled      bool
	Revision       uint64
	SortOrder      int
}

func (q NetworkEntryQueries) ListNetworkEntries(ctx context.Context, actor uint) (result []network.NetworkEntryListItem, err error) {
	if q.DB == nil {
		return nil, network.ErrNetworkEntryQueryUnavailable
	}
	err = q.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrNetworkEntryQueryPermission
			}
			return err
		}

		rows := make([]networkEntryQueryRow, 0)
		if err := tx.Table("network_entries").
			Select(`network_entries.id, network_entries.proxy_pool_id, network_entries.delivery_sort_order,
				network_entries.network, network_entries.name, network_entries.node_id, network_entries.endpoint_id,
				network_entries.address, network_entries.port, network_entries.public_port, network_entries.enabled,
				network_entries.revision, network_entries.created_at, network_entries.updated_at,
				'forward' AS service_kind, network_entries.endpoint_id AS parent_protocol_id,
				protocol_endpoints.node_id AS landing_node_id,
				CASE WHEN network_entries.path_config = '' THEN 0 ELSE 1 END AS has_path,
				nodes.name AS node_name, protocol_endpoints.name AS endpoint_name`).
			Joins("JOIN nodes ON nodes.id = network_entries.node_id").
			Joins("JOIN protocol_endpoints ON protocol_endpoints.id = network_entries.endpoint_id").
			Order("network_entries.id DESC").Scan(&rows).Error; err != nil {
			return err
		}
		result = make([]network.NetworkEntryListItem, 0, len(rows))
		if len(rows) == 0 {
			return nil
		}

		entryIDs := make([]uint, 0, len(rows))
		nodeIDs := make([]uint, 0, len(rows)*2)
		for _, row := range rows {
			entryIDs = append(entryIDs, row.ID)
			nodeIDs = append(nodeIDs, row.NodeID, row.LandingNodeID)
		}
		nodeIDs = uniqueSortedIDs(nodeIDs)

		publications := make([]model.NodeConfigPublish, 0)
		if err := tx.Select("node_id", "last_error").Where("node_id IN ?", nodeIDs).Find(&publications).Error; err != nil {
			return err
		}
		publicationByNode := make(map[uint]model.NodeConfigPublish, len(publications))
		for _, publication := range publications {
			publicationByNode[publication.NodeID] = publication
		}

		memberships := make([]networkEntryMembershipRow, 0)
		if err := tx.Table("node_group_network_entries AS membership").
			Select("membership.network_entry_id, membership.node_group_id, node_groups.name, node_groups.code, node_groups.description, node_groups.is_enabled, node_groups.revision, membership.sort_order").
			Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").
			Where("membership.network_entry_id IN ?", entryIDs).
			Order("membership.network_entry_id, node_groups.id").Scan(&memberships).Error; err != nil {
			return err
		}
		membershipsByEntry := make(map[uint][]network.NetworkEntryMembership, len(rows))
		namesByEntry := make(map[uint][]networkEntryGroupName, len(rows))
		for _, membership := range memberships {
			membershipsByEntry[membership.NetworkEntryID] = append(membershipsByEntry[membership.NetworkEntryID], network.NetworkEntryMembership{
				NodeGroupID: membership.NodeGroupID, Name: membership.Name, Code: membership.Code,
				Description: membership.Description, IsEnabled: membership.IsEnabled,
				Revision: membership.Revision, SortOrder: membership.SortOrder,
			})
			namesByEntry[membership.NetworkEntryID] = append(namesByEntry[membership.NetworkEntryID], networkEntryGroupName{ID: membership.NodeGroupID, Name: membership.Name})
		}

		for _, row := range rows {
			item := network.NetworkEntryListItem{
				NetworkEntryRecord: network.NetworkEntryRecord{
					ID: row.ID, ProxyPoolID: row.ProxyPoolID, DeliverySortOrder: row.DeliverySortOrder,
					Network: row.Network, Name: row.Name, NodeID: row.NodeID, EndpointID: row.EndpointID,
					Address: row.Address, Port: row.Port, PublicPort: row.PublicPort, Enabled: row.Enabled,
					Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
				},
				ServiceKind: row.ServiceKind, ParentProtocolID: row.ParentProtocolID,
				LandingNodeID: row.LandingNodeID, HasPath: row.HasPath,
				NodeName: row.NodeName, EndpointName: row.EndpointName,
				Memberships: make([]network.NetworkEntryMembership, 0), NodeGroupNames: make([]string, 0),
			}
			item.Memberships = append(item.Memberships, membershipsByEntry[row.ID]...)
			names := namesByEntry[row.ID]
			sort.Slice(names, func(i, j int) bool {
				if names[i].Name != names[j].Name {
					return names[i].Name < names[j].Name
				}
				return names[i].ID < names[j].ID
			})
			for _, name := range names {
				item.NodeGroupNames = append(item.NodeGroupNames, name.Name)
			}
			first, firstOK := publicationByNode[row.NodeID]
			second, secondOK := publicationByNode[row.LandingNodeID]
			item.Pending = firstOK || secondOK
			if firstOK {
				item.LastError = first.LastError
			}
			if secondOK && (second.LastError > item.LastError || second.LastError == item.LastError && second.NodeID < first.NodeID) {
				item.LastError = second.LastError
			}
			result = append(result, item)
		}
		return nil
	})
	return
}

type networkEntryGroupName struct {
	ID   uint
	Name string
}

func uniqueSortedIDs(values []uint) []uint {
	set := make(map[uint]struct{}, len(values))
	for _, value := range values {
		if value != 0 {
			set[value] = struct{}{}
		}
	}
	result := make([]uint, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
