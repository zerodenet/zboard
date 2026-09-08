package handler

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"net"
	"strings"
)

func networkEntryAddressValid(value string) bool {
	if net.ParseIP(value) != nil {
		return true
	}
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
func networkEntryPortAvailable(tx *gorm.DB, nodeID uint, port int, entryID uint) error {
	for _, query := range []*gorm.DB{tx.Model(&model.NetworkEntry{}).Where("node_id = ? AND port = ? AND id <> ?", nodeID, port, entryID), tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND port = ?", nodeID, port), tx.Model(&model.ProtocolCredential{}).Where("node_id = ? AND listen_port = ?", nodeID, port)} {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("A 的端口 %d 已被协议、凭据或其他入口占用", port)
		}
	}
	return nil
}
