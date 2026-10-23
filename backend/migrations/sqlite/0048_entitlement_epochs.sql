ALTER TABLE subscriptions ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE protocol_credentials ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE protocol_credentials ADD COLUMN current_scope INTEGER NULL DEFAULT 1;
ALTER TABLE traffic_records ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE quota_events ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0;
CREATE TABLE closed_entitlements (
 subscription_id INTEGER NOT NULL,
 epoch INTEGER NOT NULL,
 user_id INTEGER NOT NULL,
 transition_order_id INTEGER NOT NULL,
 flow_total INTEGER NOT NULL,
 closing_flow_used INTEGER NOT NULL,
 flow_used INTEGER NOT NULL,
 traffic_calc_mode INTEGER NOT NULL,
 start_at DATETIME NOT NULL,
 end_at DATETIME NOT NULL,
 closed_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 PRIMARY KEY(subscription_id,epoch)
);
CREATE UNIQUE INDEX ux_credential_current_endpoint ON protocol_credentials(subscription_id,protocol_endpoint_id,current_scope);
DROP INDEX ux_protocol_credential_subscription_endpoint;
