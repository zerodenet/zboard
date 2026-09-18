package network

import (
	"context"
	"errors"
)

var ErrResourceNotFound = errors.New("network resource not found")
var ErrResourcePermission = errors.New("resource removal requires current administrator")

type ResourceRemovalBlocked struct{ Blockers map[string]int64 }

func (e *ResourceRemovalBlocked) Error() string { return "资源仍有未完成或待核验任务" }

type ResourceRemovalStore interface {
	RemoveDNS(context.Context, uint, uint) error
	RemoveCertificate(context.Context, uint, uint) error
}
type ResourceRemoval struct{ Store ResourceRemovalStore }
type DNSRemoved struct {
	ID                  uint `json:"id"`
	Deleted             bool `json:"deleted"`
	RemoteRecordDeleted bool `json:"remote_record_deleted"`
}
type CertificateRemoved struct {
	ID                       uint `json:"id"`
	Deleted                  bool `json:"deleted"`
	RemoteFilesRetained      bool `json:"remote_files_retained"`
	ExternalCleanupCompleted bool `json:"external_cleanup_completed"`
}

func (s ResourceRemoval) DNS(ctx context.Context, actor, id uint) (DNSRemoved, error) {
	if actor == 0 {
		return DNSRemoved{}, ErrResourcePermission
	}
	if id == 0 {
		return DNSRemoved{}, ErrResourceNotFound
	}
	if err := s.Store.RemoveDNS(ctx, actor, id); err != nil {
		return DNSRemoved{}, err
	}
	return DNSRemoved{ID: id, Deleted: true}, nil
}
func (s ResourceRemoval) Certificate(ctx context.Context, actor, id uint) (CertificateRemoved, error) {
	if actor == 0 {
		return CertificateRemoved{}, ErrResourcePermission
	}
	if id == 0 {
		return CertificateRemoved{}, ErrResourceNotFound
	}
	if err := s.Store.RemoveCertificate(ctx, actor, id); err != nil {
		return CertificateRemoved{}, err
	}
	return CertificateRemoved{ID: id, Deleted: true, RemoteFilesRetained: true}, nil
}
