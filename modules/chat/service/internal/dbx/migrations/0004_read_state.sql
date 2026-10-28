-- Per-person "read up to here" marker for each thread, so unread counts
-- survive a reload and cover messages that arrived while offline.
-- target_key uses the same scheme as the client: "group:<name>",
-- "custom:<id>" or "dm:<other username>".
CREATE TABLE IF NOT EXISTS chat_read_state (
    username TEXT NOT NULL,
    target_key TEXT NOT NULL,
    last_read_id BIGINT NOT NULL,
    PRIMARY KEY (username, target_key)
);

-- A thread with no read-state row counts as read up to this id, so rolling
-- this out doesn't light up every existing thread with its whole history
-- as "unread". Threads that start later have only newer ids, so they count
-- correctly. A fresh install gets 0.
CREATE TABLE IF NOT EXISTS chat_read_baseline (
    id INT PRIMARY KEY CHECK (id = 1),
    message_id BIGINT NOT NULL
);
INSERT INTO chat_read_baseline (id, message_id)
SELECT 1, COALESCE(MAX(id), 0) FROM chat_messages
ON CONFLICT DO NOTHING;
