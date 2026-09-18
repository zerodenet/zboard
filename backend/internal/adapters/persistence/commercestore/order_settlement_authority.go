package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockSettlementUsers(tx *gorm.DB, buyer, actor uint) (model.User, error) {
	if actor == 0 {
		return model.User{}, commerce.ErrOrderPermission
	}
	ids := []uint{buyer}
	if actor != buyer {
		if actor < buyer {
			ids = []uint{actor, buyer}
		} else {
			ids = append(ids, actor)
		}
	}
	var administrator model.User
	for _, id := range ids {
		strength := "SHARE"
		if id == buyer {
			strength = "UPDATE"
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: strength}).Select("id", "email", "status", "is_admin").First(&user, id).Error; err != nil {
			if id == actor && errors.Is(err, gorm.ErrRecordNotFound) {
				return model.User{}, commerce.ErrOrderPermission
			}
			return model.User{}, err
		}
		if id == actor {
			if user.Status != "active" || !user.IsAdmin {
				return model.User{}, commerce.ErrOrderPermission
			}
			administrator = user
		}
	}
	return administrator, nil
}
