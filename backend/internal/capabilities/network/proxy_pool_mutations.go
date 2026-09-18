package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

const ProxyPoolDefaultSyncInterval = 24 * 60 * 60

var (
	ErrProxyPoolMutationUnavailable = errors.New("proxy pool mutation capability unavailable")
	ErrProxyPoolMutationPermission  = errors.New("proxy pool mutation requires current administrator")
	ErrProxyPoolNotFound            = errors.New("proxy pool not found")
	ErrProxyPoolConflict            = errors.New("proxy pool changed")
)

type ProxyPoolMutationValidation struct {
	Message string
}

func (e *ProxyPoolMutationValidation) Error() string { return e.Message }

type ProxyPoolConfigurationFacts struct {
	SupportsDatagram bool
	DatagramError    string
}

type ProxyPoolConfigurationInspector interface {
	InspectProxyPoolConfiguration(context.Context, string, bool) (ProxyPoolConfigurationFacts, error)
}

type ProxyPoolMutationSnapshot struct {
	Pool                      ProxyPoolRecord
	ConfigCiphertext          string
	SubscriptionURLCiphertext string
	SubscriptionUserAgent     string
}

type ProxyPoolMutationRequest struct {
	ID                    uint
	NodeID                uint
	Name                  string
	ExpectedRevision      uint64
	Delete                bool
	Config                *string
	SubscriptionURL       *string
	SubscriptionFormat    string
	SubscriptionUserAgent string
	AutoSync              *bool
	SyncIntervalSeconds   int
	InitialSyncAt         *time.Time
	InitialNodeCount      int
}

type ProxyPoolMutationChange struct {
	Delete                        bool
	NodeID                        uint
	Name                          string
	ReplaceConfig                 bool
	ConfigCiphertext              string
	ConfigurationSupportsDatagram bool
	ConfigurationDatagramError    string
	ReplaceSubscription           bool
	SubscriptionURLCiphertext     string
	SubscriptionFormat            string
	SubscriptionUserAgent         string
	AutoSync                      bool
	SyncIntervalSeconds           int
	SubscriptionSourceChanged     bool
	InitialSyncAt                 *time.Time
	InitialNodeCount              int
	Now                           time.Time
}

type ProxyPoolMutationRepository interface {
	LoadProxyPoolMutation(context.Context, uint, uint) (ProxyPoolMutationSnapshot, error)
	CommitProxyPoolMutation(context.Context, uint, *ProxyPoolMutationSnapshot, ProxyPoolMutationChange) (ProxyPoolRecord, error)
}

type ProxyPoolMutations struct {
	Repository ProxyPoolMutationRepository
	Cipher     ProviderCredentialCipher
	Inspector  ProxyPoolConfigurationInspector
	Now        func() time.Time
}

func (s ProxyPoolMutations) Save(ctx context.Context, actor uint, request ProxyPoolMutationRequest) (ProxyPoolRecord, error) {
	if s.Repository == nil || s.Cipher == nil || s.Inspector == nil || actor == 0 {
		return ProxyPoolRecord{}, ErrProxyPoolMutationUnavailable
	}
	request.Name = strings.TrimSpace(request.Name)
	var before *ProxyPoolMutationSnapshot
	if request.ID != 0 {
		snapshot, err := s.Repository.LoadProxyPoolMutation(ctx, actor, request.ID)
		if err != nil {
			return ProxyPoolRecord{}, err
		}
		before = &snapshot
		if request.Delete {
			return s.Repository.CommitProxyPoolMutation(ctx, actor, before, ProxyPoolMutationChange{Delete: true, NodeID: snapshot.Pool.NodeID, Now: s.now()})
		}
		if request.NodeID != snapshot.Pool.NodeID || request.ExpectedRevision != snapshot.Pool.Revision {
			return ProxyPoolRecord{}, ErrProxyPoolConflict
		}
	}
	if request.Delete || request.NodeID == 0 || request.Name == "" || len(request.Name) > 80 {
		return ProxyPoolRecord{}, &ProxyPoolMutationValidation{Message: "请选择节点并填写 1–80 字节的代理池名称"}
	}
	if before == nil && request.Config == nil {
		return ProxyPoolRecord{}, &ProxyPoolMutationValidation{Message: "请配置代理池成员或订阅地址"}
	}

	plainConfig := ""
	change := ProxyPoolMutationChange{NodeID: request.NodeID, Name: request.Name, Now: s.now()}
	if request.Config != nil {
		plainConfig = strings.TrimSpace(*request.Config)
		if plainConfig == "" {
			return ProxyPoolRecord{}, &ProxyPoolMutationValidation{Message: "请配置代理池成员"}
		}
		facts, err := s.Inspector.InspectProxyPoolConfiguration(ctx, plainConfig, true)
		if err != nil {
			return ProxyPoolRecord{}, err
		}
		ciphertext, err := s.Cipher.Encrypt(plainConfig)
		if err != nil {
			return ProxyPoolRecord{}, err
		}
		change.ReplaceConfig, change.ConfigCiphertext = true, ciphertext
		change.ConfigurationSupportsDatagram, change.ConfigurationDatagramError = facts.SupportsDatagram, facts.DatagramError
	} else {
		var err error
		plainConfig, err = s.Cipher.Decrypt(before.ConfigCiphertext)
		if err != nil {
			return ProxyPoolRecord{}, errors.New("代理池配置无法解密")
		}
		facts, err := s.Inspector.InspectProxyPoolConfiguration(ctx, plainConfig, false)
		if err != nil {
			return ProxyPoolRecord{}, err
		}
		change.ConfigurationSupportsDatagram, change.ConfigurationDatagramError = facts.SupportsDatagram, facts.DatagramError
	}

	if request.SubscriptionURL != nil {
		change.ReplaceSubscription = true
		url := strings.TrimSpace(*request.SubscriptionURL)
		if url == "" {
			if request.AutoSync != nil && *request.AutoSync {
				return ProxyPoolRecord{}, &ProxyPoolMutationValidation{Message: "启用自动同步前请填写订阅地址"}
			}
			change.SyncIntervalSeconds = ProxyPoolDefaultSyncInterval
		} else {
			if request.SyncIntervalSeconds <= 0 {
				return ProxyPoolRecord{}, &ProxyPoolMutationValidation{Message: "自动同步间隔必须是正整数"}
			}
			userAgent := strings.TrimSpace(request.SubscriptionUserAgent)
			if len(userAgent) > 255 || strings.ContainsAny(userAgent, "\r\n") {
				return ProxyPoolRecord{}, &ProxyPoolMutationValidation{Message: "订阅 User-Agent 不能超过 255 字节或包含换行"}
			}
			ciphertext, err := s.Cipher.Encrypt(url)
			if err != nil {
				return ProxyPoolRecord{}, err
			}
			change.SubscriptionURLCiphertext = ciphertext
			change.SubscriptionFormat = strings.TrimSpace(request.SubscriptionFormat)
			change.SubscriptionUserAgent = userAgent
			change.SyncIntervalSeconds = request.SyncIntervalSeconds
			if request.AutoSync != nil {
				change.AutoSync = *request.AutoSync
			} else if before != nil {
				change.AutoSync = before.Pool.AutoSync
			}
			if before == nil || before.SubscriptionURLCiphertext == "" {
				change.SubscriptionSourceChanged = true
			} else {
				previousURL, err := s.Cipher.Decrypt(before.SubscriptionURLCiphertext)
				if err != nil {
					return ProxyPoolRecord{}, errors.New("代理池订阅地址无法解密")
				}
				change.SubscriptionSourceChanged = previousURL != url
			}
			if request.InitialSyncAt != nil {
				initial := request.InitialSyncAt.UTC()
				change.InitialSyncAt = &initial
				change.InitialNodeCount = request.InitialNodeCount
			}
		}
	}
	return s.Repository.CommitProxyPoolMutation(ctx, actor, before, change)
}

func (s ProxyPoolMutations) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
