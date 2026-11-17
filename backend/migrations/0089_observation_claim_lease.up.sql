-- Independent covered due queues; avoid OR + filesort range-lock amplification.
CREATE INDEX idx_observation_claim_lease ON node_observation_intents(status,lease_until,id);
