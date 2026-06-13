package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

const initSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
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
CREATE INDEX IF NOT EXISTS idx_bets_user      ON bets(user_id);`

const migration002SQL = `CREATE TABLE IF NOT EXISTS groups (
    chat_id     INTEGER PRIMARY KEY,
    title       TEXT NOT NULL DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE bets ADD COLUMN group_chat_id INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_bets_group ON bets(group_chat_id);`

// migration003SQL changes bets UNIQUE constraint from (match_id, user_id)
// to (match_id, user_id, group_chat_id) so a user can bet on the same match
// in different groups independently.
const migration003SQL = `
CREATE TABLE bets_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    match_id            INTEGER NOT NULL REFERENCES matches(id),
    user_id             INTEGER NOT NULL REFERENCES users(id),
    picked_team         TEXT NOT NULL,
    outcome             TEXT,
    telegram_message_id INTEGER,
    sheets_row          INTEGER,
    created_at          DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at         DATETIME,
    group_chat_id       INTEGER NOT NULL DEFAULT 0,
    UNIQUE(match_id, user_id, group_chat_id)
);
INSERT INTO bets_new SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id FROM bets;
DROP TABLE bets;
ALTER TABLE bets_new RENAME TO bets;
CREATE INDEX IF NOT EXISTS idx_bets_match ON bets(match_id);
CREATE INDEX IF NOT EXISTS idx_bets_user  ON bets(user_id);
CREATE INDEX IF NOT EXISTS idx_bets_group ON bets(group_chat_id);
`

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{sqlDB}

	// Run migrations
	if err := db.runMigrations(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

func (db *DB) runMigrations() error {
	_, err := db.Exec(initSQL)
	if err != nil {
		return err
	}

	// Run migration 002 if not yet applied
	var version int
	err = db.QueryRow("SELECT version FROM schema_migrations WHERE version = 2").Scan(&version)
	if err == sql.ErrNoRows {
		// Migration 002 not yet applied, apply it
		_, err = db.Exec(migration002SQL)
		if err != nil {
			return err
		}

		// Record the migration
		_, err = db.Exec("INSERT INTO schema_migrations (version) VALUES (2)")
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Run migration 003 if not yet applied
	err = db.QueryRow("SELECT version FROM schema_migrations WHERE version = 3").Scan(&version)
	if err == sql.ErrNoRows {
		_, err = db.Exec(migration003SQL)
		if err != nil {
			return err
		}
		_, err = db.Exec("INSERT INTO schema_migrations (version) VALUES (3)")
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}
