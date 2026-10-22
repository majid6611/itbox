-- The phone menu ("press 1 for IT"): one level, one row per key. targets is
-- the list of extension numbers that ring together when the key is pressed.
ALTER TABLE voip_settings ADD COLUMN IF NOT EXISTS ivr_enabled BOOLEAN NOT NULL DEFAULT false;
-- The one extension allowed to re-record the greeting by phone (dial *99).
ALTER TABLE voip_settings ADD COLUMN IF NOT EXISTS ivr_recorder TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS voip_ivr_options (
    digit   TEXT PRIMARY KEY CHECK (digit ~ '^[1-9]$'),
    label   TEXT NOT NULL,
    targets TEXT NOT NULL
);
