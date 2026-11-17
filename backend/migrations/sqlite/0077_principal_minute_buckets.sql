CREATE TABLE principal_statistics_minutes (
 scope_type text NOT NULL,scope_id integer NOT NULL,minute_at datetime NOT NULL,
 peak integer NOT NULL,sample_count integer NOT NULL,
 PRIMARY KEY(scope_type,scope_id,minute_at));
CREATE INDEX idx_principal_minute_retention ON principal_statistics_minutes(minute_at,scope_type,scope_id);
