package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Caller holds m.mu. Only the lease owner may remove package files. Metadata
// for an uninstalled plugin remains available for configuration and data purge,
// but its archive is no longer needed to run the plugin.
func (m *Manager) prunePackageStorageLocked() error {
	if err := m.guard(m.db); err != nil {
		return err
	}
	var installations []model.PluginInstallation
	if err := m.db.Find(&installations).Error; err != nil {
		return err
	}
	keepRows := make([]string, 0, len(installations))
	keepFiles := make(map[string]bool, len(installations))
	for _, installation := range installations {
		if !digestPattern.MatchString(installation.VersionID) {
			return errors.New("invalid stored plugin version digest")
		}
		keepRows = append(keepRows, installation.VersionID)
		if installation.State != "uninstalled" {
			keepFiles[installation.VersionID] = true
		}
	}
	if err := m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		query := tx.Model(&model.PluginVersion{})
		if len(keepRows) == 0 {
			query = query.Where("id <> ?", "")
		} else {
			query = query.Where("id NOT IN ?", keepRows)
		}
		return query.Delete(&model.PluginVersion{}).Error
	}); err != nil {
		return err
	}
	root := filepath.Join(m.options.Directory, "versions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var cleanupErr error
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		switch {
		case strings.HasPrefix(name, ".import-"):
			cleanupErr = errors.Join(cleanupErr, os.RemoveAll(path))
		case digestPattern.MatchString(name) && !keepFiles[name]:
			cleanupErr = errors.Join(cleanupErr, os.RemoveAll(path))
		case digestPattern.MatchString(name) && keepFiles[name]:
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				cleanupErr = errors.Join(cleanupErr, errors.New("unsafe current plugin version directory"))
				continue
			}
			children, err := os.ReadDir(path)
			if err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
				continue
			}
			for _, child := range children {
				if strings.HasPrefix(child.Name(), ".repair-") {
					cleanupErr = errors.Join(cleanupErr, os.RemoveAll(filepath.Join(path, child.Name())))
				}
			}
		}
	}
	return cleanupErr
}
