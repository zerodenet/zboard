package network

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrManagedDNSPermission       = errors.New("managed DNS administration requires current administrator")
	ErrManagedDNSNotFound         = errors.New("managed DNS record not found")
	ErrManagedDNSRevisionConflict = errors.New("managed DNS revision conflict")
	ErrManagedDNSOperationRunning = errors.New("managed DNS operation is running")
	ErrManagedDNSDeleting         = errors.New("managed DNS record is deleting")
	ErrManagedDNSDuplicate        = errors.New("managed DNS record already exists")
	ErrManagedDNSDependency       = errors.New("managed DNS dependency is unavailable")
)

const (
	ManagedDNSPending = "pending"
	ManagedDNSSyncing = "syncing"
	ManagedDNSActive  = "active"
	ManagedDNSDrifted = "drifted"
	ManagedDNSFailed  = "failed"
)

type ManagedDNSValidation struct{ Fields map[string]string }

func (e *ManagedDNSValidation) Error() string { return "DNS 解析校验失败。" }

type ManagedDNSRecord struct {
	ID                uint       `json:"id"`
	ProviderAccountID uint       `json:"provider_account_id"`
	NodeID            uint       `json:"node_id"`
	DomainName        string     `json:"domain_name"`
	RecordType        string     `json:"record_type"`
	RecordValue       string     `json:"record_value"`
	ProviderZoneID    string     `json:"provider_zone_id"`
	ProviderRecordID  string     `json:"provider_record_id"`
	TTL               int        `json:"ttl"`
	Proxied           bool       `json:"proxied"`
	Status            string     `json:"status"`
	DesiredHash       string     `json:"desired_hash"`
	ObservedHash      string     `json:"observed_hash"`
	LastSyncedAt      *time.Time `json:"last_synced_at,omitempty"`
	LastPublicCheckAt *time.Time `json:"last_public_check_at,omitempty"`
	PublicResolved    bool       `json:"public_resolved"`
	LastError         string     `json:"last_error"`
	Revision          uint64     `json:"revision"`
	CreatedBy         uint       `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ManagedDNSOperation struct {
	ID                uint       `json:"id"`
	ProviderAccountID uint       `json:"provider_account_id"`
	ResourceType      string     `json:"resource_type"`
	ResourceID        uint       `json:"resource_id"`
	OperationType     string     `json:"operation_type"`
	Status            string     `json:"status"`
	Phase             string     `json:"phase"`
	RequestedBy       *uint      `json:"requested_by,omitempty"`
	ResultSummary     string     `json:"result_summary"`
	Error             string     `json:"error"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ManagedDNSView struct {
	ManagedDNSRecord
	ProviderName    string               `json:"provider_name"`
	ProviderKey     string               `json:"provider_key"`
	NodeName        string               `json:"node_name"`
	LatestOperation *ManagedDNSOperation `json:"latest_operation,omitempty"`
}

type ManagedDNSPage struct {
	Records []ManagedDNSView
	Total   int64
}

type ManagedDNSListQuery struct {
	Search string
	Status string
	Offset int
	Limit  int
}

type ManagedDNSRecordInput struct {
	RecordType  string `json:"record_type"`
	RecordValue string `json:"record_value"`
}

type ManagedDNSWrite struct {
	ProviderAccountID uint                    `json:"provider_account_id"`
	NodeID            uint                    `json:"node_id"`
	DomainName        string                  `json:"domain_name"`
	RecordType        string                  `json:"record_type"`
	RecordValue       string                  `json:"record_value"`
	TTL               int                     `json:"ttl"`
	Proxied           bool                    `json:"proxied"`
	TakeoverExisting  bool                    `json:"takeover_existing"`
	ExpectedRevision  uint64                  `json:"expected_revision"`
	Records           []ManagedDNSRecordInput `json:"records"`
}

type ManagedDNSDependencies struct {
	NodeAddress, NodeSSHHost    string
	ProviderKey, ProviderStatus string
	ProviderSupportsDNS         bool
}

type ManagedDNSExecution struct {
	Operation            ManagedDNSOperation
	Record               ManagedDNSRecord
	ProviderKey          string
	CredentialCiphertext string
}

type ManagedDNSProviderRequest struct {
	ProviderKey string
	Credential  string
	Record      ManagedDNSRecord
	Takeover    bool
}

type ManagedDNSProviderResult struct {
	ZoneID, RecordID, RecordType, Name, Value string
	TTL                                       int
	Proxied                                   bool
}

type ManagedDNSRepository interface {
	ListManagedDNS(context.Context, uint, ManagedDNSListQuery) (ManagedDNSPage, error)
	ManagedDNSDependencies(context.Context, uint, uint, uint) (ManagedDNSDependencies, error)
	CreateManagedDNS(context.Context, uint, []ManagedDNSRecord) ([]ManagedDNSRecord, error)
	UpdateManagedDNS(context.Context, uint, uint, uint64, ManagedDNSRecord) (ManagedDNSRecord, error)
	StartManagedDNS(context.Context, uint, uint, bool) (ManagedDNSOperation, error)
	LoadManagedDNSExecution(context.Context, uint) (ManagedDNSExecution, error)
	SetManagedDNSPhase(context.Context, ManagedDNSExecution, string) error
	FailManagedDNS(context.Context, ManagedDNSExecution, string, string) error
	CompleteManagedDNS(context.Context, ManagedDNSExecution, ManagedDNSProviderResult, bool) error
	ListManagedDNSObservations(context.Context, time.Time, time.Duration, int) ([]ManagedDNSRecord, error)
	RecordManagedDNSObservation(context.Context, uint, time.Time, bool) error
}

type ManagedDNSProvider interface {
	ApplyManagedDNS(context.Context, ManagedDNSProviderRequest, func(string) error) (ManagedDNSProviderResult, error)
}

type ManagedDNSPublicObserver interface {
	ManagedDNSResolves(context.Context, ManagedDNSRecord) bool
}

type ManagedDNS struct {
	Repository ManagedDNSRepository
	Cipher     ProviderCredentialCipher
	Provider   ManagedDNSProvider
	Observer   ManagedDNSPublicObserver
}

func (s ManagedDNS) List(ctx context.Context, actor uint, query ManagedDNSListQuery) (ManagedDNSPage, error) {
	if actor == 0 {
		return ManagedDNSPage{}, ErrManagedDNSPermission
	}
	query.Search = strings.TrimSpace(query.Search)
	query.Status = strings.TrimSpace(query.Status)
	return s.Repository.ListManagedDNS(ctx, actor, query)
}

func (s ManagedDNS) Create(ctx context.Context, actor uint, input ManagedDNSWrite) ([]ManagedDNSRecord, []ManagedDNSOperation, error) {
	if actor == 0 {
		return nil, nil, ErrManagedDNSPermission
	}
	inputs := input.Records
	if len(inputs) == 0 {
		inputs = []ManagedDNSRecordInput{{RecordType: input.RecordType, RecordValue: input.RecordValue}}
	}
	if len(inputs) < 1 || len(inputs) > 2 {
		return nil, nil, &ManagedDNSValidation{map[string]string{"records": "一次只能创建一条 A、一条 AAAA 记录。"}}
	}
	dependencies, err := s.Repository.ManagedDNSDependencies(ctx, actor, input.ProviderAccountID, input.NodeID)
	if err != nil {
		return nil, nil, err
	}
	records := make([]ManagedDNSRecord, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for index, item := range inputs {
		candidate := input
		candidate.RecordType, candidate.RecordValue, candidate.Records = item.RecordType, item.RecordValue, nil
		record, err := normalizeManagedDNS(candidate, dependencies)
		if err != nil {
			var validation *ManagedDNSValidation
			if errors.As(err, &validation) {
				fields := make(map[string]string, len(validation.Fields))
				for key, value := range validation.Fields {
					fields["records."+itoa(index)+"."+key] = value
				}
				return nil, nil, &ManagedDNSValidation{fields}
			}
			return nil, nil, err
		}
		if _, exists := seen[record.RecordType]; exists {
			return nil, nil, &ManagedDNSValidation{map[string]string{"records": "A 和 AAAA 各自最多提交一次。"}}
		}
		seen[record.RecordType] = struct{}{}
		record.CreatedBy = actor
		records = append(records, record)
	}
	records, err = s.Repository.CreateManagedDNS(ctx, actor, records)
	if err != nil {
		return nil, nil, err
	}
	operations := make([]ManagedDNSOperation, 0, len(records))
	for _, record := range records {
		operation, startErr := s.Repository.StartManagedDNS(ctx, actor, record.ID, input.TakeoverExisting)
		if startErr != nil {
			return records, operations, startErr
		}
		operations = append(operations, operation)
	}
	return records, operations, nil
}

func (s ManagedDNS) Update(ctx context.Context, actor, id uint, input ManagedDNSWrite) (ManagedDNSRecord, ManagedDNSOperation, error) {
	if actor == 0 {
		return ManagedDNSRecord{}, ManagedDNSOperation{}, ErrManagedDNSPermission
	}
	if input.ExpectedRevision == 0 {
		return ManagedDNSRecord{}, ManagedDNSOperation{}, &ManagedDNSValidation{map[string]string{"expected_revision": "请刷新后再编辑该 DNS 记录。"}}
	}
	dependencies, err := s.Repository.ManagedDNSDependencies(ctx, actor, input.ProviderAccountID, input.NodeID)
	if err != nil {
		return ManagedDNSRecord{}, ManagedDNSOperation{}, err
	}
	record, err := normalizeManagedDNS(input, dependencies)
	if err != nil {
		return ManagedDNSRecord{}, ManagedDNSOperation{}, err
	}
	record, err = s.Repository.UpdateManagedDNS(ctx, actor, id, input.ExpectedRevision, record)
	if err != nil {
		return ManagedDNSRecord{}, ManagedDNSOperation{}, err
	}
	operation, err := s.Repository.StartManagedDNS(ctx, actor, id, false)
	return record, operation, err
}

func (s ManagedDNS) Start(ctx context.Context, actor, id uint, takeover bool) (ManagedDNSOperation, error) {
	if actor == 0 {
		return ManagedDNSOperation{}, ErrManagedDNSPermission
	}
	return s.Repository.StartManagedDNS(ctx, actor, id, takeover)
}

func (s ManagedDNS) Execute(ctx context.Context, operationID uint) error {
	execution, err := s.Repository.LoadManagedDNSExecution(ctx, operationID)
	if err != nil {
		return err
	}
	fail := func(phase string, cause error) error {
		message := strings.TrimSpace(cause.Error())
		if len(message) > 4000 {
			message = message[:4000]
		}
		if persistErr := s.Repository.FailManagedDNS(context.WithoutCancel(ctx), execution, phase, message); persistErr != nil {
			return errors.Join(cause, persistErr)
		}
		return cause
	}
	token, err := s.Cipher.Decrypt(execution.CredentialCiphertext)
	if err != nil {
		return fail("credentials", err)
	}
	if err := s.Repository.SetManagedDNSPhase(ctx, execution, "resolving_zone"); err != nil {
		return fail("persisting", err)
	}
	phase := "resolving_zone"
	progress := func(next string) error {
		phase = next
		return s.Repository.SetManagedDNSPhase(ctx, execution, next)
	}
	result, err := s.Provider.ApplyManagedDNS(ctx, ManagedDNSProviderRequest{
		ProviderKey: execution.ProviderKey,
		Credential:  token,
		Record:      execution.Record,
		Takeover:    execution.Operation.OperationType == "takeover",
	}, progress)
	if err != nil {
		return fail(phase, err)
	}
	resolved := false
	if s.Observer != nil {
		resolved = s.Observer.ManagedDNSResolves(ctx, execution.Record)
	}
	if err := s.Repository.CompleteManagedDNS(ctx, execution, result, resolved); err != nil {
		return fail("persisting", err)
	}
	return nil
}

func (s ManagedDNS) ObservePublic(ctx context.Context, now time.Time, interval, timeout time.Duration, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}
	records, err := s.Repository.ListManagedDNSObservations(ctx, now, interval, 50)
	if err != nil {
		return err
	}
	jobs := make(chan ManagedDNSRecord)
	results := make(chan error, len(records))
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for record := range jobs {
				if ctx.Err() != nil {
					results <- ctx.Err()
					continue
				}
				probe, cancel := context.WithTimeout(ctx, timeout)
				resolved := s.Observer.ManagedDNSResolves(probe, record)
				probeErr := probe.Err()
				cancel()
				err := s.Repository.RecordManagedDNSObservation(ctx, record.ID, time.Now().UTC(), resolved)
				if err == nil && !resolved {
					err = probeErr
					if err == nil {
						err = errors.New("public DNS has not converged")
					}
				}
				results <- err
			}
		}()
	}
	for _, record := range records {
		jobs <- record
	}
	close(jobs)
	wg.Wait()
	close(results)
	var failures []error
	for err := range results {
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

var managedDNSLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeManagedDNS(input ManagedDNSWrite, dependencies ManagedDNSDependencies) (ManagedDNSRecord, error) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.DomainName), "."))
	recordType := strings.ToUpper(strings.TrimSpace(input.RecordType))
	value := strings.TrimSpace(input.RecordValue)
	ttl := input.TTL
	if ttl == 0 {
		ttl = 1
	}
	fields := map[string]string{}
	if !validManagedDNSDomain(domain) {
		fields["domain_name"] = "请输入有效的完整域名。"
	}
	if recordType != "A" && recordType != "AAAA" {
		fields["record_type"] = "第一版只支持 A 和 AAAA 记录。"
	}
	if value == "" {
		for _, candidate := range []string{strings.TrimSpace(dependencies.NodeAddress), strings.TrimSpace(dependencies.NodeSSHHost)} {
			ip := net.ParseIP(candidate)
			if ip != nil && ((recordType == "A" && ip.To4() != nil) || (recordType == "AAAA" && ip.To4() == nil)) {
				value = candidate
				break
			}
		}
	}
	ip := net.ParseIP(value)
	if ip == nil || (recordType == "A" && ip.To4() == nil) || (recordType == "AAAA" && ip.To4() != nil) {
		fields["record_value"] = "记录值必须是与记录类型匹配的公网 IP 地址。"
	} else if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		fields["record_value"] = "记录值必须是可公开路由的节点 IP 地址。"
	}
	if ttl != 1 && (ttl < 60 || ttl > 86400) {
		fields["ttl"] = "TTL 必须为自动（1）或 60–86400 秒。"
	}
	if strings.TrimSpace(dependencies.ProviderKey) == "" || dependencies.ProviderStatus != "active" || !dependencies.ProviderSupportsDNS {
		fields["provider_account_id"] = "请选择有效且已验证、支持 DNS 记录管理的供应商账户。"
	}
	if len(fields) > 0 {
		return ManagedDNSRecord{}, &ManagedDNSValidation{fields}
	}
	record := ManagedDNSRecord{ProviderAccountID: input.ProviderAccountID, NodeID: input.NodeID, DomainName: domain, RecordType: recordType, RecordValue: value, TTL: ttl, Proxied: input.Proxied, Status: ManagedDNSPending, Revision: 1}
	record.DesiredHash = DNSRecordHash(record.RecordType, record.DomainName, record.RecordValue, record.TTL, record.Proxied)
	return record, nil
}

func validManagedDNSDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.Contains(domain, "*") || net.ParseIP(domain) != nil || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || !managedDNSLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
