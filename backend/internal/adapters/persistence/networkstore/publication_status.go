package networkstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Publication status is a live projection: a pending node request takes
// precedence over the endpoint's previous deployment record.
const publicationStatusSQL = `CASE
 WHEN publication.node_id IS NOT NULL THEN CASE
  WHEN publication.lease_token <> '' AND publication.lease_until > ? THEN 'running'
  WHEN publication.lease_token <> '' OR (publication.last_error <> '' AND publication.next_attempt_at > ?) THEN 'failed'
  ELSE 'queued' END
 ELSE COALESCE(deployment.status, 'never') END`

// A managed front entry depends on both its entry node and the listener's
// landing node. The combined service list must reflect either pending request.
const servicePublicationStatusSQL = `CASE
 WHEN publication.node_id IS NOT NULL OR landing_publication.node_id IS NOT NULL THEN CASE
  WHEN (publication.lease_token <> '' AND publication.lease_until > ?) OR (landing_publication.lease_token <> '' AND landing_publication.lease_until > ?) THEN 'running'
  WHEN (publication.lease_token <> '' OR (publication.last_error <> '' AND publication.next_attempt_at > ?)) OR
       (landing_publication.lease_token <> '' OR (landing_publication.last_error <> '' AND landing_publication.next_attempt_at > ?)) THEN 'failed'
  ELSE 'queued' END
 ELSE COALESCE(deployment.status, 'never') END`

func (s Inventory) withPublicationStatus(ctx context.Context, base *gorm.DB, endpointColumn, nodeColumn string) *gorm.DB {
	latest := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).
		Select("protocol_endpoint_id, MAX(id) AS latest_id").Group("protocol_endpoint_id")
	return base.Session(&gorm.Session{}).
		Joins("LEFT JOIN (?) AS latest ON latest.protocol_endpoint_id = "+endpointColumn, latest).
		Joins("LEFT JOIN protocol_deployments AS deployment ON deployment.id = latest.latest_id").
		Joins("LEFT JOIN node_config_publishes AS publication ON publication.node_id = " + nodeColumn)
}

func (s Inventory) withServicePublicationStatus(ctx context.Context, base *gorm.DB) *gorm.DB {
	return s.withPublicationStatus(ctx, base, "service.endpoint_id", "service.node_id").
		Joins("LEFT JOIN node_config_publishes AS landing_publication ON landing_publication.node_id = service.landing_node_id")
}

func publicationObservationTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}
