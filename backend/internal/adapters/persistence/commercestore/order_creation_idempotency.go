package commercestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var creationKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,80}$`)

func createOwnedOrder(tx *gorm.DB, buyer uint, request commerce.OrderCreateRequest, clocks ...func() time.Time) (model.Order, error) {
	if request.IdempotencyKey == "" {
		return createOrderInTransaction(tx, buyer, request, clocks...)
	}
	invalid := func() error {
		return &commerce.ValidationError{Message: "订单创建请求已变化，请重新确认。", Fields: map[string]string{"idempotency_key": "重复请求必须使用原始创建参数。"}}
	}
	if !creationKeyPattern.MatchString(request.IdempotencyKey) {
		return model.Order{}, invalid()
	}
	key := sha256.Sum256([]byte(strconv.FormatUint(uint64(buyer), 10) + "\x00" + request.IdempotencyKey))
	keyString := hex.EncodeToString(key[:])
	body, _ := json.Marshal(request)
	fingerprint := sha256.Sum256(body)
	content := hex.EncodeToString(fingerprint[:])
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&user, buyer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Order{}, commerce.ErrBuyerUnavailable
		}
		return model.Order{}, err
	}
	if user.Status != "active" {
		return model.Order{}, commerce.ErrBuyerUnavailable
	}
	var existing model.Order
	err := tx.Where("creation_key = ? AND user_id = ?", keyString, buyer).First(&existing).Error
	if err == nil {
		if existing.CreationFingerprint != content {
			return model.Order{}, invalid()
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Order{}, err
	}
	order, err := createOrderInTransaction(tx, buyer, request, clocks...)
	if err != nil {
		return model.Order{}, err
	}
	if err := tx.Model(&order).Updates(map[string]any{"creation_key": keyString, "creation_fingerprint": content}).Error; err != nil {
		return model.Order{}, err
	}
	order.CreationKey, order.CreationFingerprint = &keyString, content
	return order, nil
}
