package meteringstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"time"
)

type CompletionAccounting struct {
	DB                  *gorm.DB
	Cipher              metering.CredentialDecryptor
	EnqueuePublications func(*gorm.DB, uint, uint) error
}

func completionNonce(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:])
}
func (s CompletionAccounting) Complete(ctx context.Context, in metering.CompletedFlow) (metering.TrafficRecord, bool, error) {
	if e := EnsureIdentityFences(s.DB.WithContext(ctx), []uint{in.NodeID}); e != nil {
		return metering.TrafficRecord{}, false, e
	}
	var activityID uint
	var record model.TrafficRecord
	exhausted := false
	var err error
	remaining := credentialstore.PrincipalIndexWorkLimit
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			break
		}
		prepared, prepareErr := (credentialstore.PrincipalIndex{DB: s.DB, Cipher: s.Cipher}).PrepareBounded(ctx, in.NodeID, remaining)
		remaining -= prepared
		if prepareErr != nil {
			err = prepareErr
			// A concurrent canonical change may invalidate coverage before the
			// preparation's final read, as well as after it. Retry only that
			// recoverable state within the same attempt and row budgets.
			if errors.Is(err, credentialstore.ErrPrincipalIndexIncomplete) && remaining > 0 {
				continue
			}
			return metering.TrafficRecord{}, false, err
		}
		event, flow := in, in
		record = model.TrafficRecord{}
		exhausted = false
		options := &sql.TxOptions{}
		if s.DB.Dialector.Name() == "mysql" {
			options.Isolation = sql.LevelReadCommitted
		}
		err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			authorities := newFlowBatch(tx)
			sample := metering.FlowSample{NodeID: in.NodeID, PrincipalKey: flow.PrincipalKey}
			if e := authorities.lockAuthorities(tx, s.Cipher, []metering.FlowSample{sample}); e != nil {
				return e
			}
			receipts, err := readReportReceipts(tx, in.NodeID, []string{event.EventID})
			if err != nil {
				return err
			}
			if len(receipts) > 0 {
				record, err = recordForReceipt(tx, receipts[0], event.EventID)
				if err != nil {
					return err
				}
				return nil
			}
			credential, credentialErr := authorities.credential(s.Cipher, tx, in.NodeID, flow.PrincipalKey)
			if credentialErr != nil {
				return credentialErr
			}
			activityID = credential.ID
			flow.PrincipalKey = credential.PrincipalKey
			subscriptionRef, err := authorities.subscription(tx, credential.SubscriptionID)
			if err != nil {
				return err
			}
			subscription := *subscriptionRef
			currentEpoch := subscription.Status != "deleted" && credential.EntitlementEpoch == subscription.EntitlementEpoch
			var closed model.ClosedEntitlement
			if !currentEpoch {
				var err error
				closed, err = loadClosedEntitlement(tx, subscription, credential.EntitlementEpoch)
				if err != nil {
					return err
				}
				subscription = closedQuotaSubscription(subscription, closed)
			}
			var endpoint model.ProtocolEndpoint
			if err := tx.Unscoped().First(&endpoint, credential.ProtocolEndpointID).Error; err != nil {
				return err
			}

			cumulativeRaw, err := metering.CheckedTrafficBytes(flow.BytesUp, flow.BytesDown, subscription.TrafficCalcMode)
			if err != nil {
				return err
			}
			usage, usageExists, legacyUsage, err := LoadFlowUsage(tx, in.NodeID, event.CoreInstanceID, flow.FlowID, credential.ID, flow.Revision, metering.FlowCounters{Raw: cumulativeRaw, Upload: flow.BytesUp, Download: flow.BytesDown})
			if err != nil {
				return err
			}
			targetUsageKey := metering.FlowUsageKey(event.CoreInstanceID, flow.FlowID)
			previousUsageKey := usage.FlowID
			continues := false
			previousRaw, previousUpload, previousDownload, previousUsed := int64(0), int64(0), int64(0), int64(0)
			if usageExists {
				if metering.RuntimeScopedFlow(targetUsageKey) && !legacyUsage {
					if usage.ProtocolCredentialID != credential.ID {
						return errors.New("flow principal changed during its runtime generation")
					}
					if usage.Revision > 0 && event.Sequence > 0 && event.Sequence < usage.Revision {
						return errors.New("Zero flow completion sequence is older than the persisted runtime cursor")
					}
					if usage.RawBytes > cumulativeRaw || usage.UploadBytes > flow.BytesUp || usage.DownloadBytes > flow.BytesDown {
						return errors.New("Zero flow completion counters regressed within one core instance")
					}
					continues = true
				} else if usage.ProtocolCredentialID == credential.ID &&
					usage.RawBytes <= cumulativeRaw && usage.UploadBytes <= flow.BytesUp && usage.DownloadBytes <= flow.BytesDown {
					continues = true
				}
				if continues {
					previousRaw = usage.RawBytes
					previousUpload = usage.UploadBytes
					previousDownload = usage.DownloadBytes
					previousUsed = usage.UsedBytes
				}
			}

			charge, err := metering.CalculateCharge(metering.FlowCounters{Raw: cumulativeRaw, Upload: flow.BytesUp, Download: flow.BytesDown}, metering.FlowCounters{Raw: previousRaw, Upload: previousUpload, Download: previousDownload}, endpoint.MultiplierMilli, subscription.FlowTotal, subscription.FlowUsed)
			if err != nil {
				return err
			}
			deltaRaw, deltaUpload, deltaDownload, charged := charge.Raw, charge.Upload, charge.Download, charge.Charged
			now := time.Now().UTC()
			recordAt := event.OccurredAt
			if recordAt.IsZero() {
				recordAt = now
			}
			record = model.TrafficRecord{
				EntitlementEpoch:        credential.EntitlementEpoch,
				UserID:                  subscription.UserID,
				SubscriptionID:          subscription.ID,
				NodeID:                  in.NodeID,
				ProtocolEndpointID:      endpoint.ID,
				ReportID:                event.EventID,
				Nonce:                   completionNonce(event.EventID),
				FlowID:                  flow.FlowID,
				EventType:               event.EventType,
				EventRevision:           flow.Revision,
				RawBytes:                deltaRaw,
				UploadBytes:             deltaUpload,
				DownloadBytes:           deltaDownload,
				TrafficCalcMode:         subscription.TrafficCalcMode,
				ProtocolMultiplierMilli: endpoint.MultiplierMilli,
				UsedBytes:               charged,
				At:                      recordAt,
				Meta:                    "{}",
			}
			if err := storeNativeSettlement(tx, &record, subscription.StartAt); err != nil {
				return err
			}
			if err := RecordUsageProjections(tx, []model.TrafficRecord{record}); err != nil {
				return err
			}
			if charged > 0 {
				subscription.FlowUsed += charged
				if subscription.FlowUsed >= subscription.FlowTotal {
					subscription.FlowUsed = subscription.FlowTotal
					exhausted = currentEpoch
				}
				if currentEpoch {
					subscription = model.Subscription(entitlements.RecordQuotaExhaustion(entitlements.Subscription(subscription), now))
					if err := tx.Model(&subscription).Updates(map[string]interface{}{
						"flow_used": subscription.FlowUsed,
						"status":    subscription.Status,
						"ended_at":  subscription.EndedAt, "end_reason": subscription.EndReason,
						"updated_at": now,
					}).Error; err != nil {
						return err
					}
				} else {
					if err := tx.Model(&model.ClosedEntitlement{}).Where("subscription_id = ? AND epoch = ?", subscription.ID, credential.EntitlementEpoch).Updates(map[string]any{"flow_used": subscription.FlowUsed, "updated_at": now}).Error; err != nil {
						return err
					}
				}
			}

			usage.ProtocolCredentialID = credential.ID
			usage.NodeID = in.NodeID
			usage.FlowID = targetUsageKey
			usage.SubscriptionID = subscription.ID
			usage.ProtocolEndpointID = endpoint.ID
			usage.PrincipalKey = credential.PrincipalKey
			usage.Revision = flow.Revision
			if strings.TrimSpace(event.CoreInstanceID) != "" {
				usage.Revision = event.Sequence
			}
			usage.RawBytes = cumulativeRaw
			usage.UploadBytes = flow.BytesUp
			usage.DownloadBytes = flow.BytesDown
			usage.UsedBytes = previousUsed + charged
			usage.Status = "completed"
			usage.LastEventID = event.EventID
			usage.LastSeenAt = recordAt
			usage.CompletedAt = &now
			if err := saveFlowUsage(tx, &usage, usageExists, previousUsageKey, !currentEpoch); err != nil {
				return err
			}

			if exhausted {
				if s.EnqueuePublications == nil {
					return errors.New("subscription publication capability unavailable")
				}
				if err := s.EnqueuePublications(tx, subscription.ID, 0); err != nil {
					return err
				}
				if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ?", subscription.ID,
					[]string{"active", "prepared"}).
					Updates(map[string]interface{}{"status": "expired", "updated_at": now}).Error; err != nil {
					return err
				}
			}
			return nil
		}, options)
		// Coverage can change after preparation when a concurrent grant retires
		// credentials. Roll back first, recover within the same remaining row
		// budget, and recheck ownership. Genuine ambiguity is never retried here.
		var conflict *mysqlDriver.MySQLError
		if attempt < 2 && ((errors.As(err, &conflict) && (conflict.Number == 1213 || conflict.Number == 1205)) || errors.Is(err, network.ErrResourcesBusy)) {
			// GORM has rolled back the complete attempt, including quota, floor,
			// identity and results. Retry only known transactional lock failures.
			timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return metering.TrafficRecord{}, false, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if !errors.Is(err, credentialstore.ErrPrincipalIndexIncomplete) && !errors.Is(err, ErrNativeReportClaimed) && !errors.Is(err, ErrNativeAuthorityChanged) {
			break
		}
	}
	if err != nil {
		return metering.TrafficRecord{}, false, err
	}
	TouchCredentialActivity(ctx, s.DB, []uint{activityID})
	return metering.TrafficRecord(record), exhausted, nil
}
