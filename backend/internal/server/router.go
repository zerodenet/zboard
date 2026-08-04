package server

import (
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/handler"
	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zeromicro/go-zero/rest"
	"gorm.io/gorm"
)

func newRoute(method, path string, fn func(http.ResponseWriter, *http.Request)) rest.Route {
	return rest.Route{Method: method, Path: path, Handler: http.HandlerFunc(fn)}
}

func RegisterRoutes(srv *rest.Server, db *gorm.DB, jwtSecret string, credentialCipher *security.CredentialCipher, zeroArtifactDir, zeroKernelContract, zeroLocalVersion string) error {
	if err := datastore.ReconcileCommerceSchema(db); err != nil {
		return err
	}
	h, err := handler.NewHandlers(db, jwtSecret, credentialCipher, zeroArtifactDir, zeroKernelContract, zeroLocalVersion)
	if err != nil {
		return err
	}
	srv.Use(h.InstallationMiddleware)

	srv.AddRoutes([]rest.Route{
		newRoute(http.MethodGet, "/healthz", h.HealthHandler),
		newRoute(http.MethodGet, "/readyz", h.ReadyHandler),
		newRoute(http.MethodGet, "/api/v1/version", h.VersionHandler),
		newRoute(http.MethodGet, "/api/v1/setup/status", h.SetupStatusHandler),
		newRoute(http.MethodPost, "/api/v1/setup/install", h.SetupInstallHandler),
		newRoute(http.MethodPost, "/api/v1/auth/register", h.RegisterAuthRoutes),
		newRoute(http.MethodPost, "/api/v1/auth/login", h.LoginHandler),
		newRoute(http.MethodGet, "/api/v1/auth/me", h.MeHandler),
		newRoute(http.MethodGet, "/api/v1/tickets", h.TicketListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/tickets", h.TicketListHandler),
		newRoute(http.MethodPost, "/api/v1/tickets", h.TicketCreateHandler),
		newRoute(http.MethodGet, "/api/v1/tickets/:id", h.TicketGetHandler),
		newRoute(http.MethodGet, "/api/v1/admin/tickets/:id", h.TicketGetHandler),
		newRoute(http.MethodPost, "/api/v1/tickets/:id/messages", h.TicketReplyHandler),
		newRoute(http.MethodPost, "/api/v1/admin/tickets/:id/messages", h.TicketReplyHandler),
		newRoute(http.MethodPost, "/api/v1/tickets/:id/close", h.TicketCloseHandler),
		newRoute(http.MethodPut, "/api/v1/admin/tickets/:id/status", h.AdminTicketStatusHandler),
		newRoute(http.MethodGet, "/api/v1/admin/users", h.AdminUsersListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/users/:id", h.AdminUserGetHandler),
		newRoute(http.MethodPut, "/api/v1/admin/settings", h.AdminSettingsUpdateHandler),
		newRoute(http.MethodGet, "/api/v1/system/configs", h.PublicSystemConfigsHandler),
		newRoute(http.MethodGet, "/api/v1/admin/system-configs", h.AdminSystemConfigsListHandler),
		newRoute(http.MethodPut, "/api/v1/admin/system-configs/:key", h.AdminSystemConfigUpdateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/tasks", h.AdminTasksListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/tasks", h.AdminTaskCreateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/tasks/:id", h.AdminTaskGetHandler),
		newRoute(http.MethodGet, "/api/v1/admin/tasks/:id/items", h.AdminTaskItemsHandler),
		newRoute(http.MethodPost, "/api/v1/admin/tasks/:id/run", h.AdminTaskRunHandler),
		newRoute(http.MethodPost, "/api/v1/admin/users", h.AdminUserCreateHandler),
		newRoute(http.MethodPut, "/api/v1/admin/users/:id", h.AdminUserUpdateHandler),
		newRoute(http.MethodGet, "/api/v1/nodes", h.NodeListHandler),
		newRoute(http.MethodPost, "/api/v1/nodes", h.NodeCreateHandler),
		newRoute(http.MethodPost, "/api/v1/admin/node-operations", h.NodeBatchOperationHandler),
		newRoute(http.MethodGet, "/api/v1/nodes/:id", h.NodeDetailHandler),
		newRoute(http.MethodPut, "/api/v1/nodes/:id", h.NodeUpdateHandler),
		newRoute(http.MethodDelete, "/api/v1/nodes/:id", h.NodeDeleteHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/ssh/test", h.NodeSSHTestHandler),
		newRoute(http.MethodPut, "/api/v1/nodes/:id/ssh", h.NodeSSHConfigHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/ssh/host-key/reset", h.NodeSSHHostKeyResetHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/ssh/terminal-ticket", h.NodeSSHTerminalTicketHandler),
		newRoute(http.MethodGet, "/api/v1/nodes/:id/ssh/terminal", h.NodeSSHTerminalHandler),
		newRoute(http.MethodGet, "/api/v1/nodes/:id/kernel", h.NodeKernelStateHandler),
		newRoute(http.MethodGet, "/api/v1/nodes/:id/load", h.NodeLoadHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/kernel/detect", h.NodeKernelDetectHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/kernel/reconcile", h.NodeKernelReconcileHandler),
		newRoute(http.MethodGet, "/api/v1/admin/kernel/releases/latest", h.LatestKernelReleaseHandler),
		newRoute(http.MethodGet, "/api/v1/admin/kernel/releases", h.KernelReleasesHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/connector-credential", h.NodeConnectorCredentialRotateHandler),
		newRoute(http.MethodDelete, "/api/v1/nodes/:id/connector-credential", h.NodeConnectorCredentialRevokeHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/heartbeat", h.NodeConnectorHeartbeatHandler),
		newRoute(http.MethodGet, "/api/v1/nodes/:id/commands", h.NodeConnectorCommandsHandler),
		newRoute(http.MethodPost, "/api/v1/nodes/:id/report-credential", h.NodeReportCredentialRotateHandler),
		newRoute(http.MethodDelete, "/api/v1/nodes/:id/report-credential", h.NodeReportCredentialRevokeHandler),
		newRoute(http.MethodGet, "/api/v1/admin/certificates", h.ManagedCertificateListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/certificates", h.ManagedCertificateCreateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/certificates/:id", h.ManagedCertificateGetHandler),
		newRoute(http.MethodPut, "/api/v1/admin/certificates/:id", h.ManagedCertificateUpdateHandler),
		newRoute(http.MethodDelete, "/api/v1/admin/certificates/:id", h.ManagedCertificateDeleteHandler),
		newRoute(http.MethodPut, "/api/v1/admin/certificates/:id/renewal", h.ManagedCertificateRenewalUpdateHandler),
		newRoute(http.MethodPost, "/api/v1/admin/certificates/:id/issue", h.ManagedCertificateIssueHandler),
		newRoute(http.MethodPost, "/api/v1/admin/certificates/:id/renew", h.ManagedCertificateRenewHandler),
		newRoute(http.MethodGet, "/api/v1/admin/provider-definitions", h.ProviderDefinitionListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/provider-accounts", h.ProviderAccountListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/provider-accounts", h.ProviderAccountCreateHandler),
		newRoute(http.MethodPost, "/api/v1/admin/provider-accounts/:id/verify", h.ProviderAccountVerifyHandler),
		newRoute(http.MethodGet, "/api/v1/admin/dns-records", h.ManagedDNSListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/dns-records", h.ManagedDNSCreateHandler),
		newRoute(http.MethodPut, "/api/v1/admin/dns-records/:id", h.ManagedDNSUpdateHandler),
		newRoute(http.MethodDelete, "/api/v1/admin/dns-records/:id", h.ManagedDNSDeleteHandler),
		newRoute(http.MethodPost, "/api/v1/admin/dns-records/:id/sync", h.ManagedDNSSyncHandler),
		newRoute(http.MethodGet, "/api/v1/admin/protocol-endpoints", h.ProtocolEndpointListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/protocol-endpoints/selection", h.ProtocolEndpointSelectionHandler),
		newRoute(http.MethodPost, "/api/v1/admin/protocol-deployments/batch", h.ProtocolBatchDeployHandler),
		newRoute(http.MethodPatch, "/api/v1/admin/protocol-endpoints/batch", h.ProtocolBatchActiveHandler),
		newRoute(http.MethodPost, "/api/v1/admin/protocol-endpoints", h.ProtocolEndpointCreateHandler),
		newRoute(http.MethodPost, "/api/v1/admin/protocol-endpoints/reality-keypair", h.ProtocolRealityKeyPairHandler),
		newRoute(http.MethodPost, "/api/v1/admin/protocol-endpoints/reality-template", h.ProtocolRealityTemplateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/protocol-endpoints/:id", h.ProtocolEndpointDetailHandler),
		newRoute(http.MethodPut, "/api/v1/admin/protocol-endpoints/:id", h.ProtocolEndpointUpdateHandler),
		newRoute(http.MethodDelete, "/api/v1/admin/protocol-endpoints/:id", h.ProtocolEndpointDeleteHandler),
		newRoute(http.MethodPatch, "/api/v1/admin/protocol-endpoints/:id/multiplier", h.ProtocolEndpointMultiplierHandler),
		newRoute(http.MethodPost, "/api/v1/admin/protocol-endpoints/:id/deploy", h.ProtocolEndpointDeployHandler),
		newRoute(http.MethodGet, "/api/v1/admin/protocol-deployments", h.ProtocolDeploymentListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/node-groups", h.NodeGroupListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/node-groups", h.NodeGroupCreateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/node-groups/:id", h.NodeGroupDetailHandler),
		newRoute(http.MethodPut, "/api/v1/admin/node-groups/:id", h.NodeGroupUpdateHandler),
		newRoute(http.MethodGet, "/api/v1/plans", h.PlanListCommerceHandler),
		newRoute(http.MethodPost, "/api/v1/plans", h.PlanCreateCommerceHandler),
		newRoute(http.MethodGet, "/api/v1/plans/:id", h.PublicPlanDetailCommerceHandler),
		newRoute(http.MethodGet, "/api/v1/plans/:id/skus", h.PublicPlanSKUListCommerceHandler),
		newRoute(http.MethodGet, "/api/v1/admin/plans/:id", h.PlanDetailHandler),
		newRoute(http.MethodPut, "/api/v1/admin/plans/:id", h.PlanUpdateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/plans/:id/skus", h.PlanSKUListCommerceHandler),
		newRoute(http.MethodPost, "/api/v1/admin/plans/:id/skus", h.PlanSKUCreateCommerceHandler),
		newRoute(http.MethodGet, "/api/v1/admin/plan-skus/:id", h.PlanSKUGetCommerceHandler),
		newRoute(http.MethodPut, "/api/v1/admin/plan-skus/:id", h.PlanSKUUpdateCommerceHandler),
		newRoute(http.MethodGet, "/api/v1/orders", h.OrderListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/orders", h.OrderListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/orders/:id", h.AdminOrderGetHandler),
		newRoute(http.MethodGet, "/api/v1/admin/orders/:id/payment-events", h.AdminOrderPaymentEventsHandler),
		newRoute(http.MethodPost, "/api/v1/orders", h.OrderCreateCommerceHandler),
		newRoute(http.MethodPost, "/api/v1/orders/:id/pay", h.OrderPayHandler),
		newRoute(http.MethodPost, "/api/v1/admin/orders/:id/pay", h.OrderPayHandler),
		newRoute(http.MethodPost, "/api/v1/orders/:id/cancel", h.OrderCancelHandler),
		newRoute(http.MethodPost, "/api/v1/admin/orders/:id/cancel", h.OrderCancelHandler),
		newRoute(http.MethodPost, "/api/v1/orders/:id/pay-callback", h.OrderPayCallbackHandler),
		newRoute(http.MethodGet, "/api/v1/subscriptions", h.SubscriptionsHandler),
		newRoute(http.MethodGet, "/api/v1/admin/subscriptions", h.SubscriptionsHandler),
		newRoute(http.MethodGet, "/api/v1/admin/subscriptions/:id", h.AdminSubscriptionGetHandler),
		newRoute(http.MethodGet, "/api/v1/subscription/access", h.SubscriptionAccessHandler),
		newRoute(http.MethodGet, "/api/v1/subscription/protocol-loads", h.AccountProtocolLoadHandler),
		newRoute(http.MethodPost, "/api/v1/subscription/access/rotate", h.SubscriptionAccessRotateHandler),
		newRoute(http.MethodDelete, "/api/v1/subscription/access", h.SubscriptionAccessRevokeHandler),
		newRoute(http.MethodGet, "/api/v1/client/subscription/:token", h.ClientSubscriptionHandler),
		newRoute(http.MethodGet, "/api/v1/traffic/summary", h.TrafficSummaryHandler),
		newRoute(http.MethodGet, "/api/v1/admin/traffic/summary", h.TrafficSummaryHandler),
		newRoute(http.MethodGet, "/api/v1/traffic/records", h.TrafficRecordsHandler),
		newRoute(http.MethodGet, "/api/v1/admin/traffic/records", h.TrafficRecordsHandler),
		newRoute(http.MethodGet, "/api/v1/traffic/reconciliation", h.TrafficReconciliationHandler),
		newRoute(http.MethodGet, "/api/v1/admin/traffic/reconciliation", h.TrafficReconciliationHandler),
		newRoute(http.MethodPost, "/api/v1/traffic/report", h.TrafficReportHandler),
		newRoute(http.MethodPost, "/api/zero/events", h.ZeroEventHandler),
		newRoute(http.MethodGet, "/api/v1/admin/dashboard", h.DashboardHandler),
		newRoute(http.MethodGet, "/api/v1/admin/audit-logs", h.AuditLogsHandler),
		newRoute(http.MethodGet, "/api/v1/admin/audit-logs/:id", h.AuditLogDetailHandler),
		newRoute(http.MethodGet, "/api/v1/admin/operation-logs", h.OperationLogsHandler),
		newRoute(http.MethodGet, "/api/v1/admin/operation-logs/:source/:id", h.OperationLogDetailHandler),
		newRoute(http.MethodGet, "/api/v1/subscription-templates", h.SubscriptionTemplateListHandler),
		newRoute(http.MethodGet, "/api/v1/admin/subscription-rule-sets", h.AdminSubscriptionRuleSetListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/subscription-rule-sets", h.AdminSubscriptionRuleSetCreateHandler),
		newRoute(http.MethodGet, "/api/v1/admin/subscription-rule-sets/:id", h.AdminSubscriptionRuleSetGetHandler),
		newRoute(http.MethodPut, "/api/v1/admin/subscription-rule-sets/:id", h.AdminSubscriptionRuleSetUpdateHandler),
		newRoute(http.MethodDelete, "/api/v1/admin/subscription-rule-sets/:id", h.AdminSubscriptionRuleSetDeleteHandler),
		newRoute(http.MethodGet, "/api/v1/admin/subscription-templates", h.AdminSubscriptionTemplateListHandler),
		newRoute(http.MethodPost, "/api/v1/admin/subscription-templates", h.AdminSubscriptionTemplateCreateHandler),
		newRoute(http.MethodPost, "/api/v1/admin/subscription-templates/preview", h.AdminSubscriptionTemplatePreviewHandler),
		newRoute(http.MethodGet, "/api/v1/admin/subscription-templates/:id", h.AdminSubscriptionTemplateGetHandler),
		newRoute(http.MethodPut, "/api/v1/admin/subscription-templates/:id", h.AdminSubscriptionTemplateUpdateHandler),
		newRoute(http.MethodDelete, "/api/v1/admin/subscription-templates/:id", h.AdminSubscriptionTemplateDeleteHandler),
		newRoute(http.MethodGet, "/api/v1/system/info", h.SystemInfoHandler),
	})
	if err := h.ReconcileSystemConfigDefaults(); err != nil {
		return err
	}
	if err := h.ReconcileSubscriptionTemplateDefaults(); err != nil {
		return err
	}
	if err := h.ReconcileMieruEndpointCredentials(); err != nil {
		return err
	}
	h.StartCertificateRenewalWorker()
	h.StartDNSPublicObservationWorker()
	return nil
}
