CREATE TABLE devices (
 device_id TEXT PRIMARY KEY,
 last_seen_at INTEGER,
 created_at INTEGER NOT NULL
);
CREATE TABLE metric_history (
 device_id TEXT NOT NULL REFERENCES devices(device_id),
 measurement TEXT NOT NULL,
 tags_json TEXT NOT NULL,
 field TEXT NOT NULL,
 sampled_at INTEGER NOT NULL,
 received_at INTEGER NOT NULL,
 value REAL NOT NULL,
 PRIMARY KEY(device_id,measurement,tags_json,field,sampled_at)
);
CREATE INDEX history_expiry ON metric_history(sampled_at);
CREATE TABLE metric_latest (
 device_id TEXT NOT NULL REFERENCES devices(device_id),
 measurement TEXT NOT NULL,
 tags_json TEXT NOT NULL,
 field TEXT NOT NULL,
 sampled_at INTEGER NOT NULL,
 received_at INTEGER NOT NULL,
 value REAL NOT NULL,
 PRIMARY KEY(device_id,measurement,tags_json,field)
);
PRAGMA user_version = 1;
