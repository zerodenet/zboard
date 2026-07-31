package server

import (
	"github.com/zeromicro/go-zero/rest"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/handler"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm"
)

func RegisterRoutes(srv *rest.Server, db *gorm.DB, jwtSecret string, credentialCipher *security.CredentialCipher, zeroArtifactDir, zeroKernelContract, zeroLocalVersion string) error {
	h, err := handler.NewHandlers(db, jwtSecret, credentialCipher, zeroArtifactDir, zeroKernelContract, zeroLocalVersion)
	if err != nil {
		return err
	}
	srv.Use(h.InstallationMiddleware)

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
			Method:  http.MethodGet,
			Path:    "/api/v1/setup/status",
			Handler: http.HandlerFunc(h.SetupStatusHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/setup/install",
			Handler: http.HandlerFunc(h.SetupInstallHandler),
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
			Path:    "/api/v1/tickets",
			Handler: http.HandlerFunc(h.TicketListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/tickets",
			Handler: http.HandlerFunc(h.TicketListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/tickets",
			Handler: http.HandlerFunc(h.TicketCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/tickets/:id",
			Handler: http.HandlerFunc(h.TicketGetHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/tickets/:id",
			Handler: http.HandlerFunc(h.TicketGetHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/tickets/:id/messages",
			Handler: http.HandlerFunc(h.TicketReplyHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/tickets/:id/messages",
			Handler: http.HandlerFunc(h.TicketReplyHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/tickets/:id/close",
			Handler: http.HandlerFunc(h.TicketCloseHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/tickets/:id/status",
			Handler: http.HandlerFunc(h.AdminTicketStatusHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/users",
			Handler: http.HandlerFunc(h.AdminUsersListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/users/:id",
			Handler: http.HandlerFunc(h.AdminUserGetHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/settings",
			Handler: http.HandlerFunc(h.AdminSettingsUpdateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/system/configs",
			Handler: http.HandlerFunc(h.PublicSystemConfigsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/system-configs",
			Handler: http.HandlerFunc(h.AdminSystemConfigsListHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/system-configs/:key",
			Handler: http.HandlerFunc(h.AdminSystemConfigUpdateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/tasks",
			Handler: http.HandlerFunc(h.AdminTasksListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/tasks",
			Handler: http.HandlerFunc(h.AdminTaskCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/tasks/:id",
			Handler: http.HandlerFunc(h.AdminTaskGetHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/tasks/:id/items",
			Handler: http.HandlerFunc(h.AdminTaskItemsHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/tasks/:id/run",
			Handler: http.HandlerFunc(h.AdminTaskRunHandler),
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
			Path:    "/api/v1/admin/node-operations",
			Handler: http.HandlerFunc(h.NodeBatchOperationHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/nodes/:id",
			Handler: http.HandlerFunc(h.NodeDetailHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/nodes/:id",
			Handler: http.HandlerFunc(h.NodeUpdateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/ssh/test",
			Handler: http.HandlerFunc(h.NodeSSHTestHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/nodes/:id/ssh",
			Handler: http.HandlerFunc(h.NodeSSHConfigHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/ssh/host-key/reset",
			Handler: http.HandlerFunc(h.NodeSSHHostKeyResetHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/ssh/terminal-ticket",
			Handler: http.HandlerFunc(h.NodeSSHTerminalTicketHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/nodes/:id/ssh/terminal",
			Handler: http.HandlerFunc(h.NodeSSHTerminalHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/nodes/:id/kernel",
			Handler: http.HandlerFunc(h.NodeKernelStateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/kernel/detect",
			Handler: http.HandlerFunc(h.NodeKernelDetectHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/kernel/reconcile",
			Handler: http.HandlerFunc(h.NodeKernelReconcileHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/kernel/releases/latest",
			Handler: http.HandlerFunc(h.LatestKernelReleaseHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/kernel/releases",
			Handler: http.HandlerFunc(h.KernelReleasesHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/connector-credential",
			Handler: http.HandlerFunc(h.NodeConnectorCredentialRotateHandler),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/v1/nodes/:id/connector-credential",
			Handler: http.HandlerFunc(h.NodeConnectorCredentialRevokeHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/heartbeat",
			Handler: http.HandlerFunc(h.NodeConnectorHeartbeatHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/nodes/:id/commands",
			Handler: http.HandlerFunc(h.NodeConnectorCommandsHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/nodes/:id/report-credential",
			Handler: http.HandlerFunc(h.NodeReportCredentialRotateHandler),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/v1/nodes/:id/report-credential",
			Handler: http.HandlerFunc(h.NodeReportCredentialRevokeHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/certificates",
			Handler: http.HandlerFunc(h.ManagedCertificateListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/certificates",
			Handler: http.HandlerFunc(h.ManagedCertificateCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/certificates/:id",
			Handler: http.HandlerFunc(h.ManagedCertificateGetHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/certificates/:id/renewal",
			Handler: http.HandlerFunc(h.ManagedCertificateRenewalUpdateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/certificates/:id/issue",
			Handler: http.HandlerFunc(h.ManagedCertificateIssueHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/certificates/:id/renew",
			Handler: http.HandlerFunc(h.ManagedCertificateRenewHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/provider-definitions",
			Handler: http.HandlerFunc(h.ProviderDefinitionListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/provider-accounts",
			Handler: http.HandlerFunc(h.ProviderAccountListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/provider-accounts",
			Handler: http.HandlerFunc(h.ProviderAccountCreateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/provider-accounts/:id/verify",
			Handler: http.HandlerFunc(h.ProviderAccountVerifyHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/dns-records",
			Handler: http.HandlerFunc(h.ManagedDNSListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/dns-records",
			Handler: http.HandlerFunc(h.ManagedDNSCreateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/dns-records/:id/sync",
			Handler: http.HandlerFunc(h.ManagedDNSSyncHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/protocol-endpoints",
			Handler: http.HandlerFunc(h.ProtocolEndpointListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/protocol-endpoints/selection",
			Handler: http.HandlerFunc(h.ProtocolEndpointSelectionHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/protocol-deployments/batch",
			Handler: http.HandlerFunc(h.ProtocolBatchDeployHandler),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/v1/admin/protocol-endpoints/batch",
			Handler: http.HandlerFunc(h.ProtocolBatchActiveHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/protocol-endpoints",
			Handler: http.HandlerFunc(h.ProtocolEndpointCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/protocol-endpoints/:id",
			Handler: http.HandlerFunc(h.ProtocolEndpointDetailHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/protocol-endpoints/:id",
			Handler: http.HandlerFunc(h.ProtocolEndpointUpdateHandler),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/v1/admin/protocol-endpoints/:id/multiplier",
			Handler: http.HandlerFunc(h.ProtocolEndpointMultiplierHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/protocol-endpoints/:id/deploy",
			Handler: http.HandlerFunc(h.ProtocolEndpointDeployHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/protocol-deployments",
			Handler: http.HandlerFunc(h.ProtocolDeploymentListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/node-groups",
			Handler: http.HandlerFunc(h.NodeGroupListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/node-groups",
			Handler: http.HandlerFunc(h.NodeGroupCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/node-groups/:id",
			Handler: http.HandlerFunc(h.NodeGroupDetailHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/node-groups/:id",
			Handler: http.HandlerFunc(h.NodeGroupUpdateHandler),
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
			Method:  http.MethodGet,
			Path:    "/api/v1/plans/:id",
			Handler: http.HandlerFunc(h.PublicPlanDetailHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/plans/:id/skus",
			Handler: http.HandlerFunc(h.PublicPlanSKUListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/plans/:id",
			Handler: http.HandlerFunc(h.PlanDetailHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/plans/:id",
			Handler: http.HandlerFunc(h.PlanUpdateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/plans/:id/skus",
			Handler: http.HandlerFunc(h.PlanSKUListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/plans/:id/skus",
			Handler: http.HandlerFunc(h.PlanSKUCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/plan-skus/:id",
			Handler: http.HandlerFunc(h.PlanSKUGetHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/plan-skus/:id",
			Handler: http.HandlerFunc(h.PlanSKUUpdateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/orders",
			Handler: http.HandlerFunc(h.OrderListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/orders",
			Handler: http.HandlerFunc(h.OrderListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/orders/:id",
			Handler: http.HandlerFunc(h.AdminOrderGetHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/orders/:id/payment-events",
			Handler: http.HandlerFunc(h.AdminOrderPaymentEventsHandler),
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
			Path:    "/api/v1/admin/orders/:id/pay",
			Handler: http.HandlerFunc(h.OrderPayHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/orders/:id/cancel",
			Handler: http.HandlerFunc(h.OrderCancelHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/orders/:id/cancel",
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
			Path:    "/api/v1/admin/subscriptions",
			Handler: http.HandlerFunc(h.SubscriptionsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/subscriptions/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionGetHandler),
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
			Path:    "/api/v1/admin/traffic/summary",
			Handler: http.HandlerFunc(h.TrafficSummaryHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/traffic/records",
			Handler: http.HandlerFunc(h.TrafficRecordsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/traffic/records",
			Handler: http.HandlerFunc(h.TrafficRecordsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/traffic/reconciliation",
			Handler: http.HandlerFunc(h.TrafficReconciliationHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/traffic/reconciliation",
			Handler: http.HandlerFunc(h.TrafficReconciliationHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/traffic/report",
			Handler: http.HandlerFunc(h.TrafficReportHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/zero/events",
			Handler: http.HandlerFunc(h.ZeroEventHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/dashboard",
			Handler: http.HandlerFunc(h.DashboardHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/audit-logs",
			Handler: http.HandlerFunc(h.AuditLogsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/audit-logs/:id",
			Handler: http.HandlerFunc(h.AuditLogDetailHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/operation-logs",
			Handler: http.HandlerFunc(h.OperationLogsHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/operation-logs/:source/:id",
			Handler: http.HandlerFunc(h.OperationLogDetailHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/subscription-templates",
			Handler: http.HandlerFunc(h.SubscriptionTemplateListHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/subscription-rule-sets",
			Handler: http.HandlerFunc(h.AdminSubscriptionRuleSetListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/subscription-rule-sets",
			Handler: http.HandlerFunc(h.AdminSubscriptionRuleSetCreateHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/subscription-rule-sets/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionRuleSetGetHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/subscription-rule-sets/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionRuleSetUpdateHandler),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/v1/admin/subscription-rule-sets/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionRuleSetDeleteHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/subscription-templates",
			Handler: http.HandlerFunc(h.AdminSubscriptionTemplateListHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/subscription-templates",
			Handler: http.HandlerFunc(h.AdminSubscriptionTemplateCreateHandler),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/admin/subscription-templates/preview",
			Handler: http.HandlerFunc(h.AdminSubscriptionTemplatePreviewHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/admin/subscription-templates/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionTemplateGetHandler),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/v1/admin/subscription-templates/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionTemplateUpdateHandler),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/v1/admin/subscription-templates/:id",
			Handler: http.HandlerFunc(h.AdminSubscriptionTemplateDeleteHandler),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/system/info",
			Handler: http.HandlerFunc(h.SystemInfoHandler),
		},
	})
	if err := h.ReconcileMieruEndpointCredentials(); err != nil {
		return err
	}
	h.StartCertificateRenewalWorker()
	h.StartDNSPublicObservationWorker()
	return nil
}
