package db

import (
	"fmt"

	"worldcup-bet-bot/internal/models"
)

// UpsertGroup inserts or updates a group chat record
func (db *DB) UpsertGroup(chatID int64, title string) error {
	query := `INSERT INTO groups (chat_id, title, created_at)
	          VALUES (?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(chat_id) DO UPDATE SET title = excluded.title`

	_, err := db.Exec(query, chatID, title)
	if err != nil {
		return fmt.Errorf("failed to upsert group: %w", err)
	}

	return nil
}

// GetAllGroups returns all known group chats
func (db *DB) GetAllGroups() ([]*models.Group, error) {
	query := `SELECT chat_id, title, created_at FROM groups ORDER BY chat_id ASC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*models.Group

	for rows.Next() {
		var group models.Group
		err := rows.Scan(&group.ChatID, &group.Title, &group.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}

		groups = append(groups, &group)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating groups: %w", err)
	}

	return groups, nil
}

// GetGroup returns a specific group by chat_id
func (db *DB) GetGroup(chatID int64) (*models.Group, error) {
	query := `SELECT chat_id, title, created_at FROM groups WHERE chat_id = ?`

	var group models.Group
	err := db.QueryRow(query, chatID).Scan(&group.ChatID, &group.Title, &group.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}
