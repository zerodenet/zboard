package platform

import (
	"context"
	"errors"
	"time"
)

var ErrSettingsPermission = errors.New("settings require current administrator")

// Setting is a safe persistence projection. Secret values are empty before
// crossing the repository boundary; Configured preserves presence information.
type Setting struct {
	ID                                             uint
	ConfigKey, Name, Value, ValueType, Description string
	IsPublic, IsSecret, Configured                 bool
	Revision                                       uint64
	UpdatedAt                                      time.Time
}
type SettingsRepository interface {
	Public(context.Context) ([]Setting, error)
	Administrative(context.Context, uint) ([]Setting, error)
	Lookup(context.Context, string) (Setting, error)
}
type Settings struct{ Repository SettingsRepository }

func (s Settings) Public(ctx context.Context) ([]SettingView, error) {
	records, err := s.Repository.Public(ctx)
	if err != nil {
		return nil, err
	}
	visible := make([]Setting, 0, len(records))
	for _, record := range records {
		if record.IsPublic && !record.IsSecret {
			visible = append(visible, record)
		}
	}
	return settingViews(visible)
}
func (s Settings) Administrative(ctx context.Context, actor uint) ([]SettingView, error) {
	if actor == 0 {
		return nil, ErrSettingsPermission
	}
	records, err := s.Repository.Administrative(ctx, actor)
	if err != nil {
		return nil, err
	}
	return settingViews(records)
}

func (s Settings) Get(ctx context.Context, key string) (Setting, error) {
	if key == "" {
		return Setting{}, ErrSettingNotFound
	}
	return s.Repository.Lookup(ctx, key)
}

func (s Settings) View(ctx context.Context, key string) (SettingView, error) {
	record, err := s.Get(ctx, key)
	if err != nil {
		return SettingView{}, err
	}
	return SettingToView(record)
}
func settingViews(records []Setting) ([]SettingView, error) {
	views := make([]SettingView, 0, len(records))
	for _, record := range records {
		view, err := SettingToView(record)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
