package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

type ProviderAccounts struct{ DB *gorm.DB }

func providerAdmin(tx *gorm.DB, id uint) (model.User, error) {
	var user model.User
	if id == 0 {
		return user, network.ErrProviderPermission
	}
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", id, true, "active").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = network.ErrProviderPermission
	}
	return user, err
}
func providerView(row model.ProviderAccount) network.ProviderAccount {
	var capabilities []string
	_ = json.Unmarshal([]byte(row.Capabilities), &capabilities)
	return network.ProviderAccount{ID: row.ID, ProviderKey: row.ProviderKey, Name: row.Name, Capabilities: capabilities, CredentialPrefix: row.CredentialPrefix, Status: row.Status, LastVerifiedAt: row.LastVerifiedAt, LastError: row.LastError, Revision: row.Revision, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func (s ProviderAccounts) LoadProvider(ctx context.Context, actor, id uint) (out network.ProviderSnapshot, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			return err
		}
		var row model.ProviderAccount
		if err := tx.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProviderNotFound
			}
			return err
		}
		out = network.ProviderSnapshot{Account: providerView(row), Ciphertext: row.CredentialCiphertext}
		return nil
	})
	return
}
func (s ProviderAccounts) CommitProvider(ctx context.Context, actor uint, before network.ProviderSnapshot, change network.ProviderChange) (out network.ProviderAccount, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Resource before actor matches network reconciliation's lock order.
		var row model.ProviderAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, before.Account.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProviderNotFound
			}
			return err
		}
		user, err := providerAdmin(tx, actor)
		if err != nil {
			return err
		}
		if row.Revision != before.Account.Revision || row.CredentialCiphertext != before.Ciphertext || row.Status != before.Account.Status || row.ProviderKey != before.Account.ProviderKey {
			return network.ErrProviderConflict
		}
		updates := map[string]any{"name": change.Name, "revision": row.Revision + 1}
		if change.ReplaceCredential {
			updates["credential_ciphertext"] = change.Ciphertext
			updates["credential_prefix"] = change.Prefix
		}
		if change.Verify {
			if change.Verified {
				updates["status"] = "active"
				updates["last_verified_at"] = change.CheckedAt
				updates["last_error"] = ""
			} else {
				updates["status"] = "invalid"
				updates["last_error"] = change.Failure
			}
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		action, detail := "provider_account.update", fmt.Sprintf("credential_replaced=%t", change.ReplaceCredential)
		if change.Verify && !change.ReplaceCredential {
			action = "provider_account.verify"
			detail = fmt.Sprintf("verified=%t", change.Verified)
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: action, Target: fmt.Sprintf("provider_account:%d", row.ID), Detail: detail}).Error; err != nil {
			return err
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		out = providerView(row)
		return nil
	})
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique constraint")) {
		err = network.ErrProviderDuplicate
	}
	return
}

func (s ProviderAccounts) CreateProvider(ctx context.Context, actor uint, input network.ProviderSnapshot) (out network.ProviderAccount, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := providerAdmin(tx, actor)
		if err != nil {
			return err
		}
		capabilities, err := json.Marshal(input.Account.Capabilities)
		if err != nil {
			return err
		}
		row := model.ProviderAccount{ProviderKey: input.Account.ProviderKey, Name: input.Account.Name, Capabilities: string(capabilities), CredentialCiphertext: input.Ciphertext, CredentialPrefix: input.Account.CredentialPrefix, Status: "pending", Revision: 1, CreatedBy: actor}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "provider_account.create", Target: fmt.Sprintf("provider_account:%d", row.ID), Detail: "provider=" + row.ProviderKey}).Error; err != nil {
			return err
		}
		out = providerView(row)
		return nil
	})
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique constraint")) {
		err = network.ErrProviderDuplicate
	}
	return
}
