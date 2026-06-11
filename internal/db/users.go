package db

import (
	"database/sql"
	"fmt"

	"worldcup-bet-bot/internal/models"
)

func (db *DB) UpsertUser(telegramID int64, username, displayName string) (*models.User, error) {
	query := `INSERT OR REPLACE INTO users (telegram_id, username, display_name, registered_at)
	          VALUES (?, ?, ?, CURRENT_TIMESTAMP)`

	_, err := db.Exec(query, telegramID, username, displayName)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert user: %w", err)
	}

	return db.GetUserByTelegramID(telegramID)
}

func (db *DB) GetUserByTelegramID(telegramID int64) (*models.User, error) {
	query := `SELECT id, telegram_id, username, display_name, registered_at FROM users WHERE telegram_id = ?`

	var user models.User
	err := db.QueryRow(query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.DisplayName,
		&user.RegisteredAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (db *DB) GetUserByID(userID int64) (*models.User, error) {
	query := `SELECT id, telegram_id, username, display_name, registered_at FROM users WHERE id = ?`

	var user models.User
	err := db.QueryRow(query, userID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.DisplayName,
		&user.RegisteredAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (db *DB) GetAllUsers() ([]*models.User, error) {
	query := `SELECT id, telegram_id, username, display_name, registered_at FROM users ORDER BY registered_at ASC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.TelegramID,
			&user.Username,
			&user.DisplayName,
			&user.RegisteredAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}
