package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrPermission        = errors.New("account administration not permitted")
	ErrLastAdministrator = errors.New("cannot modify the last active admin")
	ErrAccountNotFound   = errors.New("account not found")
	ErrEmailConflict     = errors.New("email already exists")
)

type AccountValidation struct{ Fields map[string]string }

func (e *AccountValidation) Error() string { return "invalid account input" }
func ValidEmail(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	a, err := mail.ParseAddress(value)
	return err == nil && a.Address == value
}
func ValidStatus(value string) bool {
	return value == "active" || value == "suspended" || value == "deactivated"
}

type NewAccount struct {
	Email, Password, Status string
	IsAdmin                 bool
}
type AccountChange struct {
	Status   *string
	IsAdmin  *bool
	Password *string
}
type StoredAccountChange struct {
	Status       *string
	IsAdmin      *bool
	PasswordHash *string
}
type AdministrationTx interface {
	Account(uint) (PublicAccount, error)
	Create(PublicAccount, string) (PublicAccount, error)
	Update(uint, StoredAccountChange) (PublicAccount, error)
	OtherActiveAdministrator(uint) (bool, error)
	Audit(string, uint, string) error
}
type AdministrationRepository interface {
	// Recheck the active administrator and serialize administrative mutations
	// before invoking the domain operation; commit its audit in the transaction.
	Administer(context.Context, Principal, func(AdministrationTx) error) error
}
type Administration struct{ Repository AdministrationRepository }

func (s Administration) Create(ctx context.Context, p Principal, in NewAccount) (PublicAccount, error) {
	if p.ID == 0 {
		return PublicAccount{}, ErrPermission
	}
	in.Email = NormalizeEmail(in.Email)
	in.Status = strings.TrimSpace(in.Status)
	if in.Status == "" {
		in.Status = "active"
	}
	fields := map[string]string{}
	if !ValidEmail(in.Email) {
		fields["email"] = "请输入有效邮箱。"
	}
	if !ValidPassword(in.Password) {
		fields["password"] = "密码必须为 12–72 个 UTF-8 字节。"
	}
	if !ValidStatus(in.Status) {
		fields["status"] = "账户状态无效。"
	}
	if len(fields) > 0 {
		return PublicAccount{}, &AccountValidation{fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return PublicAccount{}, err
	}
	var result PublicAccount
	err = s.Repository.Administer(ctx, p, func(tx AdministrationTx) error {
		var err error
		result, err = tx.Create(PublicAccount{Email: in.Email, Status: in.Status, IsAdmin: in.IsAdmin}, string(hash))
		if err != nil {
			return err
		}
		return tx.Audit("user.create", result.ID, fmt.Sprintf("status=%s admin=%t", result.Status, result.IsAdmin))
	})
	if err != nil {
		return PublicAccount{}, err
	}
	return result, nil
}
func (s Administration) Update(ctx context.Context, p Principal, id uint, in AccountChange) (PublicAccount, error) {
	if p.ID == 0 {
		return PublicAccount{}, ErrPermission
	}
	if id == 0 {
		return PublicAccount{}, ErrAccountNotFound
	}
	fields := map[string]string{}
	changed := []string{}
	updates := StoredAccountChange{IsAdmin: in.IsAdmin}
	if in.Status != nil {
		value := strings.TrimSpace(*in.Status)
		updates.Status = &value
		if !ValidStatus(value) {
			fields["status"] = "账户状态无效。"
		}
		changed = append(changed, "status")
	}
	if in.IsAdmin != nil {
		changed = append(changed, "is_admin")
	}
	if in.Password != nil {
		if !ValidPassword(*in.Password) {
			fields["password"] = "密码必须为 12–72 个 UTF-8 字节。"
		}
		changed = append(changed, "password")
	}
	if len(changed) == 0 {
		return PublicAccount{}, &AccountValidation{map[string]string{"form": "no valid update fields"}}
	}
	if len(fields) > 0 {
		return PublicAccount{}, &AccountValidation{fields}
	}
	if in.Password != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if err != nil {
			return PublicAccount{}, err
		}
		value := string(hash)
		updates.PasswordHash = &value
	}
	var result PublicAccount
	err := s.Repository.Administer(ctx, p, func(tx AdministrationTx) error {
		before, err := tx.Account(id)
		if err != nil {
			return err
		}
		status, admin := before.Status, before.IsAdmin
		if updates.Status != nil {
			status = *updates.Status
		}
		if updates.IsAdmin != nil {
			admin = *updates.IsAdmin
		}
		if before.IsAdmin && before.Status == "active" && (!admin || status != "active") {
			another, err := tx.OtherActiveAdministrator(id)
			if err != nil {
				return err
			}
			if !another {
				return ErrLastAdministrator
			}
		}
		result, err = tx.Update(id, updates)
		if err != nil {
			return err
		}
		return tx.Audit("user.update", id, "fields="+strings.Join(changed, ","))
	})
	if err != nil {
		return PublicAccount{}, err
	}
	return result, nil
}
