package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const accountSubscriptionAccessPrefix = "/api/v1/account/subscriptions/"

type subscriptionAccessView struct {
	Configured      bool       `json:"configured"`
	SubscriptionID  uint       `json:"subscription_id"`
	Token           string     `json:"token,omitempty"`
	TokenPrefix     string     `json:"token_prefix,omitempty"`
	SubscriptionURL string     `json:"subscription_url,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	Revoked         bool       `json:"revoked,omitempty"`
	Notice          string     `json:"notice,omitempty"`
}

func parseAccountSubscriptionAccessID(path string, rotate bool) (uint, error) {
	if !strings.HasPrefix(path, accountSubscriptionAccessPrefix) {
		return 0, errors.New("invalid subscription access path")
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, accountSubscriptionAccessPrefix), "/"), "/")
	if (!rotate && len(parts) != 2) || (rotate && len(parts) != 3) {
		return 0, errors.New("invalid subscription access path")
	}
	if parts[1] != "access" || (rotate && parts[2] != "rotate") {
		return 0, errors.New("invalid subscription access path")
	}
	parsed, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || parsed == 0 {
		return 0, errors.New("invalid subscription id")
	}
	return uint(parsed), nil
}

func subscriptionAccessAvailable(subscription model.Subscription, now time.Time) bool {
	return subscription.Status == subStatusActive && subscription.EndAt.After(now) && subscription.FlowUsed < subscription.FlowTotal
}

func (h *handlers) ownedSubscription(userID, subscriptionID uint) (model.Subscription, error) {
	var subscription model.Subscription
	err := h.db.Where("id = ? AND user_id = ?", subscriptionID, userID).First(&subscription).Error
	return subscription, err
}

func (h *handlers) createMissingSubscriptionAccessToken(subscription model.Subscription) (model.SubscriptionToken, string, error) {
	var existing model.SubscriptionToken
	err := h.db.Where("subscription_id = ? AND user_id = ?", subscription.ID, subscription.UserID).First(&existing).Error
	if err == nil {
		return existing, "", nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SubscriptionToken{}, "", err
	}

	rawToken, tokenHash, prefix, err := newSubscriptionToken()
	if err != nil {
		return model.SubscriptionToken{}, "", err
	}
	encryptedToken, err := h.credentialCipher.Encrypt(rawToken)
	if err != nil {
		return model.SubscriptionToken{}, "", err
	}
	subscriptionID := subscription.ID
	candidate := model.SubscriptionToken{
		UserID:          subscription.UserID,
		SubscriptionID:  &subscriptionID,
		TokenHash:       tokenHash,
		TokenCiphertext: encryptedToken,
		TokenPrefix:     prefix,
	}
	result := h.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subscription_id"}},
		DoNothing: true,
	}).Create(&candidate)
	if result.Error != nil {
		return model.SubscriptionToken{}, "", result.Error
	}
	if result.RowsAffected > 0 {
		return candidate, rawToken, nil
	}
	if err := h.db.Where("subscription_id = ? AND user_id = ?", subscription.ID, subscription.UserID).First(&existing).Error; err != nil {
		return model.SubscriptionToken{}, "", err
	}
	return existing, "", nil
}

func (h *handlers) accessView(access model.SubscriptionToken, rawToken string, configured bool) subscriptionAccessView {
	view := subscriptionAccessView{
		Configured:     configured,
		TokenPrefix:    access.TokenPrefix,
		LastUsedAt:     access.LastUsedAt,
		RevokedAt:      access.RevokedAt,
		SubscriptionID: 0,
	}
	if access.SubscriptionID != nil {
		view.SubscriptionID = *access.SubscriptionID
	}
	if !access.CreatedAt.IsZero() {
		createdAt := access.CreatedAt
		view.CreatedAt = &createdAt
	}
	if !access.UpdatedAt.IsZero() {
		updatedAt := access.UpdatedAt
		view.UpdatedAt = &updatedAt
	}
	if configured && rawToken != "" {
		view.Token = rawToken
		view.SubscriptionURL = "/api/v1/client/subscription/" + rawToken
	}
	return view
}

func (h *handlers) AccountSubscriptionAccessHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	subscriptionID, err := parseAccountSubscriptionAccessID(r.URL.Path, false)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	now := time.Now().UTC()
	if err := expireSubscriptions(h.db, claims.UserID, now); err != nil {
		ServerError(w, err)
		return
	}
	subscription, err := h.ownedSubscription(claims.UserID, subscriptionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if !subscriptionAccessAvailable(subscription, now) {
		OK(w, subscriptionAccessView{Configured: false, SubscriptionID: subscription.ID})
		return
	}

	access, rawToken, err := h.createMissingSubscriptionAccessToken(subscription)
	if err != nil {
		ServerError(w, err)
		return
	}
	if access.RevokedAt != nil || access.TokenCiphertext == "" {
		OK(w, h.accessView(access, "", false))
		return
	}
	if rawToken == "" {
		rawToken, err = h.readableSubscriptionToken(&access)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	OK(w, h.accessView(access, rawToken, true))
}

func (h *handlers) AccountSubscriptionAccessRotateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	subscriptionID, err := parseAccountSubscriptionAccessID(r.URL.Path, true)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	now := time.Now().UTC()
	if err := expireSubscriptions(h.db, claims.UserID, now); err != nil {
		ServerError(w, err)
		return
	}
	subscription, err := h.ownedSubscription(claims.UserID, subscriptionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if !subscriptionAccessAvailable(subscription, now) {
		Forbidden(w, "subscription is inactive, expired, or out of traffic")
		return
	}

	rawToken, tokenHash, prefix, err := newSubscriptionToken()
	if err != nil {
		ServerError(w, err)
		return
	}
	encryptedToken, err := h.credentialCipher.Encrypt(rawToken)
	if err != nil {
		ServerError(w, err)
		return
	}

	var access model.SubscriptionToken
	err = h.db.Transaction(func(tx *gorm.DB) error {
		findErr := tx.Where("subscription_id = ? AND user_id = ?", subscription.ID, claims.UserID).First(&access).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			boundID := subscription.ID
			access = model.SubscriptionToken{
				UserID:          claims.UserID,
				SubscriptionID:  &boundID,
				TokenHash:       tokenHash,
				TokenCiphertext: encryptedToken,
				TokenPrefix:     prefix,
			}
			if err := tx.Create(&access).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&access).Updates(map[string]interface{}{
				"token_hash":       tokenHash,
				"token_ciphertext": encryptedToken,
				"token_prefix":     prefix,
				"last_used_at":     nil,
				"revoked_at":       nil,
			}).Error; err != nil {
				return err
			}
			access.TokenHash = tokenHash
			access.TokenCiphertext = encryptedToken
			access.TokenPrefix = prefix
			access.LastUsedAt = nil
			access.RevokedAt = nil
		}
		return createAuditLog(tx, claims, "subscription_token.rotate", fmt.Sprintf("subscription:%d", subscription.ID), prefix)
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	view := h.accessView(access, rawToken, true)
	view.Notice = "previous URL for this subscription is invalid"
	OK(w, view)
}

func (h *handlers) AccountSubscriptionAccessRevokeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	subscriptionID, err := parseAccountSubscriptionAccessID(r.URL.Path, false)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if _, err := h.ownedSubscription(claims.UserID, subscriptionID); errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	} else if err != nil {
		ServerError(w, err)
		return
	}

	now := time.Now().UTC()
	revoked := false
	err = h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SubscriptionToken{}).
			Where("subscription_id = ? AND user_id = ? AND revoked_at IS NULL", subscriptionID, claims.UserID).
			Update("revoked_at", now)
		if result.Error != nil {
			return result.Error
		}
		revoked = result.RowsAffected > 0
		if !revoked {
			return nil
		}
		return createAuditLog(tx, claims, "subscription_token.revoke", fmt.Sprintf("subscription:%d", subscriptionID), "credential revoked")
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	view := subscriptionAccessView{Configured: false, SubscriptionID: subscriptionID, Revoked: revoked}
	if revoked {
		view.RevokedAt = &now
	}
	OK(w, view)
}

// ReconcileSubscriptionAccessTokens provisions one independent token for every
// currently usable subscription that has no token row. Revoked rows are kept
// revoked and are never silently reactivated.
func (h *handlers) ReconcileSubscriptionAccessTokens() error {
	now := time.Now().UTC()
	var subscriptions []model.Subscription
	if err := h.db.Model(&model.Subscription{}).
		Joins("LEFT JOIN subscription_tokens ON subscription_tokens.subscription_id = subscriptions.id").
		Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", subStatusActive, now).
		Where("subscription_tokens.id IS NULL").
		Order("subscriptions.id asc").
		Find(&subscriptions).Error; err != nil {
		return fmt.Errorf("list subscriptions missing access tokens: %w", err)
	}
	for _, subscription := range subscriptions {
		if _, _, err := h.createMissingSubscriptionAccessToken(subscription); err != nil {
			return fmt.Errorf("create access token for subscription %d: %w", subscription.ID, err)
		}
	}
	return nil
}

func (h *handlers) ScopedClientSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/client/subscription/"))
	if rawToken == "" || strings.Contains(rawToken, "/") {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	var access model.SubscriptionToken
	if err := h.db.Where("token_hash = ? AND revoked_at IS NULL", hashSubscriptionToken(rawToken)).First(&access).Error; err != nil {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	if access.SubscriptionID == nil || *access.SubscriptionID == 0 {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", access.UserID, userStatusActive).First(&user).Error; err != nil {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	filter, err := parseSubscriptionProjectionFilter(r.URL.Query(), h.isProtocolSupported)
	if err != nil {
		BadRequestError(w, err)
		return
	}

	now := time.Now().UTC()
	if err := expireSubscriptions(h.db, access.UserID, now); err != nil {
		ServerError(w, err)
		return
	}
	var subscription model.Subscription
	if err := h.db.Where(
		"id = ? AND user_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total",
		*access.SubscriptionID, access.UserID, subStatusActive, now,
	).First(&subscription).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Forbidden(w, "subscription is inactive, expired, or out of traffic")
			return
		}
		ServerError(w, err)
		return
	}

	allSubscriptions := []model.Subscription{subscription}
	sources, err := h.loadSubscriptionProjectionSources(allSubscriptions)
	if err != nil {
		ServerError(w, err)
		return
	}
	subscriptions := filterSubscriptionsForProjection(allSubscriptions, sources, filter)
	if len(subscriptions) > 0 {
		if err := h.ensureCredentialsForSubscriptions(subscriptions); err != nil {
			ServerError(w, err)
			return
		}
	}
	manifestNodes, err := h.buildProjectedSubscriptionManifestNodes(subscriptions, filter, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	if len(subscriptions) > 0 {
		if err := h.sortSubscriptionManifestNodes(subscriptions, manifestNodes); err != nil {
			ServerError(w, fmt.Errorf("resolve subscription delivery order: %w", err))
			return
		}
	}

	remaining := subscription.FlowTotal - subscription.FlowUsed
	if remaining < 0 {
		remaining = 0
	}
	_ = h.db.Model(&access).Update("last_used_at", now).Error
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf(
		"upload=0; download=%d; total=%d; expire=%d",
		subscription.FlowUsed, subscription.FlowTotal, subscription.EndAt.Unix(),
	))
	manifest := subscriptionManifest{
		Version:     "zboard.subscription/v1",
		GeneratedAt: now.Format(time.RFC3339),
		Subscription: subscriptionManifestSummary{
			ExpiresAt:     subscription.EndAt.Format(time.RFC3339),
			FlowTotal:     subscription.FlowTotal,
			FlowUsed:      subscription.FlowUsed,
			FlowRemaining: remaining,
		},
		ProtocolEndpoints: manifestNodes,
	}
	h.writeProjectedSubscription(w, r, manifest)
}
