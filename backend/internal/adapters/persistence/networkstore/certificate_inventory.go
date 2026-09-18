package networkstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type CertificateInventory struct{ DB *gorm.DB }

func (s CertificateInventory) ListCertificates(ctx context.Context, input network.CertificateInventoryQuery) (network.CertificateInventoryPage, error) {
	query := s.DB.WithContext(ctx).Model(&model.ManagedCertificate{})
	if input.NodeID != 0 {
		query = query.Where("node_id = ?", input.NodeID)
	}
	if input.Status != "" {
		query = query.Where("status = ?", input.Status)
	}
	if input.Search != "" {
		like := "%" + input.Search + "%"
		query = query.Where("name LIKE ? OR domains LIKE ?", like, like)
	}
	page := network.CertificateInventoryPage{}
	if err := query.Count(&page.Total).Error; err != nil {
		return page, err
	}
	var rows []model.ManagedCertificate
	if err := query.Order("id desc").Offset(input.Offset).Limit(input.Limit).Find(&rows).Error; err != nil {
		return page, err
	}
	items, err := s.decorate(ctx, rows)
	page.Items = items
	return page, err
}

func (s CertificateInventory) decorate(ctx context.Context, rows []model.ManagedCertificate) ([]network.CertificateInventoryItem, error) {
	items := make([]network.CertificateInventoryItem, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	ids, nodeIDs := make([]uint, 0, len(rows)), make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		nodeIDs = append(nodeIDs, row.NodeID)
	}
	type nodeRow struct {
		ID   uint
		Name string
	}
	var nodes []nodeRow
	if err := s.DB.WithContext(ctx).Model(&model.Node{}).Select("id, name").Where("id IN ?", nodeIDs).Scan(&nodes).Error; err != nil {
		return nil, err
	}
	names := map[uint]string{}
	for _, row := range nodes {
		names[row.ID] = row.Name
	}
	type usageRow struct {
		ManagedCertificateID uint
		Count                int64
	}
	var usages []usageRow
	if err := s.DB.WithContext(ctx).Model(&model.CertificateProtocolEndpoint{}).Select("managed_certificate_id, COUNT(*) AS count").Where("managed_certificate_id IN ?", ids).Group("managed_certificate_id").Scan(&usages).Error; err != nil {
		return nil, err
	}
	counts := map[uint]int64{}
	for _, row := range usages {
		counts[row.ManagedCertificateID] = row.Count
	}
	latestIDs := s.DB.WithContext(ctx).Model(&model.CertificateOperation{}).Select("MAX(id)").Where("managed_certificate_id IN ?", ids).Group("managed_certificate_id")
	var operations []model.CertificateOperation
	if err := s.DB.WithContext(ctx).Where("id IN (?)", latestIDs).Find(&operations).Error; err != nil {
		return nil, err
	}
	latest := map[uint]network.CertificateOperationRecord{}
	for _, row := range operations {
		latest[row.ManagedCertificateID] = certificateOperationView(row)
	}
	for _, row := range rows {
		item := network.CertificateInventoryItem{Certificate: certificateView(row), NodeName: names[row.NodeID], UsageCount: counts[row.ID]}
		if operation, ok := latest[row.ID]; ok {
			copy := operation
			item.LatestOperation = &copy
		}
		items = append(items, item)
	}
	return items, nil
}

func (s CertificateInventory) CertificateDetail(ctx context.Context, id uint) (network.CertificateInventoryDetail, error) {
	var row model.ManagedCertificate
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.CertificateInventoryDetail{}, network.ErrCertificateNotFound
		}
		return network.CertificateInventoryDetail{}, err
	}
	items, err := s.decorate(ctx, []model.ManagedCertificate{row})
	if err != nil {
		return network.CertificateInventoryDetail{}, err
	}
	var endpointIDs []uint
	if err := s.DB.WithContext(ctx).Model(&model.CertificateProtocolEndpoint{}).Where("managed_certificate_id = ?", id).Order("protocol_endpoint_id asc").Pluck("protocol_endpoint_id", &endpointIDs).Error; err != nil {
		return network.CertificateInventoryDetail{}, err
	}
	return network.CertificateInventoryDetail{Certificate: items[0], ProtocolEndpointIDs: endpointIDs}, nil
}

func (s CertificateInventory) CertificateBindings(ctx context.Context, endpointIDs []uint) (map[uint]uint, error) {
	var rows []model.CertificateProtocolEndpoint
	if err := s.DB.WithContext(ctx).Where("protocol_endpoint_id IN ?", endpointIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[uint]uint, len(rows))
	for _, row := range rows {
		result[row.ProtocolEndpointID] = row.ManagedCertificateID
	}
	return result, nil
}
