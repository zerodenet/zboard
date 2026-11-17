ALTER TABLE subscription_automatic_renewals ADD COLUMN agreement_version integer NOT NULL DEFAULT 1;
ALTER TABLE subscription_automatic_renewals ADD COLUMN approval_snapshot TEXT NOT NULL DEFAULT '';
