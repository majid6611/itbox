-- Which view (month/week/day) the portal calendar opens to by default —
-- an admin display preference, same category as business hours/slot size
-- already in calendar_settings.
ALTER TABLE calendar_settings ADD COLUMN default_view TEXT NOT NULL DEFAULT 'month';
