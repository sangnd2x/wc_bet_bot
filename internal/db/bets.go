package db

import (
	"database/sql"
	"fmt"
	"time"

	"worldcup-bet-bot/internal/models"
)

func (db *DB) InsertBet(matchID, userID int64, pickedTeam string, telegramMsgID int, groupChatID int64) error {
	query := `INSERT OR IGNORE INTO bets (match_id, user_id, picked_team, telegram_message_id, group_chat_id)
	          VALUES (?, ?, ?, ?, ?)`

	_, err := db.Exec(query, matchID, userID, pickedTeam, telegramMsgID, groupChatID)
	if err != nil {
		return fmt.Errorf("failed to insert bet: %w", err)
	}

	return nil
}

func (db *DB) GetBetsForMatch(matchID int64) ([]*models.Bet, error) {
	query := `SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id
	          FROM bets WHERE match_id = ? ORDER BY created_at ASC`

	rows, err := db.Query(query, matchID)
	if err != nil {
		return nil, fmt.Errorf("failed to query bets for match: %w", err)
	}
	defer rows.Close()

	return scanBets(rows)
}

func (db *DB) GetBetsForMatchInGroup(matchID, groupChatID int64) ([]*models.Bet, error) {
	query := `SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id
	          FROM bets WHERE match_id = ? AND group_chat_id = ? ORDER BY created_at ASC`

	rows, err := db.Query(query, matchID, groupChatID)
	if err != nil {
		return nil, fmt.Errorf("failed to query bets for match in group: %w", err)
	}
	defer rows.Close()

	return scanBets(rows)
}

func (db *DB) GetBetByMatchAndUser(matchID, userID int64) (*models.Bet, error) {
	query := `SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id
	          FROM bets WHERE match_id = ? AND user_id = ?`

	var bet models.Bet
	var outcome sql.NullString
	var sheetsRow sql.NullInt64
	var resolvedAt sql.NullTime

	err := db.QueryRow(query, matchID, userID).Scan(
		&bet.ID,
		&bet.MatchID,
		&bet.UserID,
		&bet.PickedTeam,
		&outcome,
		&bet.TelegramMessageID,
		&sheetsRow,
		&bet.CreatedAt,
		&resolvedAt,
		&bet.GroupChatID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get bet: %w", err)
	}

	if outcome.Valid {
		bet.Outcome = outcome.String
	}
	if sheetsRow.Valid {
		bet.SheetsRow = int(sheetsRow.Int64)
	}
	if resolvedAt.Valid {
		bet.ResolvedAt = &resolvedAt.Time
	}

	return &bet, nil
}

func (db *DB) UpdateBetSheetsRow(matchID, userID int64, sheetsRow int) error {
	query := `UPDATE bets SET sheets_row = ? WHERE match_id = ? AND user_id = ?`

	_, err := db.Exec(query, sheetsRow, matchID, userID)
	if err != nil {
		return fmt.Errorf("failed to update bet sheets row: %w", err)
	}

	return nil
}

func (db *DB) ResolveBets(matchID int64, winner string) error {
	// Get the match to determine the winner for comparison
	// First, get all bets for this match and their picked teams
	query := `SELECT id, picked_team FROM bets WHERE match_id = ? AND resolved_at IS NULL`

	rows, err := db.Query(query, matchID)
	if err != nil {
		return fmt.Errorf("failed to query bets to resolve: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var betID int64
		var pickedTeam string

		err := rows.Scan(&betID, &pickedTeam)
		if err != nil {
			return fmt.Errorf("failed to scan bet: %w", err)
		}

		// Determine outcome based on picked_team vs winner
		var outcome string
		if winner == "DRAW" {
			outcome = "DRAW"
		} else if pickedTeam == winner {
			outcome = "WIN"
		} else {
			outcome = "LOSS"
		}

		// Update the bet
		updateQuery := `UPDATE bets SET outcome = ?, resolved_at = CURRENT_TIMESTAMP WHERE id = ?`
		_, err = db.Exec(updateQuery, outcome, betID)
		if err != nil {
			return fmt.Errorf("failed to update bet outcome: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating bets: %w", err)
	}

	return nil
}

func (db *DB) GetUserRecord(userID int64) (*models.UserRecord, error) {
	// First, get the user
	query := `SELECT id, telegram_id, username, display_name, registered_at FROM users WHERE id = ?`

	var user models.User
	err := db.QueryRow(query, userID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.DisplayName,
		&user.RegisteredAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Then, get the user's bet statistics
	statsQuery := `SELECT
		COUNT(*) as total,
		SUM(CASE WHEN outcome = 'WIN' THEN 1 ELSE 0 END) as wins,
		SUM(CASE WHEN outcome = 'LOSS' THEN 1 ELSE 0 END) as losses,
		SUM(CASE WHEN outcome = 'DRAW' THEN 1 ELSE 0 END) as draws
	FROM bets WHERE user_id = ? AND resolved_at IS NOT NULL`

	var total sql.NullInt64
	var wins sql.NullInt64
	var losses sql.NullInt64
	var draws sql.NullInt64

	err = db.QueryRow(statsQuery, userID).Scan(&total, &wins, &losses, &draws)
	if err != nil {
		return nil, fmt.Errorf("failed to get user stats: %w", err)
	}

	record := &models.UserRecord{
		User: user,
	}

	if total.Valid {
		record.Total = int(total.Int64)
	}
	if wins.Valid {
		record.Wins = int(wins.Int64)
	}
	if losses.Valid {
		record.Losses = int(losses.Int64)
	}
	if draws.Valid {
		record.Draws = int(draws.Int64)
	}

	return record, nil
}

func (db *DB) GetUserRecordInGroup(userID, groupChatID int64) (*models.UserRecord, error) {
	// First, get the user
	query := `SELECT id, telegram_id, username, display_name, registered_at FROM users WHERE id = ?`

	var user models.User
	err := db.QueryRow(query, userID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.DisplayName,
		&user.RegisteredAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Then, get the user's bet statistics filtered by group
	statsQuery := `SELECT
		COUNT(*) as total,
		SUM(CASE WHEN outcome = 'WIN' THEN 1 ELSE 0 END) as wins,
		SUM(CASE WHEN outcome = 'LOSS' THEN 1 ELSE 0 END) as losses,
		SUM(CASE WHEN outcome = 'DRAW' THEN 1 ELSE 0 END) as draws
	FROM bets WHERE user_id = ? AND group_chat_id = ? AND resolved_at IS NOT NULL`

	var total sql.NullInt64
	var wins sql.NullInt64
	var losses sql.NullInt64
	var draws sql.NullInt64

	err = db.QueryRow(statsQuery, userID, groupChatID).Scan(&total, &wins, &losses, &draws)
	if err != nil {
		return nil, fmt.Errorf("failed to get user stats: %w", err)
	}

	record := &models.UserRecord{
		User: user,
	}

	if total.Valid {
		record.Total = int(total.Int64)
	}
	if wins.Valid {
		record.Wins = int(wins.Int64)
	}
	if losses.Valid {
		record.Losses = int(losses.Int64)
	}
	if draws.Valid {
		record.Draws = int(draws.Int64)
	}

	return record, nil
}

func (db *DB) DeleteBetsForMatch(matchID int64) error {
	_, err := db.Exec(`DELETE FROM bets WHERE match_id = ?`, matchID)
	return err
}

func scanBets(rows *sql.Rows) ([]*models.Bet, error) {
	var bets []*models.Bet

	for rows.Next() {
		var bet models.Bet
		var outcome sql.NullString
		var sheetsRow sql.NullInt64
		var resolvedAt sql.NullTime

		err := rows.Scan(
			&bet.ID,
			&bet.MatchID,
			&bet.UserID,
			&bet.PickedTeam,
			&outcome,
			&bet.TelegramMessageID,
			&sheetsRow,
			&bet.CreatedAt,
			&resolvedAt,
			&bet.GroupChatID,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan bet: %w", err)
		}

		if outcome.Valid {
			bet.Outcome = outcome.String
		}
		if sheetsRow.Valid {
			bet.SheetsRow = int(sheetsRow.Int64)
		}
		if resolvedAt.Valid {
			bet.ResolvedAt = &resolvedAt.Time
		}

		bets = append(bets, &bet)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating bets: %w", err)
	}

	return bets, nil
}

// ActiveBet represents an unresolved bet with match info
type ActiveBet struct {
	HomeTeam   string
	AwayTeam   string
	KickoffUTC time.Time
	UserName   string
	PickedTeam string // "HOME_TEAM" or "AWAY_TEAM"
}

// GetActiveBetsInGroup returns all unresolved bets in a group, joined with match info
func (db *DB) GetActiveBetsInGroup(groupChatID int64) ([]*ActiveBet, error) {
	query := `
		SELECT m.home_team, m.away_team, m.kickoff_utc,
		       COALESCE(NULLIF(u.display_name,''), u.username, 'User') as user_name,
		       b.picked_team
		FROM bets b
		JOIN matches m ON b.match_id = m.id
		JOIN users u ON b.user_id = u.id
		WHERE b.group_chat_id = ? AND b.resolved_at IS NULL
		ORDER BY m.kickoff_utc ASC, b.created_at ASC`

	rows, err := db.Query(query, groupChatID)
	if err != nil {
		return nil, fmt.Errorf("failed to query active bets: %w", err)
	}
	defer rows.Close()

	var bets []*ActiveBet
	for rows.Next() {
		var ab ActiveBet
		if err := rows.Scan(&ab.HomeTeam, &ab.AwayTeam, &ab.KickoffUTC, &ab.UserName, &ab.PickedTeam); err != nil {
			return nil, fmt.Errorf("failed to scan active bet: %w", err)
		}
		bets = append(bets, &ab)
	}
	return bets, rows.Err()
}
