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
	return createOwnedOrderWithDiscount(tx, buyer, request, nil, clocks...)
}

// existingOwnedCreation is also used before a native quote. A retry of a
// committed order never depends on the provider remaining online or enabled.
func existingOwnedCreation(tx *gorm.DB, buyer uint, request commerce.OrderCreateRequest) (*model.Order, string, string, error) {
	if request.IdempotencyKey == "" {
		return nil, "", "", nil
	}
	invalid := func() error {
		return &commerce.ValidationError{Message: "订单创建请求已变化，请重新确认。", Fields: map[string]string{"idempotency_key": "重复请求必须使用原始创建参数。"}}
	}
	if !creationKeyPattern.MatchString(request.IdempotencyKey) {
		return nil, "", "", invalid()
	}
	key := sha256.Sum256([]byte(strconv.FormatUint(uint64(buyer), 10) + "\x00" + request.IdempotencyKey))
	keyString := hex.EncodeToString(key[:])
	body, _ := json.Marshal(request)
	fingerprint := sha256.Sum256(body)
	content := hex.EncodeToString(fingerprint[:])
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&user, buyer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", "", commerce.ErrBuyerUnavailable
		}
		return nil, "", "", err
	}
	if user.Status != "active" {
		return nil, "", "", commerce.ErrBuyerUnavailable
	}
	var existing model.Order
	err := tx.Where("creation_key = ? AND user_id = ?", keyString, buyer).First(&existing).Error
	if err == nil {
		if existing.CreationFingerprint != content {
			return nil, "", "", invalid()
		}
		return &existing, keyString, content, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", "", err
	}
	return nil, keyString, content, nil
}

func createOwnedOrderWithDiscount(tx *gorm.DB, buyer uint, request commerce.OrderCreateRequest, discount *checkoutDiscount, clocks ...func() time.Time) (model.Order, error) {
	var clock func() time.Time
	if len(clocks) > 0 {
		clock = clocks[0]
	}
	existing, keyString, content, err := existingOwnedCreation(tx, buyer, request)
	if err != nil {
		return model.Order{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	order, err := createOrderWithDiscountInTransaction(tx, buyer, request, discount, clock)
	if err != nil {
		return model.Order{}, err
	}
	if keyString != "" {
		if err := tx.Model(&order).Updates(map[string]any{"creation_key": keyString, "creation_fingerprint": content}).Error; err != nil {
			return model.Order{}, err
		}
		order.CreationKey, order.CreationFingerprint = &keyString, content
	}
	return order, nil
}
