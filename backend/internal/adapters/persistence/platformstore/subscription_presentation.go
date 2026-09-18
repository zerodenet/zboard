package platformstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type SubscriptionPresentation struct{ DB *gorm.DB }

func (s SubscriptionPresentation) SubscriptionCamouflage(ctx context.Context) (string, string, error) {
	var config model.SystemConfig
	err := s.DB.WithContext(ctx).Select("value").Where("config_key = ?", "subscription_camouflage_url").First(&config).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", err
	}
	var installation model.Installation
	if config.Value == "" {
		err = s.DB.WithContext(ctx).Select("site_url").First(&installation, 1).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", err
		}
	}
	return config.Value, installation.SiteURL, nil
}

var _ platform.SubscriptionPresentationRepository = SubscriptionPresentation{}
