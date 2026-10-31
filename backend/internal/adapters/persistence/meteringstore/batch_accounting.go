package meteringstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"time"
)

type BatchAccounting struct {
	Cipher              metering.CredentialDecryptor
	EnqueuePublications func(*gorm.DB, uint, uint) error
}

// Apply shares the event-consumer transaction, including node projections.
// Batch caches never escape this call or survive a commit/rollback.
func (s BatchAccounting) Apply(tx *gorm.DB, events []metering.FlowSample) ([]metering.FlowAccountingResult, error) {
	if err := validateFlowWindowReports(tx, events); err != nil {
		return nil, err
	}
	batch := newFlowBatch(tx)
	if err := batch.loadReports(tx, events); err != nil {
		return nil, err
	}
	exhausted := make([]metering.FlowAccountingResult, 0)
	for _, event := range events {
		if err := tx.Statement.Context.Err(); err != nil {
			return nil, err
		}
		if err := metering.ValidateFlowSample(event); err != nil {
			return nil, err
		}
		result, err := s.project(tx, event, batch)
		if err != nil {
			return nil, err
		}
		if result.Exhausted {
			exhausted = append(exhausted, result)
		}
	}
	if err := batch.flush(tx); err != nil {
		return nil, err
	}
	return exhausted, nil
}
func (s BatchAccounting) project(tx *gorm.DB, in metering.FlowSample, batch *flowBatch) (metering.FlowAccountingResult, error) {
	result := metering.FlowAccountingResult{NodeID: in.NodeID}
	event, flow := in, in
	reportKey := zeroFlowReportKey{result.NodeID, event.EventID}
	endpointID, recorded, err := batch.recordedEndpoint(tx, reportKey)
	if err != nil {
		return result, err
	}
	if recorded {
		result.ProtocolEndpointID = endpointID
		return result, nil
	}

	credential, err := batch.credential(s.Cipher, tx, result.NodeID, flow.PrincipalKey)
	if err != nil {
		return result, err
	}
	flow.PrincipalKey = credential.PrincipalKey
	result.ProtocolEndpointID = credential.ProtocolEndpointID

	subscription, err := batch.subscription(tx, credential.SubscriptionID)
	if err != nil {
		return result, err
	}
	subscription, currentEpoch, err := batch.quotaSubscription(tx, credential, subscription)
	if err != nil {
		return result, err
	}
	endpoint, err := batch.endpoint(tx, credential.ProtocolEndpointID)
	if err != nil {
		return result, err
	}

	cumulativeRaw, err := metering.CheckedTrafficBytes(flow.BytesUp, flow.BytesDown, subscription.TrafficCalcMode)
	if err != nil {
		return result, err
	}
	usage, found, legacy, err := batch.loadUsage(tx, in, credential.ID, metering.FlowCounters{Raw: cumulativeRaw, Upload: flow.BytesUp, Download: flow.BytesDown})
	if err != nil {
		return result, err
	}
	if found && usage.Status == "completed" {
		return result, nil
	}
	if found && !legacy {
		if usage.ProtocolCredentialID != credential.ID {
			return result, errors.New("flow principal changed during its runtime generation")
		}
		if metering.RuntimeScopedFlow(usage.FlowID) && usage.Revision > 0 && event.Sequence > 0 && event.Sequence <= usage.Revision {
			return result, nil
		}
		if usage.RawBytes > cumulativeRaw || usage.UploadBytes > flow.BytesUp || usage.DownloadBytes > flow.BytesDown {
			return result, errors.New("Zero flow cumulative counters regressed within one core instance")
		}
	}

	previousRaw, previousUpload, previousDownload := int64(0), int64(0), int64(0)
	if found {
		previousRaw = usage.RawBytes
		previousUpload = usage.UploadBytes
		previousDownload = usage.DownloadBytes
	}
	cursorSequence := uint64(0)
	if found && !legacy && metering.RuntimeScopedFlow(usage.FlowID) {
		cursorSequence = usage.Revision
	}
	charge, err := metering.ChargeSample(in, metering.FlowCounters{Raw: previousRaw, Upload: previousUpload, Download: previousDownload}, cursorSequence, subscription.TrafficCalcMode, endpoint.MultiplierMilli, subscription.FlowTotal, subscription.FlowUsed)
	if err != nil {
		return result, err
	}
	deltaRaw, deltaUpload, deltaDownload, charged := charge.Raw, charge.Upload, charge.Download, charge.Charged
	now := time.Now().UTC()
	recordAt := event.OccurredAt
	if recordAt.IsZero() {
		recordAt = now
	}
	record := model.TrafficRecord{CreatedAt: tx.NowFunc().UTC(),
		EntitlementEpoch:        credential.EntitlementEpoch,
		UserID:                  subscription.UserID,
		SubscriptionID:          subscription.ID,
		NodeID:                  result.NodeID,
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
		Meta:                    fmt.Sprintf(`{"source_id":%q,"core_instance_id":%q,"sequence":%d,"credential_id":%q,"buffered":true,"accounting_samples":%d,"first_sequence":%d,"rounding_numerator":%d}`, event.SourceID, event.CoreInstanceID, event.Sequence, credential.CredentialID, charge.AccountingSamples, charge.FirstSequence, charge.RoundingNumerator),
	}
	if err := batch.appendRecord(tx, record, subscription.StartAt); err != nil {
		return result, err
	}

	if charged > 0 {
		subscription.FlowUsed += charged
		if subscription.FlowUsed >= subscription.FlowTotal {
			subscription.FlowUsed = subscription.FlowTotal
			result.Exhausted = currentEpoch
		}
		if currentEpoch {
			*subscription = model.Subscription(entitlements.RecordQuotaExhaustion(entitlements.Subscription(*subscription), now))
			batch.dirtySubscriptions[subscription.ID] = now
		} else {
			key := entitlementQuotaKey{subscription.ID, credential.EntitlementEpoch}
			batch.closed[key].FlowUsed = subscription.FlowUsed
			batch.dirtyClosed[key] = now
		}
	}

	previousUsageKey := usage.FlowID
	usageKey := metering.FlowUsageKey(event.CoreInstanceID, flow.FlowID)
	usage.ProtocolCredentialID = credential.ID
	usage.NodeID = result.NodeID
	usage.FlowID = usageKey
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
	usage.UsedBytes += charged
	usage.Status = "active"
	usage.LastEventID = event.EventID
	usage.LastSeenAt = recordAt
	usage.CompletedAt = nil
	if found {
		if err := tx.Save(&usage).Error; err != nil {
			return result, err
		}
	} else if err := tx.Create(&usage).Error; err != nil {
		return result, err
	}

	batch.rememberUsage(previousUsageKey, usage)
	batch.touched[credential.ID] = now
	if result.Exhausted {
		if s.EnqueuePublications == nil {
			return result, errors.New("subscription publication capability unavailable")
		}
		if err := s.EnqueuePublications(tx, subscription.ID, 0); err != nil {
			return result, err
		}
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("subscription_id = ? AND status IN ?", subscription.ID, []string{"active", "prepared"}).
			Updates(map[string]interface{}{"status": "expired", "updated_at": now}).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}
