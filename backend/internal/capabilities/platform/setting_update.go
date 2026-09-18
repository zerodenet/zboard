package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrSettingRevision = errors.New("system config revision conflict")
	ErrSettingNotFound = errors.New("system config not found")
)

type SettingValidation struct{ Cause error }

func (e *SettingValidation) Error() string { return e.Cause.Error() }
func (e *SettingValidation) Unwrap() error { return e.Cause }

type SettingsCipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}
type SettingUpdateInput struct {
	Key              string
	Value            json.RawMessage
	ExpectedRevision *uint64
}
type SettingUpdateRepository interface {
	Update(context.Context, uint, SettingUpdateInput) (Setting, error)
}
type SettingUpdate struct{ Repository SettingUpdateRepository }

func (s SettingUpdate) Update(ctx context.Context, actor uint, in SettingUpdateInput) (SettingView, error) {
	if actor == 0 || strings.HasPrefix(in.Key, "maintenance_") {
		return SettingView{}, ErrSettingsPermission
	}
	if in.Key == "" || len(in.Key) > 80 || len(in.Value) == 0 || len(in.Value) > 1<<20 || !json.Valid(in.Value) {
		return SettingView{}, &SettingValidation{Cause: errors.New("invalid setting request")}
	}
	for _, ch := range in.Key {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return SettingView{}, &SettingValidation{Cause: errors.New("invalid setting key")}
		}
	}
	row, err := s.Repository.Update(ctx, actor, in)
	if err != nil {
		return SettingView{}, err
	}
	return SettingToView(row)
}
