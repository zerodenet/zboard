package identitystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Integrations struct {
	DB  *gorm.DB
	Now func() time.Time
}

func integrationView(v model.UserAPIToken) identity.IntegrationToken {
	scopes := []string{}
	_ = json.Unmarshal([]byte(v.Scopes), &scopes)
	return identity.IntegrationToken{ID: v.ID, Name: v.Name, Prefix: v.TokenPrefix, Scopes: scopes, ExpiresAt: v.ExpiresAt, RevokedAt: v.RevokedAt, CreatedAt: v.CreatedAt}
}
func integrationAccount(db *gorm.DB, actor uint) (model.User, error) {
	var user model.User
	err := db.Where("id = ? AND status = ?", actor, "active").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return user, identity.ErrPermission
	}
	return user, err
}
func (s Integrations) Create(ctx context.Context, actor uint, in identity.IntegrationIssue, hash, prefix string) (identity.IntegrationToken, error) {
	var row model.UserAPIToken
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := integrationAccount(tx.Clauses(clause.Locking{Strength: "UPDATE"}), actor)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.UserAPIToken{}).Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", actor, time.Now().UTC()).Count(&count).Error; err != nil {
			return err
		}
		if count >= 20 {
			return identity.ErrIntegrationInput
		}
		scopes, err := json.Marshal(in.Scopes)
		if err != nil {
			return err
		}
		expires := in.ExpiresAt.UTC()
		row = model.UserAPIToken{UserID: actor, Name: in.Name, TokenHash: hash, TokenPrefix: prefix, Scopes: string(scopes), ExpiresAt: &expires}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &actor, Actor: user.Email, Action: "integration.credential.create", Target: fmt.Sprintf("api_token:%d", row.ID), Detail: string(scopes)}).Error
	})
	if err != nil {
		return identity.IntegrationToken{}, err
	}
	return integrationView(row), nil
}
func (s Integrations) List(ctx context.Context, actor uint, offset, limit int) ([]identity.IntegrationToken, error) {
	db := s.DB.WithContext(ctx)
	if _, err := integrationAccount(db, actor); err != nil {
		return nil, err
	}
	var rows []model.UserAPIToken
	if err := db.Where("user_id = ?", actor).Order("id desc").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]identity.IntegrationToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, integrationView(row))
	}
	return out, nil
}
func (s Integrations) Revoke(ctx context.Context, actor, id uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := integrationAccount(tx.Clauses(clause.Locking{Strength: "UPDATE"}), actor)
		if err != nil {
			return err
		}
		var token model.UserAPIToken
		err = tx.Where("id = ? AND user_id = ?", id, actor).First(&token).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identity.ErrPermission
		}
		if err != nil {
			return err
		}
		if token.RevokedAt != nil {
			return nil
		}
		if err := tx.Model(&token).Update("revoked_at", time.Now().UTC()).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &actor, Actor: user.Email, Action: "integration.credential.revoke", Target: fmt.Sprintf("api_token:%d", id)}).Error
	})
}

// Resolve never inherits the account's admin flag. Integration credentials are
// scoped to their owner's account and exact named operations in this version.
func (s Integrations) authenticate(ctx context.Context, c catalog.Credential) (model.UserAPIToken, error) {
	if c.Kind != "integration" || len(c.Proof) != 47 || c.Proof[:4] != "zbi_" {
		return model.UserAPIToken{}, catalog.ErrDenied
	}
	var token model.UserAPIToken
	err := s.DB.WithContext(ctx).Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", identity.IntegrationTokenHash(c.Proof), time.Now().UTC()).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserAPIToken{}, catalog.ErrDenied
	}
	if err != nil {
		return model.UserAPIToken{}, err
	}
	if _, err := integrationAccount(s.DB.WithContext(ctx), token.UserID); err != nil {
		if errors.Is(err, identity.ErrPermission) {
			return model.UserAPIToken{}, catalog.ErrDenied
		}
		return model.UserAPIToken{}, err
	}
	return token, nil
}

func (s Integrations) Authenticate(ctx context.Context, c catalog.Credential) error {
	_, err := s.authenticate(ctx, c)
	return err
}
func (s Integrations) Resolve(ctx context.Context, c catalog.Credential, operation string) (catalog.Grant, error) {
	token, err := s.authenticate(ctx, c)
	if err != nil {
		return catalog.Grant{}, err
	}
	var scopes []string
	if json.Unmarshal([]byte(token.Scopes), &scopes) != nil {
		return catalog.Grant{}, catalog.ErrDenied
	}
	allowed := false
	for _, scope := range scopes {
		if scope == operation {
			allowed = true
		}
	}
	if !allowed {
		return catalog.Grant{}, catalog.ErrDenied
	}
	return catalog.Grant{Principal: catalog.Principal{
		Kind: "integration", Subject: fmt.Sprintf("integration:%d", token.ID),
		AccountID: token.UserID, CredentialID: token.ID,
	}}, nil
}

func (s Integrations) Admit(ctx context.Context, _ catalog.Credential, grant catalog.Grant, descriptor catalog.Descriptor) error {
	principal := grant.Principal
	if s.DB == nil || principal.Kind != "integration" || principal.AccountID == 0 || principal.CredentialID == 0 || descriptor.RateLimitPerMinute < 1 {
		return catalog.ErrUnavailable
	}
	now := s.now()
	window := now.Truncate(time.Minute)
	result := s.DB.WithContext(ctx).Exec(`UPDATE user_api_tokens
		SET invocation_window_count = CASE WHEN invocation_window_started_at IS NULL OR invocation_window_started_at < ? THEN 1 ELSE invocation_window_count + 1 END,
			invocation_window_started_at = CASE WHEN invocation_window_started_at IS NULL OR invocation_window_started_at < ? THEN ? ELSE invocation_window_started_at END,
			last_used_at = ?
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?
			AND (invocation_window_started_at IS NULL OR invocation_window_started_at < ? OR invocation_window_count < ?)
			AND EXISTS (SELECT 1 FROM users WHERE users.id = user_api_tokens.user_id AND users.status = ? AND users.deleted_at IS NULL)`,
		window, window, window, now, principal.CredentialID, principal.AccountID, now, window, descriptor.RateLimitPerMinute, "active")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var valid int64
	if err := s.DB.WithContext(ctx).Model(&model.UserAPIToken{}).
		Joins("JOIN users ON users.id = user_api_tokens.user_id AND users.status = ? AND users.deleted_at IS NULL", "active").
		Where("user_api_tokens.id = ? AND user_api_tokens.user_id = ? AND user_api_tokens.revoked_at IS NULL AND user_api_tokens.expires_at > ?", principal.CredentialID, principal.AccountID, now).
		Count(&valid).Error; err != nil {
		return err
	}
	if valid == 1 {
		return catalog.ErrRateLimited
	}
	return catalog.ErrDenied
}

func (s Integrations) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
