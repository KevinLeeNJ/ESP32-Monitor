CREATE TABLE IF NOT EXISTS web_credentials (
 id INTEGER PRIMARY KEY CHECK(id=1), salt BLOB NOT NULL, hash BLOB NOT NULL,
 must_change INTEGER NOT NULL, generation INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS web_sessions (
 hash TEXT PRIMARY KEY, csrf TEXT NOT NULL, created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL, last_used INTEGER NOT NULL, generation INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS v2_devices (
 id TEXT PRIMARY KEY, token_hash TEXT UNIQUE, code TEXT UNIQUE, state BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS v2_profiles (
 device_id TEXT NOT NULL REFERENCES v2_devices(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL, body BLOB NOT NULL, PRIMARY KEY(device_id,revision)
);
CREATE TABLE IF NOT EXISTS v2_batches (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 device_id TEXT NOT NULL REFERENCES v2_devices(id) ON DELETE CASCADE,
 batch_id TEXT NOT NULL, hash TEXT NOT NULL, revision INTEGER NOT NULL,
 received_at INTEGER NOT NULL, body BLOB NOT NULL,
 UNIQUE(device_id,batch_id)
);
CREATE INDEX IF NOT EXISTS v2_batch_expiry ON v2_batches(received_at);
CREATE TABLE IF NOT EXISTS web_audit (
 id INTEGER PRIMARY KEY, at INTEGER NOT NULL, event TEXT NOT NULL, device_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS v2_values (
 device_id TEXT NOT NULL REFERENCES v2_devices(id) ON DELETE CASCADE,
 metric TEXT NOT NULL, sampled_at INTEGER NOT NULL, value BLOB NOT NULL,
 revision INTEGER NOT NULL, received_at INTEGER NOT NULL, PRIMARY KEY(device_id,metric,sampled_at)
);
CREATE INDEX IF NOT EXISTS v2_value_expiry ON v2_values(received_at);

PRAGMA user_version = 2;
