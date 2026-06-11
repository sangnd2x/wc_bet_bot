package telegram

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"worldcup-bet-bot/internal/models"
)

// MatchKeyboard builds the inline keyboard for betting on a match.
// Callback data format: "bet:<externalMatchID>:<side>"
// where side is "HOME_TEAM" or "AWAY_TEAM"
func MatchKeyboard(m *models.Match) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				m.HomeTeam,
				fmt.Sprintf("bet:%d:HOME_TEAM", m.ExternalID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				m.AwayTeam,
				fmt.Sprintf("bet:%d:AWAY_TEAM", m.ExternalID),
			),
		),
	)
}

// FormatMatchMessage formats a match for display in the given timezone.
// Example: "🏆 Mexico vs USA\n📅 Thu, 11 Jun 2026 02:00 ICT"
func FormatMatchMessage(m *models.Match, loc *time.Location) string {
	kickoff := m.KickoffUTC.In(loc)
	dateStr := kickoff.Format("Mon, 2 Jan 2006 15:04 MST")
	return fmt.Sprintf("🏆 %s vs %s\n📅 %s", m.HomeTeam, m.AwayTeam, dateStr)
}
