CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id     INTEGER NOT NULL UNIQUE,
    username        TEXT,
    display_name    TEXT NOT NULL,
    registered_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS matches (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    external_id     INTEGER NOT NULL UNIQUE,
    home_team       TEXT NOT NULL,
    away_team       TEXT NOT NULL,
    match_date      DATE NOT NULL,
    kickoff_utc     DATETIME NOT NULL,
    status          TEXT NOT NULL DEFAULT 'SCHEDULED',
    winner          TEXT,
    home_score      INTEGER,
    away_score      INTEGER,
    last_synced_at  DATETIME
);

CREATE TABLE IF NOT EXISTS bets (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    match_id            INTEGER NOT NULL REFERENCES matches(id),
    user_id             INTEGER NOT NULL REFERENCES users(id),
    picked_team         TEXT NOT NULL,
    outcome             TEXT,
    telegram_message_id INTEGER,
    sheets_row          INTEGER,
    created_at          DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at         DATETIME,
    UNIQUE(match_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_matches_date   ON matches(match_date);
CREATE INDEX IF NOT EXISTS idx_matches_status ON matches(status);
CREATE INDEX IF NOT EXISTS idx_bets_match     ON bets(match_id);
CREATE INDEX IF NOT EXISTS idx_bets_user      ON bets(user_id);
