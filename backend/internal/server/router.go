package server

import (
	"github.com/zeromicro/go-zero/rest"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/handler"
	"gorm.io/gorm"
)

func RegisterRoutes(srv *rest.Server, db *gorm.DB, jwtSecret string) {
	h := handler.NewHandlers(db, jwtSecret)

	srv.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/healthz",
			Handler: http.HandlerFunc(h.HealthHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/readyz",
			Handler: http.HandlerFunc(h.ReadyHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/version",
			Handler: http.HandlerFunc(h.VersionHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/auth/register",
			Handler: http.HandlerFunc(h.RegisterAuthRoutes),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/auth/login",
			Handler: http.HandlerFunc(h.LoginHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/auth/me",
			Handler: http.HandlerFunc(h.MeHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/users",
			Handler: http.HandlerFunc(h.AdminUsersListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/users",
			Handler: http.HandlerFunc(h.AdminUserCreateHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/users/:id",
			Handler: http.HandlerFunc(h.AdminUserUpdateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/nodes",
			Handler: http.HandlerFunc(h.NodeListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes",
			Handler: http.HandlerFunc(h.NodeCreateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/ssh/test",
			Handler: http.HandlerFunc(h.NodeSSHTestHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/protocol/config",
			Handler: http.HandlerFunc(h.NodeProtocolConfigHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/plans",
			Handler: http.HandlerFunc(h.PlanListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/plans",
			Handler: http.HandlerFunc(h.PlanCreateHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/plans/:id",
			Handler: http.HandlerFunc(h.PlanUpdateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/orders",
			Handler: http.HandlerFunc(h.OrderListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/orders",
			Handler: http.HandlerFunc(h.OrderCreateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/orders/:id/pay",
			Handler: http.HandlerFunc(h.OrderPayHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/orders/:id/cancel",
			Handler: http.HandlerFunc(h.OrderCancelHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/orders/:id/pay-callback",
			Handler: http.HandlerFunc(h.OrderPayCallbackHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/subscriptions",
			Handler: http.HandlerFunc(h.SubscriptionsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/subscription/access",
			Handler: http.HandlerFunc(h.SubscriptionAccessHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/subscription/access/rotate",
			Handler: http.HandlerFunc(h.SubscriptionAccessRotateHandler),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/v1/subscription/access",
			Handler: http.HandlerFunc(h.SubscriptionAccessRevokeHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/client/subscription/:token",
			Handler: http.HandlerFunc(h.ClientSubscriptionHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/traffic/summary",
			Handler: http.HandlerFunc(h.TrafficSummaryHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/traffic/records",
			Handler: http.HandlerFunc(h.TrafficRecordsHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/traffic/report",
			Handler: http.HandlerFunc(h.TrafficReportHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/dashboard",
			Handler: http.HandlerFunc(h.DashboardHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/system/info",
			Handler: http.HandlerFunc(h.SystemInfoHandler),
		},
	})
}
