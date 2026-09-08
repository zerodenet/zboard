CREATE TABLE IF NOT EXISTS plugin_installations (
 id TEXT PRIMARY KEY, version_id TEXT NOT NULL, name TEXT NOT NULL, publisher TEXT NOT NULL,
 state TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT FALSE, generation INTEGER NOT NULL DEFAULT 1,
 config_revision INTEGER NOT NULL DEFAULT 0, config_ciphertext TEXT NOT NULL, last_error TEXT NOT NULL,
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS plugin_versions (
 id TEXT PRIMARY KEY, plugin_id TEXT NOT NULL, version TEXT NOT NULL, digest TEXT NOT NULL,
 publisher TEXT NOT NULL, manifest TEXT NOT NULL, created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plugin_versions_plugin_id ON plugin_versions(plugin_id);
CREATE TABLE IF NOT EXISTS plugin_operations (
 id TEXT PRIMARY KEY, plugin_id TEXT NOT NULL, action TEXT NOT NULL, state TEXT NOT NULL,
 actor TEXT NOT NULL, message TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plugin_operations_plugin_id ON plugin_operations(plugin_id);
CREATE TABLE IF NOT EXISTS plugin_host_leases (
 id INTEGER PRIMARY KEY, owner TEXT NOT NULL, epoch INTEGER NOT NULL, expires_at DATETIME NOT NULL
);
