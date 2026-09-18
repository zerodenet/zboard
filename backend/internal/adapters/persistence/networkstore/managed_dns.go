package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ManagedDNS struct{ DB *gorm.DB }

func managedDNSRecordView(row model.ManagedDNSRecord) network.ManagedDNSRecord {
	return network.ManagedDNSRecord{
		ID: row.ID, ProviderAccountID: row.ProviderAccountID, NodeID: row.NodeID,
		DomainName: row.DomainName, RecordType: row.RecordType, RecordValue: row.RecordValue,
		ProviderZoneID: row.ProviderZoneID, ProviderRecordID: row.ProviderRecordID,
		TTL: row.TTL, Proxied: row.Proxied, Status: row.Status, DesiredHash: row.DesiredHash,
		ObservedHash: row.ObservedHash, LastSyncedAt: row.LastSyncedAt,
		LastPublicCheckAt: row.LastPublicCheckAt, PublicResolved: row.PublicResolved,
		LastError: row.LastError, Revision: row.Revision, CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func managedDNSOperationView(row model.ProviderOperation) network.ManagedDNSOperation {
	return network.ManagedDNSOperation{
		ID: row.ID, ProviderAccountID: row.ProviderAccountID, ResourceType: row.ResourceType,
		ResourceID: row.ResourceID, OperationType: row.OperationType, Status: row.Status,
		Phase: row.Phase, RequestedBy: row.RequestedBy, ResultSummary: row.ResultSummary,
		Error: row.Error, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func managedDNSRecordModel(row network.ManagedDNSRecord) model.ManagedDNSRecord {
	return model.ManagedDNSRecord{
		ID: row.ID, ProviderAccountID: row.ProviderAccountID, NodeID: row.NodeID,
		DomainName: row.DomainName, RecordType: row.RecordType, RecordValue: row.RecordValue,
		ProviderZoneID: row.ProviderZoneID, ProviderRecordID: row.ProviderRecordID,
		TTL: row.TTL, Proxied: row.Proxied, Status: row.Status, DesiredHash: row.DesiredHash,
		ObservedHash: row.ObservedHash, LastSyncedAt: row.LastSyncedAt,
		LastPublicCheckAt: row.LastPublicCheckAt, PublicResolved: row.PublicResolved,
		LastError: row.LastError, Revision: row.Revision, CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (s ManagedDNS) ListManagedDNS(ctx context.Context, actor uint, query network.ManagedDNSListQuery) (out network.ManagedDNSPage, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			return network.ErrManagedDNSPermission
		}
		db := tx.Model(&model.ManagedDNSRecord{})
		if query.Search != "" {
			db = db.Where("domain_name LIKE ?", "%"+query.Search+"%")
		}
		if query.Status != "" {
			db = db.Where("status = ?", query.Status)
		}
		if err := db.Count(&out.Total).Error; err != nil {
			return err
		}
		var records []model.ManagedDNSRecord
		if err := db.Order("id desc").Offset(query.Offset).Limit(query.Limit).Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			out.Records = []network.ManagedDNSView{}
			return nil
		}
		providerIDs, nodeIDs, recordIDs := make([]uint, 0, len(records)), make([]uint, 0, len(records)), make([]uint, 0, len(records))
		for _, record := range records {
			providerIDs = append(providerIDs, record.ProviderAccountID)
			nodeIDs = append(nodeIDs, record.NodeID)
			recordIDs = append(recordIDs, record.ID)
		}
		var providers []model.ProviderAccount
		if err := tx.Select("id", "name", "provider_key").Where("id IN ?", providerIDs).Find(&providers).Error; err != nil {
			return err
		}
		var nodes []model.Node
		if err := tx.Select("id", "name").Where("id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
			return err
		}
		var operations []model.ProviderOperation
		if err := tx.Where("resource_type = ? AND resource_id IN ?", "dns_record", recordIDs).Order("resource_id asc, id desc").Find(&operations).Error; err != nil {
			return err
		}
		providerByID := make(map[uint]model.ProviderAccount, len(providers))
		for _, provider := range providers {
			providerByID[provider.ID] = provider
		}
		nodeByID := make(map[uint]model.Node, len(nodes))
		for _, node := range nodes {
			nodeByID[node.ID] = node
		}
		latestByRecord := make(map[uint]network.ManagedDNSOperation, len(operations))
		for _, operation := range operations {
			if _, exists := latestByRecord[operation.ResourceID]; !exists {
				latestByRecord[operation.ResourceID] = managedDNSOperationView(operation)
			}
		}
		out.Records = make([]network.ManagedDNSView, 0, len(records))
		for _, record := range records {
			provider, providerOK := providerByID[record.ProviderAccountID]
			node, nodeOK := nodeByID[record.NodeID]
			if !providerOK || !nodeOK {
				return network.ErrManagedDNSDependency
			}
			view := network.ManagedDNSView{ManagedDNSRecord: managedDNSRecordView(record), ProviderName: provider.Name, ProviderKey: provider.ProviderKey, NodeName: node.Name}
			if operation, exists := latestByRecord[record.ID]; exists {
				copy := operation
				view.LatestOperation = &copy
			}
			out.Records = append(out.Records, view)
		}
		return nil
	})
	return
}

func (s ManagedDNS) ManagedDNSDependencies(ctx context.Context, actor, providerID, nodeID uint) (out network.ManagedDNSDependencies, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			return network.ErrManagedDNSPermission
		}
		var node model.Node
		if err := tx.Select("id", "address", "ssh_host", "lifecycle_status").First(&node, nodeID).Error; err != nil {
			return network.ErrManagedDNSDependency
		}
		if node.LifecycleStatus == "deleting" {
			return network.ErrManagedDNSDeleting
		}
		var provider model.ProviderAccount
		if err := tx.Select("id", "provider_key", "status", "capabilities").First(&provider, providerID).Error; err != nil {
			return network.ErrManagedDNSDependency
		}
		out = network.ManagedDNSDependencies{NodeAddress: node.Address, NodeSSHHost: node.SSHHost, ProviderKey: provider.ProviderKey, ProviderStatus: provider.Status, ProviderSupportsDNS: providerSupportsDNS(provider.Capabilities)}
		return nil
	})
	return
}

func (s ManagedDNS) CreateManagedDNS(ctx context.Context, actor uint, inputs []network.ManagedDNSRecord) (out []network.ManagedDNSRecord, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			return network.ErrManagedDNSPermission
		}
		out = make([]network.ManagedDNSRecord, 0, len(inputs))
		for _, input := range inputs {
			if err := lockManagedDNSDependencies(tx, input.ProviderAccountID, input.NodeID); err != nil {
				return err
			}
			row := managedDNSRecordModel(input)
			if err := tx.Create(&row).Error; err != nil {
				return managedDNSError(err)
			}
			detail := fmt.Sprintf("domain=%s type=%s node=%d provider_account=%d", row.DomainName, row.RecordType, row.NodeID, row.ProviderAccountID)
			if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "dns_record.create", Target: fmt.Sprintf("managed_dns_record:%d", row.ID), Detail: detail}).Error; err != nil {
				return err
			}
			out = append(out, managedDNSRecordView(row))
		}
		return nil
	})
	return
}

func (s ManagedDNS) UpdateManagedDNS(ctx context.Context, actor, id uint, expected uint64, input network.ManagedDNSRecord) (out network.ManagedDNSRecord, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ManagedDNSRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrManagedDNSNotFound
			}
			return err
		}
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			return network.ErrManagedDNSPermission
		}
		if row.Status == "deleting" {
			return network.ErrManagedDNSDeleting
		}
		if row.Revision != expected {
			return network.ErrManagedDNSRevisionConflict
		}
		if row.ProviderAccountID != input.ProviderAccountID || row.DomainName != input.DomainName || row.RecordType != input.RecordType {
			return &network.ManagedDNSValidation{Fields: map[string]string{"identity": "供应商、域名和记录类型不能直接修改；如需变更，请删除后重新创建。"}}
		}
		for _, nodeID := range sortedUniqueIDs(row.NodeID, input.NodeID) {
			if err := lockManagedDNSNode(tx, nodeID); err != nil {
				return err
			}
		}
		if err := lockManagedDNSProvider(tx, row.ProviderAccountID); err != nil {
			return err
		}
		var running int64
		if err := tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND status = ?", "dns_record", row.ID, "running").Count(&running).Error; err != nil {
			return err
		}
		if running > 0 || row.Status == network.ManagedDNSSyncing {
			return network.ErrManagedDNSOperationRunning
		}
		updates := map[string]any{
			"node_id": input.NodeID, "record_value": input.RecordValue, "ttl": input.TTL,
			"proxied": input.Proxied, "desired_hash": input.DesiredHash,
			"status": network.ManagedDNSPending, "public_resolved": false,
			"last_public_check_at": nil, "last_error": "", "revision": row.Revision + 1,
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return managedDNSError(err)
		}
		detail := fmt.Sprintf("node=%d ttl=%d proxied=%t revision=%d", input.NodeID, input.TTL, input.Proxied, row.Revision+1)
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "dns_record.update", Target: fmt.Sprintf("managed_dns_record:%d", row.ID), Detail: detail}).Error; err != nil {
			return err
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		out = managedDNSRecordView(row)
		return nil
	})
	return
}

func (s ManagedDNS) StartManagedDNS(ctx context.Context, actor, recordID uint, takeover bool) (out network.ManagedDNSOperation, err error) {
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var record model.ManagedDNSRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, recordID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrManagedDNSNotFound
			}
			return err
		}
		var requestedBy *uint
		if actor != 0 {
			if _, err := providerAdmin(tx, actor); err != nil {
				return network.ErrManagedDNSPermission
			}
			requestedBy = &actor
		}
		if record.Status == "deleting" {
			return network.ErrManagedDNSDeleting
		}
		if err := lockManagedDNSNode(tx, record.NodeID); err != nil {
			return err
		}
		if err := lockManagedDNSProvider(tx, record.ProviderAccountID); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND status = ?", "dns_record", record.ID, "running").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 || record.Status == network.ManagedDNSSyncing {
			return network.ErrManagedDNSOperationRunning
		}
		now := time.Now().UTC()
		kind := "sync"
		if takeover {
			kind = "takeover"
		}
		operation := model.ProviderOperation{ProviderAccountID: record.ProviderAccountID, ResourceType: "dns_record", ResourceID: record.ID, OperationType: kind, Status: "running", Phase: "queued", RequestedBy: requestedBy, StartedAt: &now}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		if err := tx.Model(&record).Updates(map[string]any{"status": network.ManagedDNSSyncing, "last_error": ""}).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(struct {
			Revision    string `json:"revision"`
			OperationID uint   `json:"operation_id"`
		}{Revision: "1", OperationID: operation.ID})
		if err != nil {
			return err
		}
		if _, err := jobstore.New(tx).Submit(ctx, jobs.Submission{Timeout: time.Minute, Owner: "system", Key: fmt.Sprintf("dns_operation:%d", operation.ID), Handler: "dns_operation", ExecutionGroup: "external", Resource: fmt.Sprintf("dns:%d", recordID), Payload: string(payload)}); err != nil {
			return err
		}
		out = managedDNSOperationView(operation)
		return nil
	})
	return
}

func (s ManagedDNS) LoadManagedDNSExecution(ctx context.Context, operationID uint) (out network.ManagedDNSExecution, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.ProviderOperation
		if err := tx.First(&operation, operationID).Error; err != nil {
			return err
		}
		if operation.ResourceType != "dns_record" || operation.Status != "running" {
			return network.ErrManagedDNSOperationRunning
		}
		var record model.ManagedDNSRecord
		if err := tx.First(&record, operation.ResourceID).Error; err != nil {
			return err
		}
		var provider model.ProviderAccount
		if err := tx.Select("id", "provider_key", "credential_ciphertext").First(&provider, record.ProviderAccountID).Error; err != nil {
			return err
		}
		out = network.ManagedDNSExecution{Operation: managedDNSOperationView(operation), Record: managedDNSRecordView(record), ProviderKey: provider.ProviderKey, CredentialCiphertext: provider.CredentialCiphertext}
		return nil
	})
	return
}

func (s ManagedDNS) SetManagedDNSPhase(ctx context.Context, execution network.ManagedDNSExecution, phase string) error {
	result := s.DB.WithContext(ctx).Model(&model.ProviderOperation{}).Where("id = ? AND status = ? AND resource_id = ?", execution.Operation.ID, "running", execution.Record.ID).Update("phase", phase)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return network.ErrManagedDNSOperationRunning
	}
	return nil
}

func (s ManagedDNS) FailManagedDNS(ctx context.Context, execution network.ManagedDNSExecution, phase, message string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.ProviderOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operation, execution.Operation.ID).Error; err != nil {
			return err
		}
		if operation.Status != "running" || operation.ResourceID != execution.Record.ID {
			return network.ErrManagedDNSOperationRunning
		}
		if err := tx.Model(&model.ManagedDNSRecord{}).Where("id = ? AND revision = ?", execution.Record.ID, execution.Record.Revision).Updates(map[string]any{"status": network.ManagedDNSFailed, "last_error": message}).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&operation).Updates(map[string]any{"status": "failed", "phase": phase, "error": message, "finished_at": now}).Error
	})
}

func (s ManagedDNS) CompleteManagedDNS(ctx context.Context, execution network.ManagedDNSExecution, result network.ManagedDNSProviderResult, publicResolved bool) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.ProviderOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operation, execution.Operation.ID).Error; err != nil {
			return err
		}
		if operation.Status != "running" || operation.ResourceID != execution.Record.ID {
			return network.ErrManagedDNSOperationRunning
		}
		var record model.ManagedDNSRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, execution.Record.ID).Error; err != nil {
			return err
		}
		if record.Revision != execution.Record.Revision || record.DesiredHash != execution.Record.DesiredHash {
			return network.ErrManagedDNSRevisionConflict
		}
		observed := network.DNSRecordHash(result.RecordType, strings.ToLower(result.Name), result.Value, result.TTL, result.Proxied)
		status := network.ManagedDNSActive
		if observed != record.DesiredHash {
			status = network.ManagedDNSDrifted
		}
		now := time.Now().UTC()
		if err := tx.Model(&record).Updates(map[string]any{"provider_zone_id": result.ZoneID, "provider_record_id": result.RecordID, "observed_hash": observed, "status": status, "last_synced_at": now, "last_public_check_at": now, "public_resolved": publicResolved, "last_error": ""}).Error; err != nil {
			return err
		}
		summary := fmt.Sprintf("%s %s -> %s", record.RecordType, record.DomainName, record.RecordValue)
		return tx.Model(&operation).Updates(map[string]any{"status": "succeeded", "phase": "completed", "result_summary": summary, "error": "", "finished_at": now}).Error
	})
}

func (s ManagedDNS) ListManagedDNSObservations(ctx context.Context, now time.Time, interval time.Duration, limit int) ([]network.ManagedDNSRecord, error) {
	var rows []model.ManagedDNSRecord
	err := s.DB.WithContext(ctx).Where("public_resolved = ? AND last_synced_at IS NOT NULL AND status IN ? AND (last_public_check_at IS NULL OR last_public_check_at <= ?)", false, []string{network.ManagedDNSActive, network.ManagedDNSDrifted}, now.Add(-interval)).Order("last_public_check_at asc, id asc").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]network.ManagedDNSRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, managedDNSRecordView(row))
	}
	return out, nil
}

func (s ManagedDNS) RecordManagedDNSObservation(ctx context.Context, id uint, checkedAt time.Time, resolved bool) error {
	updates := map[string]any{"last_public_check_at": checkedAt}
	if resolved {
		updates["public_resolved"] = true
	}
	return s.DB.WithContext(ctx).Model(&model.ManagedDNSRecord{}).Where("id = ? AND public_resolved = ?", id, false).Updates(updates).Error
}

func lockManagedDNSDependencies(tx *gorm.DB, providerID, nodeID uint) error {
	if err := lockManagedDNSNode(tx, nodeID); err != nil {
		return err
	}
	return lockManagedDNSProvider(tx, providerID)
}

func lockManagedDNSNode(tx *gorm.DB, id uint) error {
	var node model.Node
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "lifecycle_status").First(&node, id).Error; err != nil {
		return network.ErrManagedDNSDependency
	}
	if node.LifecycleStatus == "deleting" {
		return network.ErrManagedDNSDeleting
	}
	return nil
}

func lockManagedDNSProvider(tx *gorm.DB, id uint) error {
	var provider model.ProviderAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "provider_key", "status", "capabilities").First(&provider, id).Error; err != nil {
		return network.ErrManagedDNSDependency
	}
	if strings.TrimSpace(provider.ProviderKey) == "" || provider.Status != "active" || !providerSupportsDNS(provider.Capabilities) {
		return network.ErrManagedDNSDependency
	}
	return nil
}

func providerSupportsDNS(raw string) bool {
	var capabilities []string
	if json.Unmarshal([]byte(raw), &capabilities) != nil {
		return false
	}
	for _, capability := range capabilities {
		if capability == "dns.records" {
			return true
		}
	}
	return false
}

func sortedUniqueIDs(values ...uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	out := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func managedDNSError(err error) error {
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "duplicate") || strings.Contains(lower, "unique constraint") {
		return network.ErrManagedDNSDuplicate
	}
	return err
}
