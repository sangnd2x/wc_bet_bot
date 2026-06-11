package telegram

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"worldcup-bet-bot/internal/models"
)

// EnsureUserRegistered looks up the user in DB by telegram ID.
// If not found, inserts them. Returns the user.
func (b *Bot) EnsureUserRegistered(from *tgbotapi.User) (*models.User, error) {
	// Try to get existing user
	user, err := b.db.GetUserByTelegramID(int64(from.ID))
	if err != nil {
		return nil, err
	}

	if user != nil {
		return user, nil
	}

	// User doesn't exist, insert them
	username := from.UserName
	displayName := from.FirstName
	if from.LastName != "" {
		displayName = from.FirstName + " " + from.LastName
	}

	return b.db.UpsertUser(int64(from.ID), username, displayName)
}

// IsAdmin checks if the telegram user ID is the configured admin
func (b *Bot) IsAdmin(telegramID int64) bool {
	return telegramID == b.cfg.AdminUserID
}

// EnsureGroupRegistered upserts the group if the message comes from a group chat
func (b *Bot) EnsureGroupRegistered(chat *tgbotapi.Chat) {
	if chat.Type == "group" || chat.Type == "supergroup" {
		if err := b.db.UpsertGroup(chat.ID, chat.Title); err != nil {
			log.Printf("failed to register group %d: %v", chat.ID, err)
		}
	}
}
