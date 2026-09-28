package handler

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"testing"
	"time"
)

func TestAdminDirectoryCombinesVerificationAndRoleBeforePaging(t *testing.T) {
	f := newCatalogFixture(t)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		user := model.User{Email: fmt.Sprintf("filter-%d@example.test", i), AccountName: fmt.Sprintf("filter-%d", i), Password: "test", Status: "active", IsAdmin: i == 3}
		if i < 2 {
			user.EmailVerifiedAt = &now
		}
		if err := f.h.db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query string
		total int64
		email string
	}{
		{"email_verified=true&is_admin=false&offset=1", 2, "filter-0@example.test"},
		{"email_verified=false&is_admin=false&offset=0", 1, "filter-2@example.test"},
		{"email_verified=false&is_admin=true&offset=0", 1, "filter-3@example.test"},
	} {
		var page struct {
			Items []adminUserListItem
			Total int64
		}
		path := "/api/v1/admin/users?paged=true&q=filter-&sort=id&direction=desc&limit=1&" + tc.query
		if status := f.get(t, path, f.h.AdminUsersListHandler, &page); status != http.StatusOK {
			t.Fatalf("%s status %d", path, status)
		}
		if page.Total != tc.total || len(page.Items) != 1 || page.Items[0].Email != tc.email {
			t.Fatalf("%s: %+v", path, page)
		}
	}
	if status := f.get(t, "/api/v1/admin/users?email_verified=invalid", f.h.AdminUsersListHandler, nil); status != http.StatusBadRequest {
		t.Fatalf("invalid bool status %d", status)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if status := f.get(t, "/api/v1/admin/users?email_verified=false", f.h.AdminUsersListHandler, nil); status != http.StatusForbidden {
		t.Fatalf("non-admin status %d", status)
	}
}

func TestAdminDirectoryCountsExhaustedMonthlySubscriptionAsValid(t *testing.T) {
	f := newOrderFixture(t)
	now := time.Now().UTC()
	for _, tc := range []struct {
		status string
		end    time.Time
		once   bool
	}{
		{"active", now.Add(time.Hour), false},
		{"active", now.Add(-time.Hour), false},
		{"canceled", now.Add(time.Hour), false},
		{"active", now.Add(time.Hour), true},
	} {
		sub := model.Subscription{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: tc.status, StartAt: now.Add(-24 * time.Hour), EndAt: tc.end, FlowTotal: 100, FlowUsed: 100, EndsOnQuotaExhaustion: tc.once, Config: "{}"}
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := f.h.services.Identity.Directory
	page, err := service.List(context.Background(), identity.AccountDirectoryQuery{Paged: true, Limit: 50})
	if err != nil || len(page.Items) != 1 || page.Items[0].ActiveSubscriptionCount != 1 || page.Items[0].TotalSubscriptionCount != 4 {
		t.Fatalf("list: %+v %v", page, err)
	}
	detail, err := service.Detail(context.Background(), 1)
	if err != nil || detail.ActiveSubscriptionCount != 1 || detail.TotalSubscriptionCount != 4 {
		t.Fatalf("detail: %+v %v", detail, err)
	}
}
