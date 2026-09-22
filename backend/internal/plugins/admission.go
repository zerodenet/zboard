package plugins

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const StorageCapability = "zboard.storage.v1"
const ConfigCapability = "zboard.config.v1"
const StorageReadCapability = "zboard.storage.read.v1"
const StorageWriteCapability = "zboard.storage.write.v1"
const ConfigReadCapability = "zboard.config.read.v1"
const ConfigWriteCapability = "zboard.config.write.v1"
const PageCapability = "zboard.ui.page.v1"
const HTTPRouteCapability = "zboard.http.route.v1"
const AccountAssertionCapability = "zboard.account.assertion.v1"
const SubscriptionProjectionCapability = "zboard.subscription.projection.v1"
const MessageProjectionCapability = "zboard.message.projection.v1"
const AccountSelfReadCapability = "zboard.account.self.read.v1"
const AccountAdminReadCapability = "zboard.account.admin.read.v1"
const SubscriptionReadCapability = "zboard.subscription.read.v1"
const SubscriptionConfigReadCapability = "zboard.subscription.config.read.v1"
const SubscriptionAdminReadCapability = "zboard.subscription.admin.read.v1"
const SubscriptionQuotaWriteCapability = "zboard.subscription.quota.write.v1"
const SubscriptionTermWriteCapability = "zboard.subscription.term.write.v1"
const SubscriptionStatusWriteCapability = "zboard.subscription.status.write.v1"
const MessageReadCapability = "zboard.message.read.v1"
const MessageAckCapability = "zboard.message.ack.v1"
const UISlotCapability = "zboard.ui.slot.v1"
const HostDiscoveryCapability = "zboard.host.discovery.v1"

var ErrPermission = errors.New("plugin capability is outside the host admission scope")

// Admission is a host-owned receipt, never an administrator-editable permission set.
type Admission struct {
	Accepted     bool     `json:"accepted"`
	Capabilities []string `json:"capabilities"`
}

func hasCapability(v Installation, capability string) bool {
	return v.Admission.Accepted && slices.Contains(v.Admission.Capabilities, capability) && slices.Contains(v.Manifest.Capabilities, capability)
}
func configCanRead(v Installation) bool {
	return hasCapability(v, ConfigCapability) || hasCapability(v, ConfigReadCapability)
}
func configCanWrite(v Installation) bool {
	return hasCapability(v, ConfigCapability) || hasCapability(v, ConfigWriteCapability)
}
func storageCanRead(v Installation) bool {
	return hasCapability(v, StorageCapability) || hasCapability(v, StorageReadCapability)
}
func storageCanWrite(v Installation) bool {
	return hasCapability(v, StorageCapability) || hasCapability(v, StorageWriteCapability)
}
func (m *Manager) loadAdmission(v *Installation) error {
	v.Admission.Capabilities = []string{}
	var row model.PluginAuthorization
	result := m.db.Where("plugin_id = ?", v.ID).Limit(1).Find(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	var capabilities []string
	if err := json.Unmarshal([]byte(row.Capabilities), &capabilities); err != nil {
		return err
	}
	v.Admission.Accepted = row.Digest == v.Digest && slices.Equal(capabilities, v.Manifest.Capabilities) && (v.Manifest.Components.Server == nil || row.NativeTrusted)
	if v.Admission.Accepted {
		v.Admission.Capabilities = capabilities
	}
	return nil
}
func hostAdmission(manifest Manifest) (Admission, error) {
	if err := manifest.Validate(); err != nil {
		return Admission{}, err
	}
	return Admission{Accepted: true, Capabilities: append([]string{}, manifest.Capabilities...)}, nil
}
func storeAdmission(tx *gorm.DB, v Installation, actor string) error {
	raw, _ := json.Marshal(v.Admission.Capabilities)
	// Keep the existing table/columns for database compatibility. The only writer
	// is now the host lifecycle transaction after signed-package validation.
	row := model.PluginAuthorization{PluginID: v.ID, Digest: v.Digest, Capabilities: string(raw), NativeTrusted: v.Manifest.Components.Server != nil, Actor: actor}
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
}
func executionAuthorized(v Installation) error {
	if !v.Admission.Accepted {
		return ErrPermission
	}
	if v.Manifest.Components.Server != nil && !configCanRead(v) && !configCanWrite(v) {
		return ErrPermission
	}
	return nil
}
