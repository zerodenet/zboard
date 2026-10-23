package model

// Recording initialization makes a later missing configuration distinguishable
// from a legacy database that has always used the seven-day default.
const SubscriptionRecoveryPolicySchemaVersion = "0050_subscription_recovery_policy.up.sql"
