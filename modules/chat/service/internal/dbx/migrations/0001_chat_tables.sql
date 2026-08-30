-- Same reasoning as the wiki module's own migration: this reuses the
-- platform's shared Postgres (see dbx.go's Migrate, same schema_migrations
-- tracking table core and every other feature-module use), so every
-- statement here is IF NOT EXISTS.

-- A message is either a group message (group_name set, matching an LDAP
-- group name directly — no separate "channel" concept to keep in sync)
-- or a DM (recipient_username set). Exactly one of the two is set,
-- enforced below rather than trusted to application code.
CREATE TABLE IF NOT EXISTS chat_messages (
    id BIGSERIAL PRIMARY KEY,
    sender_username TEXT NOT NULL,
    group_name TEXT,
    recipient_username TEXT,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chat_messages_target CHECK (
        (group_name IS NOT NULL AND recipient_username IS NULL) OR
        (group_name IS NULL AND recipient_username IS NOT NULL)
    )
);

-- Backfill-on-reconnect ("everything since message id N") is the actual
-- source of truth for delivery, not the live WebSocket push — this index
-- is what makes that query cheap regardless of channel/DM history size.
CREATE INDEX IF NOT EXISTS chat_messages_group_idx ON chat_messages (group_name, id) WHERE group_name IS NOT NULL;
CREATE INDEX IF NOT EXISTS chat_messages_dm_idx ON chat_messages (recipient_username, sender_username, id) WHERE recipient_username IS NOT NULL;

CREATE TABLE IF NOT EXISTS chat_attachments (
    id SERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    s3_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    uploaded_by TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
