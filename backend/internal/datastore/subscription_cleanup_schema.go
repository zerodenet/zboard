package datastore

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

const subscriptionCleanupVersion = "0092_subscription_hard_cleanup.up.sql"

// Financial IDs and metering cursors outlive a live service. Only these four
// historical references lose their dependency on the mutable service row. All
// wallet/order/receipt owner constraints and runtime-child cascades stay intact.
var subscriptionHistoryReferences = map[string]string{
	"orders": "target_subscription_id", "protocol_credentials": "subscription_id",
	"flow_usages": "subscription_id", "subscription_value_settlements": "subscription_id",
}

func reconcileSubscriptionCleanupSchema(db *gorm.DB) error {
	var applied int64
	if e := db.Table("schema_migrations").Where("version=?", subscriptionCleanupVersion).Count(&applied).Error; e != nil {
		return e
	}
	if !db.Migrator().HasColumn("account_subjects", "last_entitlement_end") {
		if applied > 0 {
			return errors.New("recorded subscription cleanup anchor missing")
		}
		kind := "DATETIME(3)"
		if IsSQLite(db) {
			kind = "datetime"
		}
		if e := db.Exec("ALTER TABLE account_subjects ADD COLUMN last_entitlement_end " + kind + " NULL").Error; e != nil {
			return e
		}
	}
	for table, column := range subscriptionHistoryReferences {
		if IsSQLite(db) {
			// SQLite fresh schemas have no such relation except value settlements;
			// the migrator rebuilds only a legacy table that still has this exact FK.
			var ddl string
			if e := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&ddl).Error; e != nil {
				return e
			}
			if !strings.Contains(strings.ToLower(ddl), "references `subscriptions`") && !strings.Contains(strings.ToLower(ddl), "references subscriptions") && !strings.Contains(strings.ToLower(ddl), `references "subscriptions"`) {
				continue
			}
			var keys []struct {
				Table string
				From  string
			}
			if e := db.Raw("PRAGMA foreign_key_list(`" + table + "`)").Scan(&keys).Error; e != nil {
				return e
			}
			for _, k := range keys {
				if k.Table == "subscriptions" && k.From == column {
					if applied > 0 {
						return fmt.Errorf("historical subscription FK restored: %s", table)
					}
					name := "fk_" + table + "_subscription"
					if table == "orders" {
						name = "fk_orders_target_subscription"
					}
					if e := db.Migrator().DropConstraint(table, name); e != nil {
						return e
					}
				}
			}
		} else {
			var names []string
			if e := db.Raw("SELECT constraint_name FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND table_name=? AND column_name=? AND referenced_table_name='subscriptions'", table, column).Scan(&names).Error; e != nil {
				return e
			}
			for _, name := range names {
				if applied > 0 {
					return fmt.Errorf("historical subscription FK restored: %s", table)
				}
				if strings.Contains(name, "`") {
					return errors.New("invalid subscription constraint")
				}
				if e := nativeSettlementDDL(db, "ALTER TABLE `"+table+"` DROP FOREIGN KEY `"+name+"`, ALGORITHM=INPLACE, LOCK=NONE"); e != nil {
					return e
				}
			}
		}
	}
	if applied == 0 {
		if e := db.Table("system_configs").Where("config_key=? AND description=?", "subscription_renewal_grace_days", "到期后停服，可在此天数内续费恢复；默认7天，范围0–3650，0表示不保留恢复窗口。缩短窗口会使超过新期限的旧订阅结束并清理凭据，历史订单和账务保留。主动退订不进入恢复窗口。").UpdateColumn("description", "到期后停服，窗口内可续费恢复；默认7天，范围0–3650。超出后自动删除订阅、链接、成员及自动续费配置，历史订单、账务和最小补报依据保留。0表示到期即删除；已取消订阅直接清理。").Error; e != nil {
			return e
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: subscriptionCleanupVersion, AppliedAt: time.Now().UTC()}).Error
}
