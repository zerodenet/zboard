CREATE INDEX IF NOT EXISTS idx_observation_claim_lease ON node_observation_intents(status,lease_until,id);
