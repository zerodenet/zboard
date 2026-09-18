package networkstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s ProviderAccounts) ListProviders(ctx context.Context, actor uint) (out []network.ProviderAccount, err error) {
	out = []network.ProviderAccount{}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			return err
		}
		var rows []model.ProviderAccount
		if err := tx.Order("id desc").Find(&rows).Error; err != nil {
			return err
		}
		counts := map[uint]int64{}
		for _, table := range []any{&model.ManagedDNSRecord{}, &model.ManagedCertificate{}} {
			var groups []struct {
				ProviderAccountID uint
				Total             int64
			}
			if err := tx.Model(table).Select("provider_account_id, COUNT(*) AS total").Where("provider_account_id IS NOT NULL").Group("provider_account_id").Scan(&groups).Error; err != nil {
				return err
			}
			for _, group := range groups {
				counts[group.ProviderAccountID] += group.Total
			}
		}
		for _, row := range rows {
			view := providerView(row)
			view.UsageCount = counts[row.ID]
			out = append(out, view)
		}
		return nil
	})
	return
}

func (s ProviderAccounts) DeleteProvider(ctx context.Context, actor, id uint) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var account model.ProviderAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProviderNotFound
			}
			return err
		}
		user, err := providerAdmin(tx, actor)
		if err != nil {
			return err
		}
		blockers, err := providerDeletionBlockers(tx, id)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return &network.ProviderDeletionBlocked{Blockers: blockers}
		}
		if err := tx.Model(&model.ManagedCertificate{}).Where("provider_account_id = ?", id).Updates(map[string]any{"provider_account_id": nil, "auto_renew": false}).Error; err != nil {
			return err
		}
		if err := tx.Where("provider_account_id = ?", id).Delete(&model.ManagedDNSRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "provider_account.delete", Target: fmt.Sprintf("provider_account:%d", id), Detail: "local credential removed; external account and shared token unchanged"}).Error; err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
}

func providerDeletionBlockers(tx *gorm.DB, id uint) (map[string]int64, error) {
	blockers := map[string]int64{}
	concat := func(prefix, column string) *gorm.DB {
		expression := "(? || " + column + ")"
		if tx.Dialector.Name() == "mysql" {
			expression = "CONCAT(?, " + column + ")"
		}
		return tx.Select(expression, prefix)
	}
	certIDs := tx.Model(&model.ManagedCertificate{}).Select("id").Where("provider_account_id = ?", id)
	dnsResources := concat("dns:", "id").Model(&model.ManagedDNSRecord{}).Where("provider_account_id = ?", id)
	inspections := concat("dns-inspection:", "id").Model(&model.ManagedDNSRecord{}).Where("provider_account_id = ?", id)
	certResources := concat("certificate:", "id").Model(&model.ManagedCertificate{}).Where("provider_account_id = ?", id)
	dnsKeys := concat("dns_operation:", "id").Model(&model.ProviderOperation{}).Where("provider_account_id = ? AND resource_type = ?", id, "dns_record")
	certKeys := concat("certificate_operation:", "id").Model(&model.CertificateOperation{}).Where("managed_certificate_id IN (?)", certIDs)
	pending := tx.Model(&jobstore.Record{}).Where("owner = ? AND state NOT IN ?", "system", []jobs.State{jobs.Succeeded, jobs.Failed, jobs.Canceled}).Where("resource IN (?) OR resource IN (?) OR resource IN (?) OR `key` IN (?) OR `key` IN (?)", dnsResources, inspections, certResources, dnsKeys, certKeys)
	queries := map[string]*gorm.DB{
		"running_operations":     tx.Model(&model.ProviderOperation{}).Where("provider_account_id = ? AND status = ?", id, "running"),
		"certificate_operations": tx.Model(&model.CertificateOperation{}).Where("managed_certificate_id IN (?) AND status = ?", certIDs, "running"),
		"active_certificates":    tx.Model(&model.ManagedCertificate{}).Where("provider_account_id = ? AND status IN ?", id, []string{"issuing", "renewing"}),
		"unverified_jobs":        pending,
	}
	// Even a terminal Run cannot justify destroying contradictory domain evidence.
	for _, kind := range []string{"dns", "certificate"} {
		table, prefix := "provider_operations", "dns_operation:"
		operations := tx.Table(table).Where("provider_account_id = ? AND resource_type = ?", id, "dns_record")
		if kind == "certificate" {
			table, prefix = "certificate_operations", "certificate_operation:"
			operations = tx.Table(table).Where("managed_certificate_id IN (?)", certIDs)
		}
		expression := "(? || " + table + ".id)"
		if tx.Dialector.Name() == "mysql" {
			expression = "CONCAT(?, " + table + ".id)"
		}
		mismatch := tx.Model(&jobstore.Record{}).Select("1").Where("owner = ? AND `key` = "+expression+" AND state IN ? AND state <> "+table+".status", "system", prefix, []jobs.State{jobs.Succeeded, jobs.Failed})
		queries[kind+"_contradictions"] = operations.Where("EXISTS (?)", mismatch)
	}
	for name, query := range queries {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			blockers[name] = count
		}
	}
	return blockers, nil
}
