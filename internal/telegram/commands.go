package telegram

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"worldcup-bet-bot/internal/db"
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
/upcoming - Show next 3 upcoming matches
/matches &lt;DDMMYYYY&gt; - Show matches for a specific date
/result - Show current leaderboard
/bets - Show active bets in this group
/guess N-M - Guess score when both players pick same team
/clearbet - Clear bets for a specific match
/start - Show this help message

How to play:
- Use the inline buttons to place your bet
- Both players must pick opposite teams
- Results are automatically updated when matches finish
`
	reply := tgbotapi.NewMessage(msg.Chat.ID, startMsg)
	reply.ParseMode = "HTML"

	if _, err := b.api.Send(reply); err != nil {
		log.Printf("failed to send /start reply to chat %d: %v", msg.Chat.ID, err)
	}
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
		if err := b.SendMatchToChatForGroup(msg.Chat.ID, match); err != nil {
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

// cmdClearBet handles /clearbet — shows inline keyboard to delete bets for a specific match
func (b *Bot) cmdClearBet(ctx context.Context, msg *tgbotapi.Message) {
	matches, err := b.db.GetActiveBetMatchesInGroup(msg.Chat.ID)
	if err != nil {
		log.Printf("cmdClearBet: %v", err)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch bets."))
		return
	}
	if len(matches) == 0 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "No active bets to clear."))
		return
	}

	var kbRows [][]tgbotapi.InlineKeyboardButton
	for _, m := range matches {
		label := fmt.Sprintf("❌ %s vs %s", m.HomeTeam, m.AwayTeam)
		data := fmt.Sprintf("clearbet:%d", m.MatchID)
		kbRows = append(kbRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, data),
		))
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, "Select match to clear bets:")
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(kbRows...)
	b.api.Send(reply)
}

// cmdGuess handles /guess N-M command for score-guess betting mode
func (b *Bot) cmdGuess(ctx context.Context, msg *tgbotapi.Message, args string) {
	args = strings.TrimSpace(args)
	parts := strings.SplitN(args, "-", 2)
	if len(parts) != 2 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Invalid format. Use /guess N-M (e.g. /guess 2-0)"))
		return
	}
	homeGuess, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || homeGuess < 0 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Invalid format. Use /guess N-M (e.g. /guess 2-0)"))
		return
	}
	awayGuess, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || awayGuess < 0 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Invalid format. Use /guess N-M (e.g. /guess 2-0)"))
		return
	}

	user, err := b.EnsureUserRegistered(msg.From)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to register user."))
		return
	}

	bet, err := b.db.GetPendingScoreGuessBet(user.ID, msg.Chat.ID)
	if err != nil {
		log.Printf("GetPendingScoreGuessBet error: %v", err)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch bet."))
		return
	}
	if bet == nil {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "No pending score guess for you."))
		return
	}
	if bet.GuessedHomeScore != nil {
		alreadyText := fmt.Sprintf("Already guessed %d-%d", *bet.GuessedHomeScore, *bet.GuessedAwayScore)
		if m, err2 := b.db.GetMatchByID(bet.MatchID); err2 == nil && m != nil {
			alreadyText = fmt.Sprintf("Already guessed %s %d-%d %s", m.HomeTeam, *bet.GuessedHomeScore, *bet.GuessedAwayScore, m.AwayTeam)
		}
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, alreadyText))
		return
	}

	ok, err := b.db.SetScoreGuess(bet.MatchID, user.ID, msg.Chat.ID, homeGuess, awayGuess)
	if err != nil || !ok {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to save guess, try again."))
		return
	}

	match, err := b.db.GetMatchByID(bet.MatchID)
	confirmText := fmt.Sprintf("Got it! Your guess: %d-%d ✅", homeGuess, awayGuess)
	if err == nil && match != nil {
		confirmText = fmt.Sprintf("Got it! Your guess: %s %d-%d %s ✅", match.HomeTeam, homeGuess, awayGuess, match.AwayTeam)
	}
	b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, confirmText))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// cmdBets handles /bets command
func (b *Bot) cmdBets(ctx context.Context, msg *tgbotapi.Message) {
	log.Println("Handling /bets command")

	bets, err := b.db.GetActiveBetsInGroup(msg.Chat.ID)
	if err != nil {
		log.Printf("Failed to get active bets: %v", err)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch bets."))
		return
	}

	if len(bets) == 0 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "No active bets."))
		return
	}

	// Group bets by match (home_team + away_team + kickoff)
	type matchKey struct {
		home    string
		away    string
		kickoff time.Time
	}
	var order []matchKey
	grouped := make(map[matchKey][]*db.ActiveBet)
	for _, bet := range bets {
		k := matchKey{bet.HomeTeam, bet.AwayTeam, bet.KickoffUTC}
		if _, exists := grouped[k]; !exists {
			order = append(order, k)
		}
		grouped[k] = append(grouped[k], bet)
	}

	text := "📋 <b>Active Bets</b>\n─────────────\n"
	for _, k := range order {
		matchBets := grouped[k]
		kickoff := k.kickoff.In(b.loc)
		text += fmt.Sprintf("\n🏆 <b>%s vs %s</b>\n📅 %s\n", k.home, k.away, kickoff.Format("Mon, 2 Jan 15:04 MST"))
		for _, ab := range matchBets {
			teamName := ab.AwayTeam
			if ab.PickedTeam == "HOME_TEAM" {
				teamName = ab.HomeTeam
			}
			if ab.SameTeamMode {
				if ab.GuessedHomeScore != nil {
					text += fmt.Sprintf("  • %s → %s (guess: %d-%d)\n", ab.UserName, teamName, *ab.GuessedHomeScore, *ab.GuessedAwayScore)
				} else {
					text += fmt.Sprintf("  • %s → %s (waiting for guess...)\n", ab.UserName, teamName)
				}
			} else {
				text += fmt.Sprintf("  • %s → %s\n", ab.UserName, teamName)
			}
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	reply.ParseMode = "HTML"
	if _, err := b.api.Send(reply); err != nil {
		log.Printf("failed to send /bets reply: %v", err)
	}
}
