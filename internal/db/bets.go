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
	query := `SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id, guessed_home_score, guessed_away_score
	          FROM bets WHERE match_id = ? ORDER BY created_at ASC`

	rows, err := db.Query(query, matchID)
	if err != nil {
		return nil, fmt.Errorf("failed to query bets for match: %w", err)
	}
	defer rows.Close()

	return scanBets(rows)
}

func (db *DB) GetBetsForMatchInGroup(matchID, groupChatID int64) ([]*models.Bet, error) {
	query := `SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id, guessed_home_score, guessed_away_score
	          FROM bets WHERE match_id = ? AND group_chat_id = ? ORDER BY created_at ASC`

	rows, err := db.Query(query, matchID, groupChatID)
	if err != nil {
		return nil, fmt.Errorf("failed to query bets for match in group: %w", err)
	}
	defer rows.Close()

	return scanBets(rows)
}

func (db *DB) GetBetByMatchAndUser(matchID, userID int64) (*models.Bet, error) {
	query := `SELECT id, match_id, user_id, picked_team, outcome, telegram_message_id, sheets_row, created_at, resolved_at, group_chat_id, guessed_home_score, guessed_away_score
	          FROM bets WHERE match_id = ? AND user_id = ?`

	var bet models.Bet
	var outcome sql.NullString
	var sheetsRow sql.NullInt64
	var resolvedAt sql.NullTime
	var guessedHome sql.NullInt64
	var guessedAway sql.NullInt64

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
		&guessedHome,
		&guessedAway,
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
	if guessedHome.Valid {
		v := int(guessedHome.Int64)
		bet.GuessedHomeScore = &v
	}
	if guessedAway.Valid {
		v := int(guessedAway.Int64)
		bet.GuessedAwayScore = &v
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

func (db *DB) SetScoreGuess(matchID, userID, groupChatID int64, homeScore, awayScore int) (bool, error) {
	result, err := db.Exec(
		`UPDATE bets SET guessed_home_score = ?, guessed_away_score = ?
		 WHERE match_id = ? AND user_id = ? AND group_chat_id = ? AND resolved_at IS NULL`,
		homeScore, awayScore, matchID, userID, groupChatID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to set score guess: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func (db *DB) GetPendingScoreGuessBet(userID, groupChatID int64) (*models.Bet, error) {
	query := `
		SELECT id, match_id, user_id, picked_team, outcome,
		       telegram_message_id, sheets_row, created_at, resolved_at,
		       group_chat_id, guessed_home_score, guessed_away_score
		FROM bets
		WHERE user_id = ?
		  AND group_chat_id = ?
		  AND resolved_at IS NULL
		  AND guessed_home_score IS NULL
		  AND EXISTS (
		      SELECT 1 FROM bets b2
		      WHERE b2.match_id = bets.match_id
		        AND b2.group_chat_id = bets.group_chat_id
		        AND b2.user_id != bets.user_id
		        AND b2.picked_team = bets.picked_team
		  )
		LIMIT 1`

	var bet models.Bet
	var outcome sql.NullString
	var sheetsRow sql.NullInt64
	var resolvedAt sql.NullTime
	var guessedHome sql.NullInt64
	var guessedAway sql.NullInt64

	err := db.QueryRow(query, userID, groupChatID).Scan(
		&bet.ID, &bet.MatchID, &bet.UserID, &bet.PickedTeam, &outcome,
		&bet.TelegramMessageID, &sheetsRow, &bet.CreatedAt, &resolvedAt,
		&bet.GroupChatID, &guessedHome, &guessedAway,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending score guess bet: %w", err)
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
	if guessedHome.Valid {
		v := int(guessedHome.Int64)
		bet.GuessedHomeScore = &v
	}
	if guessedAway.Valid {
		v := int(guessedAway.Int64)
		bet.GuessedAwayScore = &v
	}
	return &bet, nil
}

func (db *DB) ResolveBets(matchID int64, winner string, homeScore, awayScore int) error {
	type betRow struct {
		id          int64
		groupChatID int64
		pickedTeam  string
		guessedHome sql.NullInt64
		guessedAway sql.NullInt64
	}

	rows, err := db.Query(
		`SELECT id, group_chat_id, picked_team, guessed_home_score, guessed_away_score
		 FROM bets WHERE match_id = ? AND resolved_at IS NULL`,
		matchID,
	)
	if err != nil {
		return fmt.Errorf("failed to query bets to resolve: %w", err)
	}

	var pending []betRow
	for rows.Next() {
		var b betRow
		if err := rows.Scan(&b.id, &b.groupChatID, &b.pickedTeam, &b.guessedHome, &b.guessedAway); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan bet: %w", err)
		}
		pending = append(pending, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating bets: %w", err)
	}

	// Group by (groupChatID, pickedTeam)
	type groupKey struct {
		groupChatID int64
		pickedTeam  string
	}
	buckets := make(map[groupKey][]betRow)
	var bucketOrder []groupKey
	for _, b := range pending {
		k := groupKey{b.groupChatID, b.pickedTeam}
		if _, exists := buckets[k]; !exists {
			bucketOrder = append(bucketOrder, k)
		}
		buckets[k] = append(buckets[k], b)
	}

	abs := func(x int) int {
		if x < 0 {
			return -x
		}
		return x
	}

	outcomes := make(map[int64]string) // bet.id → outcome

	for _, k := range bucketOrder {
		bets := buckets[k]
		if len(bets) == 2 {
			// Score-guess mode
			a, b2 := bets[0], bets[1]
			maxDist := int(^uint(0) >> 1) // MaxInt

			distA := maxDist
			if a.guessedHome.Valid && a.guessedAway.Valid {
				distA = abs(int(a.guessedHome.Int64)-homeScore) + abs(int(a.guessedAway.Int64)-awayScore)
			}
			distB := maxDist
			if b2.guessedHome.Valid && b2.guessedAway.Valid {
				distB = abs(int(b2.guessedHome.Int64)-homeScore) + abs(int(b2.guessedAway.Int64)-awayScore)
			}

			switch {
			case distA == maxDist && distB == maxDist:
				outcomes[a.id] = "DRAW"
				outcomes[b2.id] = "DRAW"
			case distA == maxDist:
				outcomes[a.id] = "LOSS"
				outcomes[b2.id] = "WIN"
			case distB == maxDist:
				outcomes[a.id] = "WIN"
				outcomes[b2.id] = "LOSS"
			case distA < distB:
				outcomes[a.id] = "WIN"
				outcomes[b2.id] = "LOSS"
			case distA > distB:
				outcomes[a.id] = "LOSS"
				outcomes[b2.id] = "WIN"
			default:
				outcomes[a.id] = "DRAW"
				outcomes[b2.id] = "DRAW"
			}
		} else {
			// Normal mode
			for _, b2 := range bets {
				if winner == "DRAW" {
					outcomes[b2.id] = "DRAW"
				} else if b2.pickedTeam == winner {
					outcomes[b2.id] = "WIN"
				} else {
					outcomes[b2.id] = "LOSS"
				}
			}
		}
	}

	for id, outcome := range outcomes {
		if _, err := db.Exec(
			`UPDATE bets SET outcome = ?, resolved_at = CURRENT_TIMESTAMP WHERE id = ?`,
			outcome, id,
		); err != nil {
			return fmt.Errorf("failed to update bet %d: %w", id, err)
		}
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
		var guessedHome sql.NullInt64
		var guessedAway sql.NullInt64

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
			&guessedHome,
			&guessedAway,
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
		if guessedHome.Valid {
			v := int(guessedHome.Int64)
			bet.GuessedHomeScore = &v
		}
		if guessedAway.Valid {
			v := int(guessedAway.Int64)
			bet.GuessedAwayScore = &v
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
	HomeTeam         string
	AwayTeam         string
	KickoffUTC       time.Time
	UserName         string
	PickedTeam       string // "HOME_TEAM" or "AWAY_TEAM"
	GuessedHomeScore *int
	GuessedAwayScore *int
	SameTeamMode     bool
}

// GetActiveBetsInGroup returns all unresolved bets in a group, joined with match info
func (db *DB) GetActiveBetsInGroup(groupChatID int64) ([]*ActiveBet, error) {
	query := `
		SELECT m.home_team, m.away_team, m.kickoff_utc,
		       COALESCE(NULLIF(u.display_name,''), u.username, 'User') as user_name,
		       b.picked_team,
		       b.guessed_home_score,
		       b.guessed_away_score,
		       (SELECT COUNT(*) FROM bets b2
		        WHERE b2.match_id = b.match_id
		          AND b2.group_chat_id = b.group_chat_id
		          AND b2.picked_team = b.picked_team) > 1 AS same_team_mode
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
		var guessedHome sql.NullInt64
		var guessedAway sql.NullInt64
		var sameTeamMode bool
		if err := rows.Scan(&ab.HomeTeam, &ab.AwayTeam, &ab.KickoffUTC, &ab.UserName, &ab.PickedTeam, &guessedHome, &guessedAway, &sameTeamMode); err != nil {
			return nil, fmt.Errorf("failed to scan active bet: %w", err)
		}
		if guessedHome.Valid {
			v := int(guessedHome.Int64)
			ab.GuessedHomeScore = &v
		}
		if guessedAway.Valid {
			v := int(guessedAway.Int64)
			ab.GuessedAwayScore = &v
		}
		ab.SameTeamMode = sameTeamMode
		bets = append(bets, &ab)
	}
	return bets, rows.Err()
}
