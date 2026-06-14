package db

import (
	"database/sql"
	"fmt"
	"time"
	"worldcup-bet-bot/internal/models"
)

func (db *DB) UpsertMatch(m *models.Match) error {
	query := `INSERT INTO matches (external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(external_id) DO UPDATE SET
	            home_team      = excluded.home_team,
	            away_team      = excluded.away_team,
	            match_date     = excluded.match_date,
	            kickoff_utc    = excluded.kickoff_utc,
	            status         = excluded.status,
	            winner         = excluded.winner,
	            home_score     = excluded.home_score,
	            away_score     = excluded.away_score,
	            last_synced_at = CURRENT_TIMESTAMP`

	_, err := db.Exec(query, m.ExternalID, m.HomeTeam, m.AwayTeam, m.MatchDate, m.KickoffUTC, m.Status, m.Winner, m.HomeScore, m.AwayScore)
	if err != nil {
		return fmt.Errorf("failed to upsert match: %w", err)
	}

	return nil
}

func (db *DB) GetMatchByID(id int64) (*models.Match, error) {
	query := `SELECT id, external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at, notified_30min FROM matches WHERE id = ?`

	var match models.Match
	var winner sql.NullString
	var homeScore sql.NullInt64
	var awayScore sql.NullInt64
	var notified int

	err := db.QueryRow(query, id).Scan(
		&match.ID, &match.ExternalID, &match.HomeTeam, &match.AwayTeam,
		&match.MatchDate, &match.KickoffUTC, &match.Status,
		&winner, &homeScore, &awayScore, &match.LastSyncedAt, &notified,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get match by id: %w", err)
	}
	if winner.Valid {
		match.Winner = winner.String
	}
	if homeScore.Valid {
		match.HomeScore = int(homeScore.Int64)
	}
	if awayScore.Valid {
		match.AwayScore = int(awayScore.Int64)
	}
	match.NotifiedPreMatch = notified != 0
	return &match, nil
}

func (db *DB) GetMatchByExternalID(externalID int) (*models.Match, error) {
	query := `SELECT id, external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at, notified_30min FROM matches WHERE external_id = ?`

	var match models.Match
	var winner sql.NullString
	var homeScore sql.NullInt64
	var awayScore sql.NullInt64
	var notified int

	err := db.QueryRow(query, externalID).Scan(
		&match.ID,
		&match.ExternalID,
		&match.HomeTeam,
		&match.AwayTeam,
		&match.MatchDate,
		&match.KickoffUTC,
		&match.Status,
		&winner,
		&homeScore,
		&awayScore,
		&match.LastSyncedAt,
		&notified,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get match: %w", err)
	}

	if winner.Valid {
		match.Winner = winner.String
	}
	if homeScore.Valid {
		match.HomeScore = int(homeScore.Int64)
	}
	if awayScore.Valid {
		match.AwayScore = int(awayScore.Int64)
	}
	match.NotifiedPreMatch = notified != 0

	return &match, nil
}

// GetStaleMatches returns matches whose kickoff is older than the given cutoff
// but are still in a non-terminal status (not FINISHED/CANCELLED/POSTPONED).
// Used by the reconciliation job to catch matches missed by the regular poller.
func (db *DB) GetStaleMatches(cutoff time.Time) ([]*models.Match, error) {
	query := `SELECT id, external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at, notified_30min
	          FROM matches
	          WHERE kickoff_utc < ?
	            AND status NOT IN ('FINISHED', 'CANCELLED', 'POSTPONED')
	          ORDER BY kickoff_utc ASC`

	rows, err := db.Query(query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale matches: %w", err)
	}
	defer rows.Close()

	return scanMatches(rows)
}

func (db *DB) GetMatchesByDate(date time.Time) ([]*models.Match, error) {
	query := `SELECT id, external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at, notified_30min
	          FROM matches WHERE DATE(match_date) = DATE(?) ORDER BY kickoff_utc ASC`

	rows, err := db.Query(query, date)
	if err != nil {
		return nil, fmt.Errorf("failed to query matches by date: %w", err)
	}
	defer rows.Close()

	return scanMatches(rows)
}

func (db *DB) GetUpcomingMatches(limit int) ([]*models.Match, error) {
	query := `SELECT id, external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at, notified_30min
	          FROM matches WHERE status IN ('SCHEDULED', 'TIMED') AND kickoff_utc > datetime('now') ORDER BY kickoff_utc ASC LIMIT ?`

	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query upcoming matches: %w", err)
	}
	defer rows.Close()

	return scanMatches(rows)
}

func (db *DB) GetActiveMatches() ([]*models.Match, error) {
	query := `SELECT id, external_id, home_team, away_team, match_date, kickoff_utc, status, winner, home_score, away_score, last_synced_at, notified_30min
	          FROM matches WHERE status IN ('SCHEDULED', 'TIMED', 'IN_PLAY', 'PAUSED') ORDER BY kickoff_utc ASC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active matches: %w", err)
	}
	defer rows.Close()

	return scanMatches(rows)
}

func (db *DB) UpdateMatchResult(externalID int, winner string, homeScore, awayScore int) error {
	query := `UPDATE matches SET status = 'FINISHED', winner = ?, home_score = ?, away_score = ? WHERE external_id = ?`

	_, err := db.Exec(query, winner, homeScore, awayScore, externalID)
	if err != nil {
		return fmt.Errorf("failed to update match result: %w", err)
	}

	return nil
}

// GetMatchesStartingIn30Min returns SCHEDULED/TIMED matches whose kickoff is
// between now and now+30 minutes that have not been notified yet.
func (db *DB) GetMatchesStartingIn30Min() ([]*models.Match, error) {
	query := `
		SELECT id, external_id, home_team, away_team, match_date, kickoff_utc,
		       status, winner, home_score, away_score, last_synced_at, notified_30min
		FROM matches
		WHERE status IN ('SCHEDULED', 'TIMED')
		  AND kickoff_utc > datetime('now')
		  AND kickoff_utc <= datetime('now', '+30 minutes')
		  AND notified_30min = 0
		ORDER BY kickoff_utc ASC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query pre-match window: %w", err)
	}
	defer rows.Close()
	return scanMatches(rows)
}

// MarkMatchNotified sets notified_30min = 1 to prevent re-sending the 30-min reminder.
func (db *DB) MarkMatchNotified(matchID int64) error {
	_, err := db.Exec(`UPDATE matches SET notified_30min = 1 WHERE id = ?`, matchID)
	if err != nil {
		return fmt.Errorf("failed to mark match notified: %w", err)
	}
	return nil
}

func scanMatches(rows *sql.Rows) ([]*models.Match, error) {
	var matches []*models.Match

	for rows.Next() {
		var match models.Match
		var winner sql.NullString
		var homeScore sql.NullInt64
		var awayScore sql.NullInt64
		var notified int

		err := rows.Scan(
			&match.ID,
			&match.ExternalID,
			&match.HomeTeam,
			&match.AwayTeam,
			&match.MatchDate,
			&match.KickoffUTC,
			&match.Status,
			&winner,
			&homeScore,
			&awayScore,
			&match.LastSyncedAt,
			&notified,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan match: %w", err)
		}

		if winner.Valid {
			match.Winner = winner.String
		}
		if homeScore.Valid {
			match.HomeScore = int(homeScore.Int64)
		}
		if awayScore.Valid {
			match.AwayScore = int(awayScore.Int64)
		}
		match.NotifiedPreMatch = notified != 0

		matches = append(matches, &match)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating matches: %w", err)
	}

	return matches, nil
}
