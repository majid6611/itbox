-- One row: this install's identity and its licence. The install id is made
-- once and never changes; it is how the vendor tells installs apart.
-- send_monitoring lets the customer stop reports even with a valid licence.
-- last_log_id remembers how far the system log has been reported.
CREATE TABLE licence (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    install_id TEXT NOT NULL,
    licence_key TEXT NOT NULL DEFAULT '',
    send_monitoring BOOLEAN NOT NULL DEFAULT true,
    last_log_id BIGINT NOT NULL DEFAULT 0,
    last_sent_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT ''
);
INSERT INTO licence (install_id) VALUES (gen_random_uuid()::text);
