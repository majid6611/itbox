-- Replying to a message: a pointer to the quoted message. If the original
-- is ever hard-deleted the reply just loses its quote; a normal delete is
-- a soft delete, so the quote shows "message deleted" instead.
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS reply_to_id BIGINT REFERENCES chat_messages(id) ON DELETE SET NULL;

-- 'simple' (no stemming, no stop words) because the company's messages
-- aren't necessarily English.
CREATE INDEX IF NOT EXISTS chat_messages_fts_idx ON chat_messages USING GIN (to_tsvector('simple', content));
