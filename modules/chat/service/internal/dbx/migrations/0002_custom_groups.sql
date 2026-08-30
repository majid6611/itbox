-- Employee-created private groups — distinct from the LDAP-group channels
-- in 0001, which are open to every employee by design. A custom group is
-- invisible to anyone not an explicit member: not just its messages, but
-- its very existence (it never appears in another employee's group list).
CREATE TABLE IF NOT EXISTS chat_custom_groups (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_group_members (
    group_id BIGINT NOT NULL REFERENCES chat_custom_groups(id) ON DELETE CASCADE,
    username TEXT NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, username)
);

-- A message now targets exactly one of three things: an open LDAP-group
-- channel, a DM, or a private custom group. Drop and recreate the old
-- two-way CHECK as a three-way one — safe on an already-populated table
-- since every existing row still satisfies it (custom_group_id is NULL
-- for all of them until this column exists).
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS custom_group_id BIGINT REFERENCES chat_custom_groups(id) ON DELETE CASCADE;

ALTER TABLE chat_messages DROP CONSTRAINT IF EXISTS chat_messages_target;
ALTER TABLE chat_messages ADD CONSTRAINT chat_messages_target CHECK (
    (CASE WHEN group_name IS NOT NULL THEN 1 ELSE 0 END) +
    (CASE WHEN recipient_username IS NOT NULL THEN 1 ELSE 0 END) +
    (CASE WHEN custom_group_id IS NOT NULL THEN 1 ELSE 0 END) = 1
);

CREATE INDEX IF NOT EXISTS chat_messages_custom_group_idx ON chat_messages (custom_group_id, id) WHERE custom_group_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS chat_group_members_username_idx ON chat_group_members (username);
