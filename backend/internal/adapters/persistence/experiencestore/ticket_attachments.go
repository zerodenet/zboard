package experiencestore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func saveTicketAttachments(tx *gorm.DB, actor, messageID uint, inputs []experience.TicketAttachment) error {
	inputs, err := experience.NormalizeTicketAttachments(inputs)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		row := model.TicketAttachment{MessageID: messageID, Name: input.Name, URL: input.URL}
		if input.FileID != "" {
			var file model.StoredFile
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_id = ? AND purpose = ? AND deleted_at IS NULL", input.FileID, actor, "ticket").First(&file).Error; err != nil {
				return experience.ErrInvalid
			}
			var count int64
			if err := tx.Model(&model.TicketAttachment{}).Where("file_id = ?", file.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return experience.ErrFileInUse
			}
			row.FileID, row.Name, row.URL = &file.ID, file.Name, storedFile(file).URL()
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

type ticketMessageWithAttachments struct {
	model.TicketMessage
	AuthorEmail string
	Attachments []experience.TicketAttachment
}

func loadTicketMessages(tx *gorm.DB, id, beforeID uint, limit int) ([]ticketMessageWithAttachments, error) {
	// Limit messages before joining attachments, so attachment fan-out never
	// changes pagination and details retain their existing two-query budget.
	page := tx.Table("ticket_messages").Where("ticket_id = ?", id)
	if beforeID > 0 {
		page = page.Where("id < ?", beforeID)
	}
	page = page.Order("created_at DESC, id DESC").Limit(limit)
	type joined struct {
		model.TicketMessage
		AuthorEmail    string
		AttachmentID   uint
		FileID         string
		AttachmentName string
		AttachmentURL  string
		FileSize       int64
		ContentType    string
	}
	var rows []joined
	err := tx.Table("(?) AS m", page).Select("m.*, COALESCE(u.email, '') AS author_email, COALESCE(a.id, 0) AS attachment_id, COALESCE(a.file_id, '') AS file_id, COALESCE(a.name, '') AS attachment_name, COALESCE(a.url, '') AS attachment_url, COALESCE(f.size, 0) AS file_size, COALESCE(f.content_type, '') AS content_type").Joins("LEFT JOIN users u ON u.id = m.author_id").Joins("LEFT JOIN ticket_attachments a ON a.message_id = m.id").Joins("LEFT JOIN stored_files f ON f.id = a.file_id").Order("m.created_at DESC, m.id DESC, a.id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]ticketMessageWithAttachments, 0, limit)
	positions := map[uint]int{}
	for _, row := range rows {
		position, exists := positions[row.ID]
		if !exists {
			position = len(result)
			positions[row.ID] = position
			result = append(result, ticketMessageWithAttachments{TicketMessage: row.TicketMessage, AuthorEmail: row.AuthorEmail, Attachments: []experience.TicketAttachment{}})
		}
		if row.AttachmentID != 0 {
			result[position].Attachments = append(result[position].Attachments, experience.TicketAttachment{ID: row.AttachmentID, FileID: row.FileID, Name: row.AttachmentName, URL: row.AttachmentURL, Size: row.FileSize, ContentType: row.ContentType})
		}
	}
	return result, nil
}
