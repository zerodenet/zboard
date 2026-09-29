-- Repair fixed quota subscriptions whose timed sale unit was not "once".
-- Preserve renewable services and fixed services with automatic cycle resets.
UPDATE subscriptions SET ends_on_quota_exhaustion = TRUE
WHERE lifecycle = 'fixed' AND reset_policy IN (0, 5)
  AND ends_on_quota_exhaustion = FALSE;
