// Package application composes core capabilities with their infrastructure.
// HTTP and plugin adapters consume these services; they do not create workers.
package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/experiencestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"log"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observabilitystore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"gorm.io/gorm"
)

type Services struct {
	MessageTemplates            messaging.Templates
	PrincipalCollection         metering.PrincipalCollection
	FairUseObservationSeries    metering.ObservationSeriesService
	FairUseFlowCollection       metering.FlowCollection
	FairUseEvaluationRequests   metering.EvaluationRequests
	FairUseEvaluator            metering.Evaluator
	FairUseTelemetry            metering.Telemetry
	FairUseEvaluationSource     metering.EvaluationSource
	FairUseObservations         metering.Observations
	FairUsePolicies             metering.Policies
	DeliveryOrder               network.DeliveryOrder
	ProtocolEndpointMultiplier  network.ProtocolEndpointMultiplier
	ProxyPoolQueries            network.ProxyPoolQueries
	NetworkEntryQueries         network.NetworkEntryQueries
	NodeGroupMutations          network.NodeGroupMutations
	NodeAdministration          network.NodeAdministration
	NetworkInventory            network.Inventory
	ManagedPublicationInventory network.ManagedPublicationInventory
	NativeOperationStatus       network.NativeOperationStatus
	DNSDeletion                 network.DNSDeletion
	NodeActivity                network.NodeActivity
	EventCredentials            network.EventCredentials
	SSHHostTrust                network.SSHHostTrust
	ProtocolEndpointRemoval     network.ProtocolEndpointRemoval
	PublicationRequests         network.PublicationRequests
	CertificateLifecycle        network.CertificateLifecycle
	CertificateInventory        network.CertificateInventory
	SubscriptionQueries         entitlements.SubscriptionQueries
	SubscriptionTemplates       entitlements.SubscriptionTemplates
	SubscriptionProjection      entitlements.SubscriptionProjection
	NetworkEntryProjection      entitlements.NetworkEntryProjection
	Announcements               experience.Announcements
	Tickets                     experience.Tickets
	OrderQueries                commerce.OrderQueries
	OrderAssignment             commerce.OrderAssignment
	OrderCancellation           commerce.OrderCancellation
	OrderCreation               commerce.OrderCreation
	PlanListing                 commerce.PlanListing
	PlanDetails                 commerce.PlanDetails
	SKUQueries                  commerce.SKUQueries
	PlanCreation                commerce.PlanCreation
	PlanUpdate                  commerce.PlanUpdate
	SKUCreation                 commerce.SKUCreation
	SKUUpdate                   commerce.SKUUpdate
	Settings                    platform.Settings
	SiteSettings                platform.SiteSettings
	SiteCustomizationDefaults   platform.SiteCustomizationDefaults
	SubscriptionPresentation    platform.SubscriptionPresentation
	SystemConfigDefaults        platform.SystemConfigDefaults
	Installation                platform.Installation
	Maintenance                 platform.Maintenance
	Identity                    Identity
	Jobs                        *jobs.Runtime
	JobSubmissions              jobs.Submissions
	JobHistory                  jobs.HistoryQueries
	JobCancellations            jobs.CancellationService
	BatchRequests               jobs.BatchRequests
	JobReviews                  jobs.ReviewService
	OperationHistory            network.OperationHistory
	HistoryRetention            observability.HistoryRetention
	Audit                       observability.Audit
	AuditDirectory              observability.AuditDirectory
	Dashboard                   observability.Dashboard
	EntityReferences            observability.EntityReferences
	OperationLogs               observability.OperationLogs
	ProtocolEndpointOrder       network.ProtocolEndpointOrder
	ProviderDirectory           network.ProviderDirectory
	ResourceRemoval             network.ResourceRemoval
	TopologyRemoval             network.TopologyRemoval
	lifecycle                   atomic.Uint32
	maintenance                 maintenanceCache
	installation                installationCache
	trafficReadMu               sync.RWMutex
	trafficReadDB               *gorm.DB
}

func New(db *gorm.DB, secret string) *Services {
	s := &Services{Identity: NewIdentity(db, secret), JobReviews: jobs.ReviewService{Repository: networkstore.JobReviews{DB: db}}}
	s.trafficReadDB = db
	s.MessageTemplates = messaging.Templates{Repository: messagingstore.Templates{DB: db}}
	s.PrincipalCollection = metering.PrincipalCollection{Repository: meteringstore.PrincipalCollection{DB: db}}
	s.FairUseObservationSeries = metering.ObservationSeriesService{Repository: meteringstore.ObservationSeries{DB: db}}
	s.FairUseFlowCollection = metering.FlowCollection{Repository: meteringstore.FlowCollection{DB: db}}
	s.FairUseEvaluationRequests = metering.EvaluationRequests{Repository: meteringstore.EvaluationRequests{DB: db}}
	s.FairUseEvaluator = metering.Evaluator{Source: meteringstore.EvaluationSource{DB: db}, Sampler: meteringstore.Telemetry{DB: db}, Repository: meteringstore.EvaluationWriter{DB: db}}
	s.FairUseTelemetry = metering.Telemetry{Repository: meteringstore.Telemetry{DB: db}}
	s.FairUseEvaluationSource = meteringstore.EvaluationSource{DB: db}
	s.FairUseObservations = metering.Observations{Repository: meteringstore.Observations{DB: db}}
	s.FairUsePolicies = metering.Policies{Repository: meteringstore.Policies{DB: db}}
	s.DeliveryOrder = network.DeliveryOrder{Repository: networkstore.DeliveryOrder{DB: db}}
	s.ProtocolEndpointMultiplier = network.ProtocolEndpointMultiplier{Repository: networkstore.ProtocolEndpointMultiplier{DB: db}}
	s.ProxyPoolQueries = network.ProxyPoolQueries{Repository: networkstore.ProxyPoolQueries{DB: db}, Details: networkstore.ProxyPoolQueries{DB: db}}
	s.NetworkEntryQueries = network.NetworkEntryQueries{Repository: networkstore.NetworkEntryQueries{DB: db}}
	s.NodeGroupMutations = network.NodeGroupMutations{Repository: networkstore.NodeGroupMutations{DB: db}}
	s.NodeAdministration = network.NodeAdministration{Repository: networkstore.NodeAdministration{DB: db}}
	s.NetworkInventory = network.Inventory{Repository: networkstore.Inventory{DB: db}}
	s.ManagedPublicationInventory = network.ManagedPublicationInventory{Repository: networkstore.ManagedPublicationInventory{DB: db}}
	s.NativeOperationStatus = network.NativeOperationStatus{Repository: networkstore.NativeOperationStatus{DB: db}}
	s.DNSDeletion = network.DNSDeletion{Repository: networkstore.DNSDeletion{DB: db}}
	s.NodeActivity = network.NodeActivity{Repository: networkstore.NodeActivity{DB: db}}
	s.EventCredentials = network.EventCredentials{Repository: networkstore.EventCredentials{DB: db}}
	s.SSHHostTrust = network.SSHHostTrust{Repository: networkstore.SSHHostTrust{DB: db}}
	s.ProtocolEndpointRemoval = network.ProtocolEndpointRemoval{Store: networkstore.ProtocolEndpointRemoval{DB: db}}
	s.PublicationRequests = network.PublicationRequests{Repository: networkstore.PublicationRequests{DB: db}}
	s.CertificateLifecycle = network.CertificateLifecycle{Repository: networkstore.CertificateLifecycle{DB: db}}
	s.CertificateInventory = network.CertificateInventory{Repository: networkstore.CertificateInventory{DB: db}}
	s.SubscriptionQueries = entitlements.SubscriptionQueries{Repository: entitlementstore.SubscriptionQueries{DB: db}}
	s.SubscriptionTemplates = entitlements.SubscriptionTemplates{Repository: entitlementstore.SubscriptionTemplates{DB: db}}
	s.SubscriptionProjection = entitlements.SubscriptionProjection{Repository: entitlementstore.SubscriptionProjection{DB: db}}
	s.NetworkEntryProjection = entitlements.NetworkEntryProjection{Repository: entitlementstore.NetworkEntryProjection{DB: db}}
	s.Announcements = experience.Announcements{Repository: experiencestore.Announcements{DB: db}}
	s.Tickets = experience.Tickets{Repository: experiencestore.Tickets{DB: db}}
	s.OrderQueries = commerce.OrderQueries{Repository: commercestore.OrderQueries{DB: db}}
	s.OrderAssignment = commerce.OrderAssignment{Repository: commercestore.OrderAssignment{DB: db}}
	s.OrderCancellation = commerce.OrderCancellation{Repository: commercestore.OrderCancellation{DB: db}}
	s.OrderCreation = commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: db}}
	s.PlanListing = commerce.PlanListing{Repository: commercestore.PlanListing{DB: db}}
	s.PlanDetails = commerce.PlanDetails{Repository: commercestore.PlanDetails{DB: db}}
	s.SKUQueries = commerce.SKUQueries{Repository: commercestore.SKUQueries{DB: db}}
	s.PlanCreation = commerce.PlanCreation{Repository: commercestore.PlanCreation{DB: db}}
	s.PlanUpdate = commerce.PlanUpdate{Repository: commercestore.PlanUpdate{DB: db}}
	s.SKUUpdate = commerce.SKUUpdate{Repository: commercestore.SKUUpdate{DB: db}}
	s.SKUCreation = commerce.SKUCreation{Repository: commercestore.SKUCreation{DB: db}}
	s.Settings = platform.Settings{Repository: platformstore.Settings{DB: db}}
	s.SiteSettings = platform.SiteSettings{Repository: platformstore.Settings{DB: db}}
	s.SiteCustomizationDefaults = platform.SiteCustomizationDefaults{Repository: platformstore.SiteCustomizationDefaults{DB: db}}
	s.SubscriptionPresentation = platform.SubscriptionPresentation{Repository: platformstore.SubscriptionPresentation{DB: db}}
	s.SystemConfigDefaults = platform.SystemConfigDefaults{Repository: platformstore.SystemConfigDefaults{DB: db}}
	s.Installation = platform.Installation{Repository: installationRepository{InstallationRepository: platformstore.Installation{DB: db}, invalidate: s.InvalidateInstallation}}
	s.Maintenance = platform.Maintenance{Repository: maintenanceRepository{MaintenanceRepository: platformstore.Maintenance{DB: db}, invalidate: s.InvalidateMaintenance}}
	s.TopologyRemoval = network.TopologyRemoval{Store: networkstore.TopologyRemoval{DB: db}}
	s.ResourceRemoval = network.ResourceRemoval{Store: networkstore.ResourceRemoval{DB: db}}
	s.ProviderDirectory = network.ProviderDirectory{Store: networkstore.ProviderAccounts{DB: db}}
	s.OperationHistory = network.OperationHistory{Store: networkstore.OperationHistory{DB: db}}
	s.HistoryRetention = observability.HistoryRetention{Repository: observabilitystore.HistoryRetention{DB: db}}
	s.Audit = observability.Audit{Repository: observabilitystore.Audit{DB: db}}
	s.AuditDirectory = observability.AuditDirectory{Repository: observabilitystore.AuditDirectory{DB: db}}
	s.Dashboard = observability.Dashboard{Repository: observabilitystore.Dashboard{DB: db}}
	s.EntityReferences = observability.EntityReferences{Repository: observabilitystore.EntityReferences{DB: db}}
	s.OperationLogs = observability.OperationLogs{Repository: observabilitystore.OperationLogs{DB: db}}
	s.ProtocolEndpointOrder = network.ProtocolEndpointOrder{Repository: networkstore.ProtocolEndpointOrder{DB: db}}
	s.BatchRequests = jobs.BatchRequests{Repository: jobstore.BatchRequests{DB: db}}
	s.JobSubmissions = jobs.Submissions{Repository: jobstore.New(db)}
	s.JobHistory = jobs.HistoryQueries{Repository: jobstore.New(db)}
	s.Jobs = jobs.NewGatedRuntime(jobstore.New(db), uuid.NewString(), s.isReady, s.WorkPaused, func(err error) { log.Printf("job runtime: %v", err) })
	s.JobCancellations = jobs.CancellationService{Repository: jobstore.New(db), Active: s.Jobs}
	return s
}

func (s *Services) isReady() bool { return s.lifecycle.Load() == 1 }

func (s *Services) DatabaseReady(ctx context.Context) error {
	db, err := s.Identity.db.DB()
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}

// StartWork opens the bootstrap gate after every startup step and registration.
func (s *Services) StartWork() { s.lifecycle.CompareAndSwap(0, 1) }
func (s *Services) Close()     { s.lifecycle.Store(2); s.Jobs.Close() }

// Matches prevents a legacy transport adapter from pairing a capability set
// with a different authority database or signing configuration.
func (s *Services) Matches(db *gorm.DB, secret string) bool {
	return s.Identity.db == db && s.Identity.secret == secret
}

func (s *Services) RecoverLegacyOperations(ctx context.Context) (int, error) {
	return (networkstore.Recovery{DB: s.Identity.db}).RecoverLegacyNative(ctx)
}

func (s *Services) DNSReconciliation(observer network.DNSObserver) network.DNSReconciliation {
	return network.DNSReconciliation{Store: networkstore.DNSReconciliation{DB: s.Identity.db}, Observer: observer}
}

func (s *Services) ManagedDNS(cipher network.ProviderCredentialCipher, provider network.ManagedDNSProvider, observer network.ManagedDNSPublicObserver) network.ManagedDNS {
	return network.ManagedDNS{Repository: networkstore.ManagedDNS{DB: s.Identity.db}, Cipher: cipher, Provider: provider, Observer: observer}
}

func (s *Services) ProviderAccounts(cipher network.ProviderCredentialCipher, verifier network.ProviderCredentialVerifier) network.ProviderAccounts {
	return network.ProviderAccounts{Store: networkstore.ProviderAccounts{DB: s.Identity.db}, Cipher: cipher, Verifier: verifier}
}

func (s *Services) ProviderCreation(cipher network.ProviderCredentialCipher, verifier network.ProviderCredentialVerifier, registry network.ProviderRegistry) network.ProviderCreation {
	return network.ProviderCreation{Store: networkstore.ProviderAccounts{DB: s.Identity.db}, Accounts: s.ProviderAccounts(cipher, verifier), Registry: registry}
}

func (s *Services) ProxyPoolMutations(cipher network.ProviderCredentialCipher, inspector network.ProxyPoolConfigurationInspector) network.ProxyPoolMutations {
	return network.ProxyPoolMutations{Repository: networkstore.ProxyPoolMutations{DB: s.Identity.db}, Cipher: cipher, Inspector: inspector}
}

func (s *Services) ProxyPoolSubscriptions(cipher network.ProviderCredentialCipher, inspector network.ProxyPoolConfigurationInspector) network.ProxyPoolSubscriptions {
	return network.ProxyPoolSubscriptions{Repository: networkstore.ProxyPoolSubscriptions{DB: s.Identity.db}, Cipher: cipher, Inspector: inspector}
}

func (s *Services) NetworkEntryMutations(cipher network.ProviderCredentialCipher, inspector network.ProxyPoolConfigurationInspector) network.NetworkEntryMutations {
	return network.NetworkEntryMutations{Repository: networkstore.NetworkEntryMutations{DB: s.Identity.db}, Cipher: cipher, Inspector: inspector}
}

func (s *Services) ProtocolEndpointMutations(cipher network.ProviderCredentialCipher) network.ProtocolEndpointMutations {
	return network.ProtocolEndpointMutations{Repository: networkstore.ProtocolEndpointMutations{DB: s.Identity.db}, Cipher: cipher}
}

func (s *Services) MieruEndpointConfigurations(cipher network.ProviderCredentialCipher) network.MieruEndpointConfigurations {
	return network.MieruEndpointConfigurations{Repository: networkstore.MieruEndpointConfigurations{DB: s.Identity.db}, Cipher: cipher}
}

func (s *Services) NodeRemoval() network.NodeRemoval {
	return network.NodeRemoval{Store: networkstore.NodeRemoval{DB: s.Identity.db}}
}

func (s *Services) SubscriptionRuleSets(content entitlements.RuleSetContentStore) entitlements.SubscriptionRuleSets {
	return entitlements.SubscriptionRuleSets{Repository: entitlementstore.SubscriptionRuleSets{DB: s.Identity.db, Content: content}}
}

func (s *Services) CredentialExpiry() entitlements.CredentialExpiry {
	return entitlements.CredentialExpiry{Repository: entitlementstore.CredentialExpiry{DB: s.Identity.db, Publish: networkstore.EnqueueNodePublication}}
}

func RequestNodePublication(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	return networkstore.EnqueuePublication(tx, nodeID, endpointID, actor)
}
func (s *Services) OrderSettlement(cipher zeroadapter.Cipher, mieru bool) commerce.OrderSettlement {
	return commerce.OrderSettlement{Repository: commercestore.OrderSettlement{DB: s.Identity.db, Issuer: zeroadapter.Issuer{Cipher: cipher, Mieru: mieru}}}
}

func (s *Services) OrderAccess(cipher zeroadapter.Cipher) commerce.OrderAccess {
	return commerce.OrderAccess{Repository: commercestore.OrderAccess{DB: s.Identity.db, Cipher: cipher}}
}
