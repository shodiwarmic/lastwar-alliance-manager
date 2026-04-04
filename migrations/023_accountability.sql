-- +goose Up
-- +goose StatementBegin

-- Restore train no-show tracking (removed when train_schedules was dropped in 012)
ALTER TABLE train_logs ADD COLUMN showed_up INTEGER NOT NULL DEFAULT 1;

-- Post-event storm attendance (UNIQUE on date+member prevents double-logging)
CREATE TABLE storm_attendance (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  storm_date    TEXT    NOT NULL,
  member_id     INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  status        TEXT    NOT NULL DEFAULT 'not_enrolled' CHECK(status IN ('attended','no_show','excused','not_enrolled')),
  excuse_reason TEXT    NOT NULL DEFAULT '',
  recorded_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(storm_date, member_id)
);
CREATE INDEX idx_storm_attendance_member ON storm_attendance(member_id);
CREATE INDEX idx_storm_attendance_date   ON storm_attendance(storm_date DESC);

-- Accountability strikes
CREATE TABLE accountability_strikes (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  member_id      INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  strike_type    TEXT    NOT NULL CHECK(strike_type IN ('vs_below_threshold','train_no_show','storm_no_show','manual')),
  reason         TEXT    NOT NULL DEFAULT '',
  ref_date       TEXT,
  status         TEXT    NOT NULL DEFAULT 'active' CHECK(status IN ('active','excused')),
  excused_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
  excused_reason TEXT    NOT NULL DEFAULT '',
  created_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_strikes_member ON accountability_strikes(member_id);

-- Permission columns
ALTER TABLE rank_permissions ADD COLUMN view_accountability   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_accountability INTEGER NOT NULL DEFAULT 0;
UPDATE rank_permissions SET view_accountability = 1, manage_accountability = 1 WHERE rank IN ('R4','R5');

-- +goose StatementEnd
