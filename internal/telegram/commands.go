package telegram

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// cmdStart handles /start command
func (b *Bot) cmdStart(ctx context.Context, msg *tgbotapi.Message) {
	log.Printf("Handling /start command from user %d in chat %d", msg.From.ID, msg.Chat.ID)
	if _, err := b.EnsureUserRegistered(msg.From); err != nil {
		log.Printf("failed to register user on /start: %v", err)
	}
	startMsg := `
Welcome to World Cup 2026 Betting Bot! 🏆

Available commands:
/upcoming_match - Show next 3 upcoming matches
/matches <DDMMYYYY> - Show matches for a specific date
/result - Show current leaderboard
/set_result <match_id> <team> - Set match result (admin only)
/start - Show this help message

How to play:
- Use the inline buttons to place your bet
- Both players must pick opposite teams
- Results are automatically updated when matches finish
`
	reply := tgbotapi.NewMessage(msg.Chat.ID, startMsg)
	reply.ParseMode = "HTML"

	b.api.Send(reply)
}

// cmdUpcomingMatch handles /upcoming_match command
func (b *Bot) cmdUpcomingMatch(ctx context.Context, msg *tgbotapi.Message) {
	log.Println("Handling /upcoming_match command")

	// Query DB for next 3 SCHEDULED matches
	matches, err := b.db.GetUpcomingMatches(3)
	if err != nil {
		log.Printf("Failed to get upcoming matches from DB: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch matches. Please try again.")
		b.api.Send(reply)
		return
	}

	// If DB empty, call fbClient.GetUpcomingMatches(ctx, 7) and upsert to DB
	if len(matches) == 0 {
		log.Println("No matches in DB, fetching from API...")
		apiMatches, err := b.fbClient.GetUpcomingMatches(ctx, 7)
		if err != nil {
			log.Printf("Failed to fetch from API: %v", err)
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch matches from API. Please try again.")
			b.api.Send(reply)
			return
		}

		// Upsert to DB
		for i := range apiMatches {
			apiMatches[i].MatchDate = apiMatches[i].KickoffUTC.Truncate(24 * time.Hour)
			if err := b.db.UpsertMatch(&apiMatches[i]); err != nil {
				log.Printf("Failed to upsert match: %v", err)
			}
		}

		limit := min(3, len(apiMatches))
		for i := 0; i < limit; i++ {
			matches = append(matches, &apiMatches[i])
		}
	}

	// Send each match with keyboard
	if len(matches) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "No upcoming matches found.")
		b.api.Send(reply)
		return
	}

	for _, match := range matches {
		if err := b.SendMatchToChat(msg.Chat.ID, match); err != nil {
			log.Printf("Failed to send match: %v", err)
		}
	}
}

// cmdMatches handles /matches <DDMMYYYY> command
func (b *Bot) cmdMatches(ctx context.Context, msg *tgbotapi.Message, args string) {
	log.Printf("Handling /matches command with args: %s", args)

	args = strings.TrimSpace(args)
	if args == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Usage: /matches DDMMYYYY (e.g., /matches 11062026)")
		b.api.Send(reply)
		return
	}

	// Parse the date argument DDMMYYYY
	if len(args) != 8 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Invalid date format. Use DDMMYYYY (e.g., 11062026)")
		b.api.Send(reply)
		return
	}

	day, err := strconv.Atoi(args[0:2])
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Invalid date format.")
		b.api.Send(reply)
		return
	}

	month, err := strconv.Atoi(args[2:4])
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Invalid date format.")
		b.api.Send(reply)
		return
	}

	year, err := strconv.Atoi(args[4:8])
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Invalid date format.")
		b.api.Send(reply)
		return
	}

	matchDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	// Query DB for that date
	matches, err := b.db.GetMatchesByDate(matchDate)
	if err != nil {
		log.Printf("Failed to query matches: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch matches. Please try again.")
		b.api.Send(reply)
		return
	}

	// If empty, fetch from football API and upsert
	if len(matches) == 0 {
		log.Printf("No matches in DB for %s, fetching from API...", matchDate.Format("2006-01-02"))
		apiMatches, err := b.fbClient.GetMatchesByDate(ctx, matchDate)
		if err != nil {
			log.Printf("Failed to fetch from API: %v", err)
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch matches from API. Please try again.")
			b.api.Send(reply)
			return
		}

		// Upsert to DB
		for i := range apiMatches {
			apiMatches[i].MatchDate = apiMatches[i].KickoffUTC.Truncate(24 * time.Hour)
			if err := b.db.UpsertMatch(&apiMatches[i]); err != nil {
				log.Printf("Failed to upsert match: %v", err)
			}
		}

		for i := range apiMatches {
			matches = append(matches, &apiMatches[i])
		}
	}

	// Send matches
	if len(matches) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("No matches found for %s.", matchDate.Format("02/01/2006")))
		b.api.Send(reply)
		return
	}

	now := time.Now().UTC()
	for _, match := range matches {
		// Send with keyboard if kickoff is in the future, text-only if past
		text := FormatMatchMessage(match, b.loc)
		reply := tgbotapi.NewMessage(msg.Chat.ID, text)
		reply.ParseMode = "HTML"

		if match.KickoffUTC.After(now) {
			reply.ReplyMarkup = MatchKeyboard(match)
		}

		b.api.Send(reply)
	}
}

// cmdLeaderboard handles /result and /leaderboard commands
func (b *Bot) cmdLeaderboard(ctx context.Context, msg *tgbotapi.Message) {
	log.Println("Handling leaderboard command")

	chatID := msg.Chat.ID

	// Get all users
	users, err := b.db.GetAllUsers()
	if err != nil {
		log.Printf("Failed to get users: %v", err)
		reply := tgbotapi.NewMessage(chatID, "Failed to fetch leaderboard. Please try again.")
		b.api.Send(reply)
		return
	}

	if len(users) == 0 {
		reply := tgbotapi.NewMessage(chatID, "No users registered yet.")
		b.api.Send(reply)
		return
	}

	// Get records for each user in this group and format response
	result := "🏆 Leaderboard\n─────────────\n"
	hasAnyBets := false

	for _, user := range users {
		record, err := b.db.GetUserRecordInGroup(user.ID, chatID)
		if err != nil {
			log.Printf("Failed to get record for user %d in group %d: %v", user.ID, chatID, err)
			continue
		}

		// Skip users with no bets in this group
		if record.Total == 0 {
			continue
		}

		hasAnyBets = true
		displayName := user.DisplayName
		if displayName == "" {
			displayName = user.Username
		}
		if displayName == "" {
			displayName = fmt.Sprintf("User%d", user.ID)
		}

		result += fmt.Sprintf("%s: %dW / %dL / %dD\n", displayName, record.Wins, record.Losses, record.Draws)
	}

	if !hasAnyBets {
		reply := tgbotapi.NewMessage(chatID, "No bets recorded in this group yet.")
		b.api.Send(reply)
		return
	}

	reply := tgbotapi.NewMessage(chatID, result)
	reply.ParseMode = "HTML"

	b.api.Send(reply)
}

// cmdSetResult handles /set_result <match_id> <team> command (admin only)
func (b *Bot) cmdSetResult(ctx context.Context, msg *tgbotapi.Message, args string) {
	log.Printf("Handling /set_result command with args: %s", args)

	// Check admin
	if !b.IsAdmin(int64(msg.From.ID)) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Admin only.")
		b.api.Send(reply)
		return
	}

	parts := strings.Fields(args)
	if len(parts) < 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Usage: /set_result <match_id> <team>\nTeam: home, away, draw")
		b.api.Send(reply)
		return
	}

	// Parse match_id
	externalID, err := strconv.Atoi(parts[0])
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Invalid match_id.")
		b.api.Send(reply)
		return
	}

	// Parse team
	teamStr := strings.ToLower(parts[1])
	var winner string
	switch teamStr {
	case "home":
		winner = "HOME_TEAM"
	case "away":
		winner = "AWAY_TEAM"
	case "draw":
		winner = "DRAW"
	default:
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Invalid team. Use: home, away, draw")
		b.api.Send(reply)
		return
	}

	// Look up match by external_id
	match, err := b.db.GetMatchByExternalID(externalID)
	if err != nil {
		log.Printf("Failed to get match: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch match from DB.")
		b.api.Send(reply)
		return
	}

	if match == nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Match not found.")
		b.api.Send(reply)
		return
	}

	// Update match result and resolve bets
	homeScore := 0
	awayScore := 0
	if winner == "HOME_TEAM" {
		homeScore = 1
	} else if winner == "AWAY_TEAM" {
		awayScore = 1
	}

	if err := b.db.UpdateMatchResult(externalID, winner, homeScore, awayScore); err != nil {
		log.Printf("Failed to update match result: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to update match result.")
		b.api.Send(reply)
		return
	}

	if err := b.db.ResolveBets(match.ID, winner); err != nil {
		log.Printf("Failed to resolve bets: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Failed to resolve bets.")
		b.api.Send(reply)
		return
	}

	// Update Google Sheets for resolved bets
	bets, err := b.db.GetBetsForMatch(match.ID)
	if err != nil {
		log.Printf("Failed to get bets for match: %v", err)
	}

	if len(bets) >= 2 {
		bet1 := bets[0]
		bet2 := bets[1]
		if bet1.SheetsRow > 0 {
			if err := b.sheetsClient.UpdateBetResult(ctx, bet1.SheetsRow, winner, bet1.Outcome, bet2.Outcome); err != nil {
				log.Printf("Failed to update sheets for bet row %d: %v", bet1.SheetsRow, err)
			}
		}
	}

	// Send confirmation to group
	msg2 := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Result set for %s vs %s: Winner = %s", match.HomeTeam, match.AwayTeam, winner))
	b.api.Send(msg2)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
