-- Setup keys are now one-time and expire after a day (see
-- netbird.CreateSetupKey). NULL for keys issued before that, which are
-- reusable and last 30 days.
ALTER TABLE vpn_access ADD COLUMN key_expires_at TIMESTAMPTZ;
