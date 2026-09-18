package network

import (
	"context"
	"errors"
)

var ErrNativeOperationUnavailable = errors.New("native operation status unavailable")

type NativeOperationStatusRepository interface {
	NativeOperationStatus(context.Context, OperationKind, uint) (string, error)
}

type NativeOperationStatus struct {
	Repository NativeOperationStatusRepository
}

func (s NativeOperationStatus) Status(ctx context.Context, kind OperationKind, id uint) (string, error) {
	if s.Repository == nil || id == 0 || (kind != CertificateOperation && kind != DNSOperation) {
		return "", ErrNativeOperationUnavailable
	}
	return s.Repository.NativeOperationStatus(ctx, kind, id)
}
