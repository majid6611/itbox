-- Calendar module display settings: business hours + time-grid slot size
-- for the week/day views, and a color per employee so the shared company
-- calendar can visually tell whose event is whose. Pure display
-- preferences, not calendar data — the events themselves live entirely in
-- Radicale (see modules/calendar-radicale), nothing about them is stored
-- here.
CREATE TABLE calendar_settings (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    start_time TEXT NOT NULL DEFAULT '07:00',
    end_time TEXT NOT NULL DEFAULT '20:00',
    slot_duration_minutes INT NOT NULL DEFAULT 30
);

CREATE TABLE calendar_member_colors (
    username TEXT PRIMARY KEY,
    color TEXT NOT NULL
);
