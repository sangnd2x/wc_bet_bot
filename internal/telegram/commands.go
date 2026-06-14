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
/changebet - Change your team pick or score guess
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

// cmdChangeBet handles /changebet — shows inline keyboard to change team pick or score guess
func (b *Bot) cmdChangeBet(ctx context.Context, msg *tgbotapi.Message) {
	user, err := b.EnsureUserRegistered(msg.From)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to register user."))
		return
	}

	myBets, err := b.db.GetMyBetsInGroup(user.ID, msg.Chat.ID)
	if err != nil {
		log.Printf("cmdChangeBet: %v", err)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch your bets."))
		return
	}
	if len(myBets) == 0 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "No active bets to change."))
		return
	}

	var kbRows [][]tgbotapi.InlineKeyboardButton
	for _, mb := range myBets {
		pickedTeamName := mb.AwayTeam
		if mb.PickedTeam == "HOME_TEAM" {
			pickedTeamName = mb.HomeTeam
		}
		var label string
		if mb.SameTeamMode {
			// Score-guess mode — show guess info button
			if mb.GuessedHome != nil {
				label = fmt.Sprintf("🎯 %s vs %s → %s (guess: %d-%d)", mb.HomeTeam, mb.AwayTeam, pickedTeamName, *mb.GuessedHome, *mb.GuessedAway)
			} else {
				label = fmt.Sprintf("🎯 %s vs %s → %s (no guess yet)", mb.HomeTeam, mb.AwayTeam, pickedTeamName)
			}
			data := fmt.Sprintf("changebet_guess_info:%d", mb.MatchID)
			kbRows = append(kbRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, data),
			))
		} else {
			// Normal mode — allow team change
			label = fmt.Sprintf("🔄 %s vs %s → %s", mb.HomeTeam, mb.AwayTeam, pickedTeamName)
			data := fmt.Sprintf("changebet_match:%d", mb.MatchID)
			kbRows = append(kbRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, data),
			))
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, "Select bet to change:")
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(kbRows...)
	b.api.Send(reply)
}

// cmdGuess handles /guess N-M command for score-guess betting mode.
// If multiple matches need a guess, shows a keyboard so the user picks which match the score applies to.
// /guess (no args) lists all pending matches.
func (b *Bot) cmdGuess(ctx context.Context, msg *tgbotapi.Message, args string) {
	user, err := b.EnsureUserRegistered(msg.From)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to register user."))
		return
	}

	pending, err := b.db.GetAllPendingScoreGuessBets(user.ID, msg.Chat.ID)
	if err != nil {
		log.Printf("GetAllPendingScoreGuessBets error: %v", err)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch bets."))
		return
	}

	args = strings.TrimSpace(args)

	// No args — list pending matches
	if args == "" {
		if len(pending) == 0 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "No pending score guesses for you."))
			return
		}
		text := "⏳ Pending score guesses:\n"
		for _, bet := range pending {
			if m, err2 := b.db.GetMatchByID(bet.MatchID); err2 == nil && m != nil {
				text += fmt.Sprintf("• %s vs %s — type /guess N-M (N=%s goals, M=%s goals)\n",
					m.HomeTeam, m.AwayTeam, m.HomeTeam, m.AwayTeam)
			}
		}
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, text))
		return
	}

	// Parse score
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

	// When score args are provided, also include already-guessed bets (allow overwrite)
	allScoreBets, err := b.db.GetAllMyScoreGuessBets(user.ID, msg.Chat.ID)
	if err != nil {
		log.Printf("GetAllMyScoreGuessBets error: %v", err)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Failed to fetch bets."))
		return
	}

	if len(allScoreBets) == 0 {
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "No score-guess bets for you."))
		return
	}

	// Multiple score bets — ask user to pick which match this score applies to
	if len(allScoreBets) > 1 {
		// Clear any previous pending-guess keyboard for this user
		b.clearPendingGuessKB(msg.Chat.ID, user.ID)

		var kbRows [][]tgbotapi.InlineKeyboardButton
		for _, bet := range allScoreBets {
			m, err2 := b.db.GetMatchByID(bet.MatchID)
			if err2 != nil || m == nil {
				continue
			}
			label := fmt.Sprintf("⚽ %s vs %s (%d-%d)", m.HomeTeam, m.AwayTeam, homeGuess, awayGuess)
			data := fmt.Sprintf("guess_apply:%d:%d:%d", bet.MatchID, homeGuess, awayGuess)
			kbRows = append(kbRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, data),
			))
		}
		reply := tgbotapi.NewMessage(msg.Chat.ID,
			fmt.Sprintf("Score %d-%d applies to which match?", homeGuess, awayGuess))
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(kbRows...)
		sent, err := b.api.Send(reply)
		if err == nil {
			b.storePendingGuessKB(msg.Chat.ID, user.ID, sent.MessageID)
		}
		return
	}

	// Exactly 1 — apply directly
	b.applyScoreGuess(msg.Chat.ID, allScoreBets[0].MatchID, user.ID, homeGuess, awayGuess)
}

// applyScoreGuess validates the guessed score and saves it.
// The guess must favor the picked team (picked team must win the match).
func (b *Bot) applyScoreGuess(chatID, matchID, userID int64, homeGuess, awayGuess int) {
	// Get match info
	m, err := b.db.GetMatchByID(matchID)
	if err != nil || m == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "Failed to fetch match, try again."))
		return
	}

	// Find this user's bet for this match in this group
	bets, err := b.db.GetBetsForMatchInGroup(matchID, chatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "Failed to fetch bet, try again."))
		return
	}
	var pickedTeam string
	for _, bet := range bets {
		if bet.UserID == userID {
			pickedTeam = bet.PickedTeam
			break
		}
	}
	if pickedTeam == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "No active score-guess bet found for you in this match."))
		return
	}

	// Validate: guess must be in favor of the picked team (picked team must win)
	pickedTeamName := m.AwayTeam
	if pickedTeam == "HOME_TEAM" {
		pickedTeamName = m.HomeTeam
	}
	validGuess := false
	if pickedTeam == "HOME_TEAM" && homeGuess > awayGuess {
		validGuess = true
	} else if pickedTeam == "AWAY_TEAM" && awayGuess > homeGuess {
		validGuess = true
	}
	if !validGuess {
		var exampleGuess string
		if pickedTeam == "HOME_TEAM" {
			exampleGuess = fmt.Sprintf("/guess 2-0 means %s 2-0 %s (%s wins)", m.HomeTeam, m.AwayTeam, m.HomeTeam)
		} else {
			exampleGuess = fmt.Sprintf("/guess 0-2 means %s 0-2 %s (%s wins)", m.HomeTeam, m.AwayTeam, m.AwayTeam)
		}
		errMsg := fmt.Sprintf(
			"❌ Guess must favor %s (the team you picked).\n\nFormat: /guess N-M where N = %s goals, M = %s goals\nExample: %s\n\nTo change your pick: /changebet",
			pickedTeamName, m.HomeTeam, m.AwayTeam, exampleGuess,
		)
		b.api.Send(tgbotapi.NewMessage(chatID, errMsg))
		return
	}

	// Save the guess
	ok, err := b.db.SetScoreGuess(matchID, userID, chatID, homeGuess, awayGuess)
	if err != nil || !ok {
		b.api.Send(tgbotapi.NewMessage(chatID, "Failed to save guess, try again."))
		return
	}
	confirmText := fmt.Sprintf("Got it! Your guess for %s vs %s: %s %d-%d %s ✅",
		m.HomeTeam, m.AwayTeam, m.HomeTeam, homeGuess, awayGuess, m.AwayTeam)
	b.api.Send(tgbotapi.NewMessage(chatID, confirmText))
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
