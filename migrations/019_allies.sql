-- +goose Up
-- +goose StatementBegin
CREATE TABLE ally_agreement_types (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    active     INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO ally_agreement_types (name, sort_order) VALUES
    ('Trucks', 0), ('Trains', 1), ('Secret Tasks', 2);

CREATE TABLE allies (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    server     TEXT NOT NULL,
    tag        TEXT NOT NULL,
    name       TEXT NOT NULL,
    active     INTEGER NOT NULL DEFAULT 1,
    notes      TEXT NOT NULL DEFAULT '',
    contact    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ally_agreements (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    ally_id             INTEGER NOT NULL,
    agreement_type_id   INTEGER NOT NULL,
    FOREIGN KEY (ally_id) REFERENCES allies(id) ON DELETE CASCADE,
    FOREIGN KEY (agreement_type_id) REFERENCES ally_agreement_types(id) ON DELETE CASCADE,
    UNIQUE(ally_id, agreement_type_id)
);

ALTER TABLE rank_permissions ADD COLUMN view_allies   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_allies INTEGER NOT NULL DEFAULT 0;
UPDATE rank_permissions SET view_allies   = 1 WHERE rank IN ('R1', 'R2', 'R3', 'R4', 'R5');
UPDATE rank_permissions SET manage_allies = 1 WHERE rank IN ('R4', 'R5');
-- +goose StatementEnd
