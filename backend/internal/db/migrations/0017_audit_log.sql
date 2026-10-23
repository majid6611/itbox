-- The audit log: who did what, and when. Append-only, and each row carries a
-- hash of the row before it, so a changed or removed row shows up when the
-- chain is verified (see internal/audit).
-- audit_log has existed since the first version as an empty placeholder that
-- nothing ever wrote to; this turns it into the real thing.
ALTER TABLE audit_log
    ADD COLUMN actor_kind TEXT NOT NULL DEFAULT '',  -- admin | employee | '' (not signed in)
    ADD COLUMN ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN status INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN detail TEXT NOT NULL DEFAULT '',
    ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log
    ALTER COLUMN actor SET DEFAULT '',
    ALTER COLUMN target SET DEFAULT '';
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor);

-- Rows can't be edited, and can only be removed once they are past the
-- one-year retention — enforced by the database, not just by the app.
CREATE FUNCTION audit_log_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'the audit log cannot be edited';
    END IF;
    IF OLD.at > now() - interval '1 year' THEN
        RAISE EXCEPTION 'the audit log keeps entries for one year';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_guard BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_guard();
