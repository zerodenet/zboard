package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestMessageTemplatesPreserveAuthorityRevisionAndSystemOwnership(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	actor := model.User{Email: "template-admin@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	active := false
	in := messaging.TemplateWrite{Name: " New ", Slug: " NEW ", Category: "operational", SubjectTemplate: "Hello {{user_email}}", BodyTemplate: "Body", IsActive: &active}
	service := h.services.MessageTemplates
	row, err := service.Save(ctx, actor.ID, 0, in)
	if err != nil || row.IsActive || row.Slug != "new" {
		t.Fatal(row, err)
	}
	var persisted model.EmailTemplate
	if err := h.db.First(&persisted, row.ID).Error; err != nil || persisted.IsActive {
		t.Fatal("disabled default overridden", persisted, err)
	}
	revision := row.Revision
	in.ExpectedRevision = &revision
	if _, err := service.Save(ctx, actor.ID, row.ID, in); err != nil {
		t.Fatal(err)
	}
	_, err = service.Save(ctx, actor.ID, row.ID, in)
	var conflict *messaging.TemplateConflict
	if !errors.As(err, &conflict) || conflict.CurrentRevision != revision+1 {
		t.Fatal("stale revision accepted", err)
	}
	registration := model.EmailTemplate{Name: "Registration", Slug: "registration", Category: "registration", SubjectTemplate: "Hello", BodyTemplate: "Body", Revision: 1}
	if err := h.db.Create(&registration).Error; err != nil {
		t.Fatal(err)
	}
	in.ExpectedRevision = nil
	updated, err := service.Save(ctx, actor.ID, registration.ID, in)
	if err != nil || updated.Category != "registration" || updated.Slug != "registration" {
		t.Fatal("system identity changed", updated, err)
	}
	if err := service.Delete(ctx, actor.ID, registration.ID); !errors.Is(err, messaging.ErrTemplateProtected) {
		t.Fatal(err)
	}
	if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(ctx, actor.ID, ""); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked administrator listed templates", err)
	}
	if _, err := service.Preview(ctx, actor.ID, messaging.TemplatePreviewInput{Category: "operational", SubjectTemplate: "Hello", BodyTemplate: "Body"}); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked administrator previewed", err)
	}
	if err := service.Delete(ctx, actor.ID, row.ID); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked administrator deleted", err)
	}
}
