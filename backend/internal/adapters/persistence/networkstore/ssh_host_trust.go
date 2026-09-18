package networkstore

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SSHHostTrust struct{ DB *gorm.DB }

func (s SSHHostTrust) PinSSHHostKey(ctx context.Context, nodeID uint, expected, observed string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "ssh_host_key_fingerprint").First(&node, nodeID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrSSHHostTrustNodeMissing
			}
			return err
		}
		stored := strings.TrimSpace(node.SSHHostKeyFingerprint)
		if expected != "" {
			if stored == "" {
				return errors.New("SSH host trust was reset while connecting; retry the connection to enroll the current host key")
			}
			if subtle.ConstantTimeCompare([]byte(stored), []byte(expected)) != 1 || subtle.ConstantTimeCompare([]byte(observed), []byte(expected)) != 1 {
				return fmt.Errorf("SSH host key changed while connecting: expected %s, received %s; verify the VPS identity before resetting trust", stored, observed)
			}
			return nil
		}
		if stored == "" {
			return tx.Model(&node).Update("ssh_host_key_fingerprint", observed).Error
		}
		if subtle.ConstantTimeCompare([]byte(stored), []byte(observed)) == 1 {
			return nil
		}
		return fmt.Errorf("SSH host key changed while it was being recorded: expected %s, received %s; verify the VPS identity before resetting trust", stored, observed)
	})
}
