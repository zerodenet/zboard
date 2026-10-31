package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
)

// CopyApplicationData copies the authoritative table inventory into a prepared,
// empty destination. The caller owns source quiescence, destination transaction
// and foreign-key ordering. Missing legacy source tables may be absent; an
// existing source table must never be silently dropped from the destination.
func CopyApplicationData(source, target *gorm.DB) error {
	if err := preflightPrincipalMinuteSchema(source); err != nil {
		return err
	}
	if err := preflightCompactPeriodicSchema(source); err != nil {
		return err
	}
	if err := preflightFlowStartStatisticsCopy(source); err != nil {
		return err
	}
	if err := preflightPrincipalStatisticsCopy(source); err != nil {
		return err
	}
	if err := preflightPrincipalRuntimeCopy(source); err != nil {
		return err
	}
	if err := preflightCoverageRuntimeCopy(source); err != nil {
		return err
	}
	if err := preflightObservationRetirementSchema(source); err != nil {
		return err
	}
	if err := preflightAutomaticPendingOrders(source); err != nil {
		return err
	}
	if err := preflightDiscountRedemptionSchema(source); err != nil {
		return err
	}
	tables, err := MigrationTables(source)
	if err != nil {
		return err
	}
	sourceTables, err := migrationTableSet(source)
	if err != nil {
		return fmt.Errorf("inspect source tables: %w", err)
	}
	// Reject recorded identity loss even when the source nodes table itself
	// is absent and the legacy table loop would otherwise skip it.
	if err := inspectTrafficMetaArchiveSchema(source, nil); err != nil {
		return err
	}
	if err := preflightReportReceiptSchema(source); err != nil {
		return err
	}
	if err := preflightDiagnosticMaterialSchema(source); err != nil {
		return err
	}
	if err := preflightObservationMaterialSchema(source); err != nil {
		return err
	}
	_, observationReceipt, err := observationSchema(source)
	if err != nil {
		return err
	}
	if err := preflightProtocolDeletionSchema(source); err != nil {
		return err
	}
	if err := preflightNodeArchiveSchema(source); err != nil {
		return err
	}
	if err := preflightNodeAssetSchema(source); err != nil {
		return err
	}
	targetTables, err := migrationTableSet(target)
	if err != nil {
		return fmt.Errorf("inspect target tables: %w", err)
	}
	// Validate the complete actual source inventory before writing any rows.
	// Truly legacy sources may have no auxiliary tables; recorded authority was
	// already checked above and cannot pass as an absent legacy table.
	for _, table := range tables {
		if sourceTables[table] && !targetTables[table] {
			return fmt.Errorf("migration destination is missing source table %s", table)
		}
	}
	if sourceTables["subscriptions"] {
		// Legacy sources may lack the operational retry column, including a
		// recorded-but-lost schedule. NULL in the owned empty destination means
		// retry immediately; it does not manufacture money or completed cleanup.
		if err := validateSubscriptionRetirementRetryColumn(target); err != nil {
			return err
		}
		columns, _, err := projectionColumns(source, "subscriptions")
		if err != nil {
			return err
		}
		if _, present := columns["retirement_retry_at"]; present {
			if err := validateSubscriptionRetirementRetryColumn(source); err != nil {
				return err
			}
		}
	}
	if sourceTables["principal_flow_scope_observations"] || sourceTables["principal_statistics_state"] {
		if err := requirePrincipalStatisticsDestination(target); err != nil {
			return err
		}
	}
	if sourceTables["subscription_flow_start_events"] || sourceTables["flow_start_statistics_state"] {
		if err := requireFlowStartStatisticsDestination(target); err != nil {
			return err
		}
	}
	if err := requirePrincipalRuntimeDestination(target, sourceTables); err != nil {
		return err
	}
	if err := requireCoverageRuntimeDestination(target, sourceTables); err != nil {
		return err
	}
	for _, table := range tables {
		if !sourceTables[table] {
			continue
		}
		if !targetTables[table] {
			return fmt.Errorf("migration destination is missing source table %s", table)
		}
		if err := copyTableRows(source, target, table); err != nil {
			return err
		}
	}
	if err := initializeCopiedObservationBudget(target, sourceTables["node_observation_material_budget"], sourceTables["node_observation_parts"] || sourceTables["node_observation_intents"]); err != nil {
		return err
	}
	if err := initializeCopiedPrincipalStatistics(target, sourceTables["principal_statistics_state"], sourceTables["principal_flow_scope_observations"]); err != nil {
		return err
	}
	if err := initializeCopiedFlowStartStatistics(target, sourceTables["flow_start_statistics_state"], sourceTables["subscription_flow_start_events"]); err != nil {
		return err
	}
	// A prepared target has already recorded0060 before source data arrives.
	// Only a genuinely legacy source imports its copied old cursor, in this same
	// owned target transaction; never infer missing recorded source authority.
	if !observationReceipt {
		if err := importObservationFloors(target); err != nil {
			return err
		}
	}
	return RebuildMySQLTrafficHourly(target)
}

func copyTableRows(source, target *gorm.DB, table string) error {
	// Classify before opening source Rows: migration sources can be pinned to a
	// single connection. Never query a receipt while that connection streams.
	legacyNodeAssets := false
	if table == "nodes" {
		state, err := inspectNodeAssetSchema(source)
		if err != nil {
			return fmt.Errorf("classify copied node assets: %w", err)
		}
		legacyNodeAssets = !state.column
	}

	rows, err := source.Table(table).Rows()
	if err != nil {
		return fmt.Errorf("read source table %s: %w", table, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read source columns %s: %w", table, err)
	}
	missingRetirementRetry := table == "subscriptions"
	if missingRetirementRetry {
		for _, column := range columns {
			if strings.EqualFold(column, "retirement_retry_at") {
				missingRetirementRetry = false
				break
			}
		}
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return fmt.Errorf("read source column types %s: %w", table, err)
	}
	// Only known missing derived columns invalidate copied coverage. A complete
	// source may legitimately contain empty digests; do not infer corruption
	// from values or rewrite canonical credentials/source data.
	resetPrincipalLookup := false
	lookupVersionColumn := "principal_lookup_version"
	if table == "protocol_credentials" {
		present := make(map[string]bool, 3)
		for _, column := range columns {
			name := strings.ToLower(column)
			present[name] = true
			if name == "principal_lookup_version" {
				// Reuse its actual map key, including case, to avoid inserting
				// two case-equivalent destination columns.
				lookupVersionColumn = column
			}
		}
		resetPrincipalLookup = !present["principal_digest"] || !present["secret_principal_digest"] || !present["principal_lookup_version"]
	}
	batch := make([]map[string]interface{}, 0, 200)
	var runtimeBatchBytes int64
	for rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return fmt.Errorf("scan source table %s: %w", table, err)
		}
		record := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			if raw, ok := values[index].([]byte); ok {
				kind := strings.ToUpper(types[index].DatabaseTypeName())
				switch {
				case strings.Contains(kind, "BLOB"), kind == "BINARY", kind == "VARBINARY", kind == "BIT":
					record[column] = append([]byte(nil), raw...)
				default:
					// MySQL returns JSON/text/decimal as bytes; inserting them
					// as []byte into SQLite would change their storage class.
					record[column] = string(raw)
				}
			} else {
				record[column] = values[index]
			}
		}
		if err := validateFlowStartStatisticsCopyTime(source, target, table, record); err != nil {
			return err
		}
		if err := validatePrincipalStatisticsCopyTime(source, target, table, record); err != nil {
			return err
		}
		if resetPrincipalLookup {
			// Target INSERT belongs to the caller's existing copy transaction.
			// A missing version uses the prepared target's standard column name.
			record[lookupVersionColumn] = int64(0)
		}
		if missingRetirementRetry {
			// Explicit NULL also avoids inheriting an unrelated target default.
			// This is target-only operational eligibility, never copied money.
			record["retirement_retry_at"] = nil
		}
		if legacyNodeAssets {
			// No source column AND no0059receipt: establish target-only future labels,
			// in the caller's owned target transaction. Never stamp old spool material.
			tag, err := model.NewNodeAssetTag()
			if err != nil {
				return err
			}
			record["asset_tag"] = tag
		}
		runtimeBytes, err := coverageRuntimeCopyRecord(table, record)
		if err != nil {
			return err
		}
		principalBytes, err := principalRuntimeCopyRecord(table, record)
		if err != nil {
			return err
		}
		runtimeBytes += principalBytes
		if runtimeBatchBytes+runtimeBytes > auxiliaryRuntimeCopyBatchBytes && len(batch) > 0 {
			if err := target.Table(table).Create(&batch).Error; err != nil {
				return fmt.Errorf("write bounded runtime target table %s: %w", table, err)
			}
			batch = make([]map[string]interface{}, 0, 200)
			runtimeBatchBytes = 0
		}
		runtimeBatchBytes += runtimeBytes
		batch = append(batch, record)
		if len(batch) == cap(batch) {
			if err := target.Table(table).Create(&batch).Error; err != nil {
				return fmt.Errorf("write target table %s: %w", table, err)
			}
			batch = make([]map[string]interface{}, 0, 200)
			runtimeBatchBytes = 0
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source table %s: %w", table, err)
	}
	if len(batch) > 0 {
		if err := target.Table(table).Create(&batch).Error; err != nil {
			return fmt.Errorf("write target table %s: %w", table, err)
		}
	}
	var sourceCount, targetCount int64
	if err := source.Table(table).Count(&sourceCount).Error; err != nil {
		return err
	}
	if err := target.Table(table).Count(&targetCount).Error; err != nil {
		return err
	}
	if sourceCount != targetCount {
		return fmt.Errorf("verify table %s: source=%d target=%d", table, sourceCount, targetCount)
	}
	return nil
}

func migrationTableSet(db *gorm.DB) (map[string]bool, error) {
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(tables))
	for _, table := range tables {
		result[table] = true
	}
	return result, nil
}
