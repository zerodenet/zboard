package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Confirmation is a state binding, not an authorization token. Only the normal
// authenticated administrator import endpoint may submit it. No global trust
// entry or historical signed archive is renamed by this operation.
type PublisherMigration struct {
	FromPublisher string `json:"from_publisher"`
	ToPublisher   string `json:"to_publisher"`
	FromVersionID string `json:"from_version_id"`
	PreviousState string `json:"previous_state"`
	Generation    uint64 `json:"generation"`
	Confirmation  string `json:"confirmation"`
}

func publisherMigration(prev model.PluginInstallation, p *Package) *PublisherMigration {
	if prev.ID == "" || prev.Publisher == p.Publisher {
		return nil
	}
	// Include configuration revision so a confirmation cannot race an operator
	// editing configuration or clearing retained data after inspection.
	raw, _ := json.Marshal([]any{prev.ID, prev.Publisher, prev.SigningKey, prev.VersionID, prev.Generation, prev.ConfigRevision, prev.State, prev.Enabled, prev.LocalTrust, p.Publisher, p.Digest})
	sum := sha256.Sum256(raw)
	return &PublisherMigration{FromPublisher: prev.Publisher, ToPublisher: p.Publisher, FromVersionID: prev.VersionID, PreviousState: prev.State, Generation: prev.Generation, Confirmation: hex.EncodeToString(sum[:])}
}

func confirmPublisherMigration(prev model.PluginInstallation, p *Package, confirmed bool, tokens []string) error {
	migration := publisherMigration(prev, p)
	if migration == nil {
		if len(tokens) > 0 && tokens[0] != "" {
			return ErrConflict
		}
		return nil
	}
	if len(tokens) == 0 || tokens[0] == "" {
		return errors.New("请明确确认同签名密钥的发布者迁移后再安装")
	}
	if !confirmed || tokens[0] != migration.Confirmation {
		return ErrConflict
	}
	return nil
}

func (m *Manager) newImportOperation(prev model.PluginInstallation, p *Package, actor string) (model.PluginOperation, error) {
	if publisherMigration(prev, p) == nil {
		return m.newOperation(p.Manifest.ID, "import", actor)
	}
	op := model.PluginOperation{ID: uuid.NewString(), PluginID: p.Manifest.ID, Action: "publisher_migration", Actor: actor, State: "running", PreviousPublisher: prev.Publisher, Publisher: p.Publisher, SigningKeyFingerprint: keyFingerprint(p.PublicKey), PackageDigest: p.Digest}
	return op, m.db.Create(&op).Error
}

// Recheck inside the installation transaction after candidate validation. A
// replaced host, stale snapshot or concurrent lifecycle/configuration mutation
// must not be overwritten by the candidate's saved installation row.
func lockPreviousInstallation(tx *gorm.DB, prev model.PluginInstallation, id string) error {
	var current model.PluginInstallation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Limit(1).Find(&current).Error; err != nil {
		return err
	}
	if current.ID != prev.ID || current.Generation != prev.Generation || current.ConfigRevision != prev.ConfigRevision || current.Publisher != prev.Publisher || current.SigningKey != prev.SigningKey || current.VersionID != prev.VersionID || current.State != prev.State || current.Enabled != prev.Enabled || current.LocalTrust != prev.LocalTrust {
		return ErrConflict
	}
	return nil
}
