ALTER TABLE subscriptions ADD COLUMN entitlement_epoch bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE protocol_credentials ADD COLUMN entitlement_epoch bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE protocol_credentials ADD COLUMN current_scope tinyint unsigned NULL DEFAULT 1;
ALTER TABLE traffic_records ADD COLUMN entitlement_epoch bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE quota_events ADD COLUMN entitlement_epoch bigint unsigned NOT NULL DEFAULT 0;
CREATE TABLE closed_entitlements (
 subscription_id bigint unsigned NOT NULL,
 epoch bigint unsigned NOT NULL,
 user_id bigint unsigned NOT NULL,
 transition_order_id bigint unsigned NOT NULL,
 flow_total bigint NOT NULL,
 closing_flow_used bigint NOT NULL,
 flow_used bigint NOT NULL,
 traffic_calc_mode smallint NOT NULL,
 start_at datetime(3) NOT NULL,
 end_at datetime(3) NOT NULL,
 closed_at datetime(3) NOT NULL,
 updated_at datetime(3) NOT NULL,
 PRIMARY KEY(subscription_id,epoch)
);
CREATE UNIQUE INDEX ux_credential_current_endpoint ON protocol_credentials(subscription_id,protocol_endpoint_id,current_scope);
DROP INDEX ux_protocol_credential_subscription_endpoint ON protocol_credentials;
