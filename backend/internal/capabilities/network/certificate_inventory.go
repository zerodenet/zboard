package network

import "context"

type CertificateInventoryQuery struct {
	NodeID uint
	Status string
	Search string
	Offset int
	Limit  int
}

type CertificateInventoryItem struct {
	Certificate     CertificateRecord
	NodeName        string
	UsageCount      int64
	LatestOperation *CertificateOperationRecord
}

type CertificateInventoryPage struct {
	Items []CertificateInventoryItem
	Total int64
}

type CertificateInventoryDetail struct {
	Certificate         CertificateInventoryItem
	ProtocolEndpointIDs []uint
}

type CertificateInventoryRepository interface {
	ListCertificates(context.Context, CertificateInventoryQuery) (CertificateInventoryPage, error)
	CertificateDetail(context.Context, uint) (CertificateInventoryDetail, error)
	CertificateBindings(context.Context, []uint) (map[uint]uint, error)
}

type CertificateInventory struct {
	Repository CertificateInventoryRepository
}

func (s CertificateInventory) List(ctx context.Context, query CertificateInventoryQuery) (CertificateInventoryPage, error) {
	return s.Repository.ListCertificates(ctx, query)
}

func (s CertificateInventory) Detail(ctx context.Context, id uint) (CertificateInventoryDetail, error) {
	if id == 0 {
		return CertificateInventoryDetail{}, ErrCertificateNotFound
	}
	return s.Repository.CertificateDetail(ctx, id)
}

func (s CertificateInventory) Bindings(ctx context.Context, endpointIDs []uint) (map[uint]uint, error) {
	if len(endpointIDs) == 0 {
		return map[uint]uint{}, nil
	}
	return s.Repository.CertificateBindings(ctx, endpointIDs)
}
