package meteringstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	event, flow := in, in
	var record model.TrafficRecord
	exhausted := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("node_id = ? AND report_id = ?", in.NodeID, event.EventID).First(&record).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		credential, credentialErr := ResolveCompletionCredential(tx, s.Cipher, in.NodeID, flow.PrincipalKey)
		if credentialErr != nil {
			return credentialErr
		}
		flow.PrincipalKey = credential.PrincipalKey
		var subscription model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&subscription, credential.SubscriptionID).Error; err != nil {
			return err
		}
		var endpoint model.ProtocolEndpoint
		if err := tx.First(&endpoint, credential.ProtocolEndpointID).Error; err != nil {
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
			Meta:                    fmt.Sprintf(`{"source_id":%q,"core_instance_id":%q,"sequence":%d,"credential_id":%q,"continued_flow":%t}`, event.SourceID, event.CoreInstanceID, event.Sequence, credential.CredentialID, continues),
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if charged > 0 {
			subscription.FlowUsed += charged
			if subscription.FlowUsed >= subscription.FlowTotal {
				subscription.FlowUsed = subscription.FlowTotal
				subscription.Status = "expired"
				exhausted = true
			}
			if err := tx.Model(&subscription).Updates(map[string]interface{}{
				"flow_used":  subscription.FlowUsed,
				"status":     subscription.Status,
				"updated_at": now,
			}).Error; err != nil {
				return err
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
		if usageExists {
			if err := tx.Save(&usage).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&usage).Error; err != nil {
			return err
		}

		if err := tx.Model(&credential).Updates(map[string]interface{}{"last_used_at": now, "updated_at": now}).Error; err != nil {
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
	})
	if err != nil {
		return metering.TrafficRecord{}, false, err
	}
	return metering.TrafficRecord(record), exhausted, nil
}
