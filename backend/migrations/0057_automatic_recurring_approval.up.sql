ALTER TABLE subscription_automatic_renewals ADD COLUMN agreement_version int NOT NULL DEFAULT 1;
ALTER TABLE subscription_automatic_renewals ADD COLUMN approval_snapshot TEXT NOT NULL DEFAULT ('');
