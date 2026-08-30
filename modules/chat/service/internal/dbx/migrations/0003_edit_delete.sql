-- Deleting a message is a soft delete: the row (and its place in the
-- thread) stays, but content is cleared server-side and the client is
-- told to render a tombstone ("this message was deleted") rather than the
-- real text — matches what every mainstream chat app does, and means a
-- deleted message can't be recovered by inspecting the API response
-- either, not just hidden by the UI.
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
