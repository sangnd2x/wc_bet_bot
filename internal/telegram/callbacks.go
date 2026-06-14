package telegram

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"worldcup-bet-bot/internal/models"
	"worldcup-bet-bot/internal/sheets"
)

// handleBetCallback handles inline keyboard button presses for betting.
// Callback data format: "bet:<externalMatchID>:<side>"
func (b *Bot) handleBetCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	log.Printf("Handling callback: %s from user %d", cq.Data, cq.From.ID)

	// Guard: cq.Message can be nil for inline-mode callbacks (no chat context)
	if cq.Message == nil {
		b.answerCallback(cq.ID, "This action is only supported inside a group chat", false)
		return
	}

	// Route by prefix
	if strings.HasPrefix(cq.Data, "clearbet:") {
		b.handleClearBetCallback(cq)
		return
	}
	if strings.HasPrefix(cq.Data, "changebet_match:") {
		b.handleChangeBetMatchCallback(ctx, cq)
		return
	}
	if strings.HasPrefix(cq.Data, "changebet_apply:") {
		b.handleChangeBetApplyCallback(ctx, cq)
		return
	}
	if strings.HasPrefix(cq.Data, "changebet_guess_info:") {
		b.handleChangeBetGuessInfoCallback(cq)
		return
	}
	if strings.HasPrefix(cq.Data, "guess_apply:") {
		b.handleGuessApplyCallback(ctx, cq)
		return
	}

	// Parse callback data — if not "bet:" prefix, ignore
	if !strings.HasPrefix(cq.Data, "bet:") {
		b.answerCallback(cq.ID, "Unknown action", false)
		return
	}

	parts := strings.Split(cq.Data, ":")
	if len(parts) != 3 {
		b.answerCallback(cq.ID, "Invalid callback data", false)
		return
	}

	externalMatchID, err := strconv.Atoi(parts[1])
	if err != nil {
		b.answerCallback(cq.ID, "Invalid match ID", false)
		return
	}

	side := parts[2]

	// Look up match by externalMatchID in DB
	match, err := b.db.GetMatchByExternalID(externalMatchID)
	if err != nil {
		log.Printf("Failed to get match: %v", err)
		b.answerCallback(cq.ID, "Failed to fetch match", false)
		return
	}

	if match == nil {
		b.answerCallback(cq.ID, "Match not found", false)
		return
	}

	// EnsureUserRegistered for the pressing user
	user, err := b.EnsureUserRegistered(cq.From)
	if err != nil {
		log.Printf("Failed to register user: %v", err)
		b.answerCallback(cq.ID, "Failed to register user", false)
		return
	}

	// Get chatID from the callback query message
	chatID := cq.Message.Chat.ID

	// Load existing bets for this match in this group
	existingBets, err := b.db.GetBetsForMatchInGroup(match.ID, chatID)
	if err != nil {
		log.Printf("Failed to get existing bets: %v", err)
		b.answerCallback(cq.ID, "Failed to fetch bets", false)
		return
	}

	// Guard 1: Allow betting only on SCHEDULED or TIMED matches
	if match.Status != "SCHEDULED" && match.Status != "TIMED" {
		b.answerCallback(cq.ID, "Betting is closed for this match", false)
		return
	}

	// Guard 2: If this user already has a bet → answer "You already picked [team]! ✅"
	for _, bet := range existingBets {
		if bet.UserID == user.ID {
			pickedTeamName := match.AwayTeam
			if bet.PickedTeam == "HOME_TEAM" {
				pickedTeamName = match.HomeTeam
			}
			b.answerCallback(cq.ID, fmt.Sprintf("You already picked %s! ✅", pickedTeamName), false)
			return
		}
	}

	// Insert bet with db.InsertBet (including group chat ID)
	telegramMsgID := cq.Message.MessageID
	if err := b.db.InsertBet(match.ID, user.ID, side, telegramMsgID, chatID); err != nil {
		log.Printf("Failed to insert bet: %v", err)
		b.answerCallback(cq.ID, "Failed to save bet, try again", false)
		return
	}

	// Get team name for feedback
	teamName := match.AwayTeam
	if side == "HOME_TEAM" {
		teamName = match.HomeTeam
	}

	// Answer callback with "Locked in: [team name]! ✅"
	b.answerCallback(cq.ID, fmt.Sprintf("Locked in: %s! ✅", teamName), true)

	// After inserting, reload bets for this group
	updatedBets, err := b.db.GetBetsForMatchInGroup(match.ID, chatID)
	if err != nil {
		log.Printf("Failed to reload bets: %v", err)
		return
	}

	// Build updated message text
	msgText := FormatMatchMessage(match, b.loc)

	// Track which user picked which side
	var user1Bet, user2Bet *models.Bet
	for _, bet := range updatedBets {
		if len(updatedBets) == 1 {
			user1Bet = bet
		} else if user1Bet == nil {
			user1Bet = bet
		} else if user2Bet == nil {
			user2Bet = bet
		}
	}

	// If both users have now bet
	if len(updatedBets) >= 2 && user1Bet != nil && user2Bet != nil {
		// Get user display names
		user1, err := b.db.GetUserByID(user1Bet.UserID)
		user2, err2 := b.db.GetUserByID(user2Bet.UserID)

		if err == nil && user1 != nil && err2 == nil && user2 != nil {
			user1Name := user1.DisplayName
			if user1Name == "" {
				user1Name = user1.Username
			}
			if user1Name == "" {
				user1Name = "User"
			}

			user2Name := user2.DisplayName
			if user2Name == "" {
				user2Name = user2.Username
			}
			if user2Name == "" {
				user2Name = "User"
			}

			user1TeamName := match.AwayTeam
			if user1Bet.PickedTeam == "HOME_TEAM" {
				user1TeamName = match.HomeTeam
			}

			user2TeamName := match.AwayTeam
			if user2Bet.PickedTeam == "HOME_TEAM" {
				user2TeamName = match.HomeTeam
			}

			if user1Bet.PickedTeam == user2Bet.PickedTeam {
				// Score-guess mode: both picked same team
				msgText := FormatMatchMessage(match, b.loc)
				msgText += fmt.Sprintf("\n✅ %s → %s (waiting for guess...)\n✅ %s → %s (waiting for guess...)", user1Name, user1TeamName, user2Name, user2TeamName)

				// Edit original message to remove keyboard
				editMsg := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, msgText)
				editMsg.ParseMode = "HTML"
				b.api.Send(editMsg)

				// Prompt both users to guess the score
				prompt := fmt.Sprintf("Both picked %s!\nGuess the final score with /guess N-M\n• N = %s goals, M = %s goals\n• e.g. /guess 2-0 means %s 2-0 %s", user1TeamName, match.HomeTeam, match.AwayTeam, match.HomeTeam, match.AwayTeam)
				b.SendToChat(chatID, prompt)
			} else {
				// Normal mode: different teams picked
				msgText := FormatMatchMessage(match, b.loc)
				msgText += fmt.Sprintf("\n✅ %s → %s\n✅ %s → %s", user1Name, user1TeamName, user2Name, user2TeamName)

				// Call sheets.AppendBetRow
				betRow := sheets.BetRow{
					MatchDate:    match.MatchDate.Format("02/01/2006"),
					MatchID:      match.ExternalID,
					HomeTeam:     match.HomeTeam,
					AwayTeam:     match.AwayTeam,
					User1Name:    user1Name,
					User1Pick:    user1TeamName,
					User2Name:    user2Name,
					User2Pick:    user2TeamName,
					ActualWinner: "",
					User1Result:  "",
					User2Result:  "",
				}

				sheetsRow, err := b.sheetsClient.AppendBetRow(ctx, betRow)
				if err != nil {
					log.Printf("Failed to append bet row to sheets: %v", err)
				} else {
					if err := b.db.UpdateBetSheetsRow(match.ID, user1Bet.UserID, sheetsRow); err != nil {
						log.Printf("Failed to update sheets row for user1: %v", err)
					}
					if err := b.db.UpdateBetSheetsRow(match.ID, user2Bet.UserID, sheetsRow); err != nil {
						log.Printf("Failed to update sheets row for user2: %v", err)
					}
				}

				// Edit message: remove keyboard
				editMsg := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, msgText)
				editMsg.ParseMode = "HTML"
				b.api.Send(editMsg)

				// Send "Bet is on!" confirmation message to the group
				confirmMsg := fmt.Sprintf("🎰 Bet is on!\n%s vs %s\n%s → %s\n%s → %s", match.HomeTeam, match.AwayTeam, user1Name, user1TeamName, user2Name, user2TeamName)
				b.SendToChat(chatID, confirmMsg)
			}
		}
	} else if len(updatedBets) == 1 && user1Bet != nil {
		// If only one user has bet, edit the message to show who has bet and which side remains
		betUser, err := b.db.GetUserByID(user1Bet.UserID)
		if err == nil && betUser != nil {
			betUserName := betUser.DisplayName
			if betUserName == "" {
				betUserName = betUser.Username
			}
			if betUserName == "" {
				betUserName = "User"
			}

			pickedTeam := match.AwayTeam
			if user1Bet.PickedTeam == "HOME_TEAM" {
				pickedTeam = match.HomeTeam
			}

			remainingTeam := match.HomeTeam
			if user1Bet.PickedTeam == "HOME_TEAM" {
				remainingTeam = match.AwayTeam
			}

			msgText = FormatMatchMessage(match, b.loc)
			msgText += fmt.Sprintf("\n✅ %s → %s\n⏳ Waiting for partner to pick %s", betUserName, pickedTeam, remainingTeam)

			// Edit message: build keyboard with taken button showing username
			editMsg := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, msgText)
			editMsg.ParseMode = "HTML"

			// Build keyboard: taken button with ✅ label, remaining button with team name
			takenButtonLabel := fmt.Sprintf("✅ %s", betUserName)
			takenButtonData := fmt.Sprintf("bet:%d:%s", match.ExternalID, user1Bet.PickedTeam)
			remainingButtonLabel := remainingTeam
			remainingButtonData := fmt.Sprintf("bet:%d:%s", match.ExternalID, "AWAY_TEAM")
			if user1Bet.PickedTeam == "AWAY_TEAM" {
				remainingButtonData = fmt.Sprintf("bet:%d:HOME_TEAM", match.ExternalID)
			}

			kb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(takenButtonLabel, takenButtonData),
					tgbotapi.NewInlineKeyboardButtonData(remainingButtonLabel, remainingButtonData),
				),
			)
			editMsg.ReplyMarkup = &kb
			b.api.Send(editMsg)
		}
	}
}

// answerCallback sends an answer to a callback query
func (b *Bot) handleGuessApplyCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	// Format: guess_apply:<matchID>:<homeScore>:<awayScore>
	parts := strings.SplitN(cq.Data, ":", 4)
	if len(parts) != 4 {
		b.answerCallback(cq.ID, "Invalid data", false)
		return
	}
	matchID, err1 := strconv.ParseInt(parts[1], 10, 64)
	homeGuess, err2 := strconv.Atoi(parts[2])
	awayGuess, err3 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil || err3 != nil {
		b.answerCallback(cq.ID, "Invalid data", false)
		return
	}

	user, err := b.EnsureUserRegistered(cq.From)
	if err != nil {
		b.answerCallback(cq.ID, "Failed to register user", false)
		return
	}

	chatID := cq.Message.Chat.ID
	b.answerCallback(cq.ID, "Saving guess...", false)

	// Clear tracked keyboard and remove the markup
	b.clearPendingGuessKB(chatID, user.ID)

	ok := b.applyScoreGuess(chatID, matchID, user.ID, homeGuess, awayGuess)
	if !ok {
		return
	}

	// After saving, check if any score-guess bets still need a guess
	remaining, err := b.db.GetAllPendingScoreGuessBets(user.ID, chatID)
	if err != nil {
		log.Printf("handleGuessApplyCallback: failed to check remaining: %v", err)
		return
	}
	if len(remaining) > 0 {
		text := fmt.Sprintf("⏳ You still have %d match(es) waiting for a score guess:\n", len(remaining))
		for _, rem := range remaining {
			if m, err2 := b.db.GetMatchByID(rem.MatchID); err2 == nil && m != nil {
				text += fmt.Sprintf("• %s vs %s\n", m.HomeTeam, m.AwayTeam)
			}
		}
		text += "\nType /guess N-M for the next match."
		b.SendToChat(chatID, text)
	}
}

func (b *Bot) handleClearBetCallback(cq *tgbotapi.CallbackQuery) {
	parts := strings.SplitN(cq.Data, ":", 2)
	if len(parts) != 2 {
		b.answerCallback(cq.ID, "Invalid data", false)
		return
	}
	matchID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		b.answerCallback(cq.ID, "Invalid match ID", false)
		return
	}

	chatID := cq.Message.Chat.ID

	match, err := b.db.GetMatchByID(matchID)
	if err != nil || match == nil {
		b.answerCallback(cq.ID, "Match not found", false)
		return
	}

	if err := b.db.DeleteBetsForMatchInGroup(matchID, chatID); err != nil {
		log.Printf("handleClearBetCallback: failed to delete bets: %v", err)
		b.answerCallback(cq.ID, "Failed to clear bets", false)
		return
	}

	b.answerCallback(cq.ID, "Bets cleared ✅", true)

	// Edit the selection message to confirm
	edited := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID,
		fmt.Sprintf("Bets cleared for %s vs %s ✅", match.HomeTeam, match.AwayTeam))
	b.api.Send(edited)
}

func (b *Bot) answerCallback(callbackID string, text string, showAlert bool) {
	callback := tgbotapi.NewCallback(callbackID, text)
	callback.ShowAlert = showAlert

	if _, err := b.api.Request(callback); err != nil {
		log.Printf("Failed to answer callback: %v", err)
	}
}

// handleChangeBetMatchCallback shows a team picker for the selected match
// Callback: changebet_match:<matchID>
func (b *Bot) handleChangeBetMatchCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	parts := strings.SplitN(cq.Data, ":", 2)
	if len(parts) != 2 {
		b.answerCallback(cq.ID, "Invalid data", false)
		return
	}
	matchID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		b.answerCallback(cq.ID, "Invalid match ID", false)
		return
	}

	match, err := b.db.GetMatchByID(matchID)
	if err != nil || match == nil {
		b.answerCallback(cq.ID, "Match not found", false)
		return
	}

	b.answerCallback(cq.ID, "", false)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(match.HomeTeam, fmt.Sprintf("changebet_apply:%d:HOME_TEAM", matchID)),
			tgbotapi.NewInlineKeyboardButtonData(match.AwayTeam, fmt.Sprintf("changebet_apply:%d:AWAY_TEAM", matchID)),
		),
	)
	msg := tgbotapi.NewMessage(cq.Message.Chat.ID, fmt.Sprintf("Change pick for %s vs %s:", match.HomeTeam, match.AwayTeam))
	msg.ReplyMarkup = kb
	b.api.Send(msg)
}

// handleChangeBetApplyCallback applies the team change and resets score guesses
// Callback: changebet_apply:<matchID>:<HOME_TEAM|AWAY_TEAM>
func (b *Bot) handleChangeBetApplyCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	parts := strings.SplitN(cq.Data, ":", 3)
	if len(parts) != 3 {
		b.answerCallback(cq.ID, "Invalid data", false)
		return
	}
	matchID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		b.answerCallback(cq.ID, "Invalid match ID", false)
		return
	}
	newTeam := parts[2]
	if newTeam != "HOME_TEAM" && newTeam != "AWAY_TEAM" {
		b.answerCallback(cq.ID, "Invalid team", false)
		return
	}

	user, err := b.EnsureUserRegistered(cq.From)
	if err != nil {
		b.answerCallback(cq.ID, "Failed to register user", false)
		return
	}

	chatID := cq.Message.Chat.ID

	match, err := b.db.GetMatchByID(matchID)
	if err != nil || match == nil {
		b.answerCallback(cq.ID, "Match not found", false)
		return
	}

	// Update user's pick
	if err := b.db.UpdateBetPick(matchID, user.ID, chatID, newTeam); err != nil {
		log.Printf("handleChangeBetApplyCallback: failed to update pick: %v", err)
		b.answerCallback(cq.ID, "Failed to update pick", false)
		return
	}

	// Clear all score guesses for this match in this group (team changed, guesses no longer valid)
	if err := b.db.ClearGuessesForMatchInGroup(matchID, chatID); err != nil {
		log.Printf("handleChangeBetApplyCallback: failed to clear guesses: %v", err)
	}

	b.answerCallback(cq.ID, "Pick updated ✅", true)

	// Reload bets and determine new state
	updatedBets, err := b.db.GetBetsForMatchInGroup(matchID, chatID)
	if err != nil {
		log.Printf("handleChangeBetApplyCallback: failed to reload bets: %v", err)
		return
	}

	newTeamName := match.AwayTeam
	if newTeam == "HOME_TEAM" {
		newTeamName = match.HomeTeam
	}

	// Check if both bets now point to the same team (score-guess mode) or different
	if len(updatedBets) >= 2 {
		bet1 := updatedBets[0]
		bet2 := updatedBets[1]

		u1, _ := b.db.GetUserByID(bet1.UserID)
		u2, _ := b.db.GetUserByID(bet2.UserID)
		u1Name := "User"
		u2Name := "User"
		if u1 != nil {
			u1Name = u1.DisplayName
			if u1Name == "" {
				u1Name = u1.Username
			}
			if u1Name == "" {
				u1Name = "User"
			}
		}
		if u2 != nil {
			u2Name = u2.DisplayName
			if u2Name == "" {
				u2Name = u2.Username
			}
			if u2Name == "" {
				u2Name = "User"
			}
		}
		u1TeamName := match.AwayTeam
		if bet1.PickedTeam == "HOME_TEAM" {
			u1TeamName = match.HomeTeam
		}
		u2TeamName := match.AwayTeam
		if bet2.PickedTeam == "HOME_TEAM" {
			u2TeamName = match.HomeTeam
		}

		msgText := FormatMatchMessage(match, b.loc)
		if bet1.PickedTeam == bet2.PickedTeam {
			// Same team — score-guess mode
			msgText += fmt.Sprintf("\n✅ %s → %s (waiting for guess...)\n✅ %s → %s (waiting for guess...)", u1Name, u1TeamName, u2Name, u2TeamName)
			editMsg := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, msgText)
			editMsg.ParseMode = "HTML"
			b.api.Send(editMsg)
			prompt := fmt.Sprintf("Pick changed! Both now picked %s.\nGuess the final score with /guess N-M\n• N = %s goals, M = %s goals\n• e.g. /guess 2-0 means %s 2-0 %s",
				u1TeamName, match.HomeTeam, match.AwayTeam, match.HomeTeam, match.AwayTeam)
			b.SendToChat(chatID, prompt)
		} else {
			// Different teams — normal mode
			msgText += fmt.Sprintf("\n✅ %s → %s\n✅ %s → %s", u1Name, u1TeamName, u2Name, u2TeamName)
			editMsg := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, msgText)
			editMsg.ParseMode = "HTML"
			b.api.Send(editMsg)
			confirmMsg := fmt.Sprintf("🔄 Bet updated!\n%s vs %s\n%s → %s\n%s → %s",
				match.HomeTeam, match.AwayTeam, u1Name, u1TeamName, u2Name, u2TeamName)
			b.SendToChat(chatID, confirmMsg)
		}
	} else {
		// Only 1 bet remaining — show confirmation
		b.SendToChat(chatID, fmt.Sprintf("✅ Pick changed to %s for %s vs %s", newTeamName, match.HomeTeam, match.AwayTeam))
	}
}

// handleChangeBetGuessInfoCallback informs user to use /guess to change their score guess
// Callback: changebet_guess_info:<matchID>
func (b *Bot) handleChangeBetGuessInfoCallback(cq *tgbotapi.CallbackQuery) {
	parts := strings.SplitN(cq.Data, ":", 2)
	if len(parts) != 2 {
		b.answerCallback(cq.ID, "Invalid data", false)
		return
	}
	matchID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		b.answerCallback(cq.ID, "Invalid match ID", false)
		return
	}

	match, err := b.db.GetMatchByID(matchID)
	if err != nil || match == nil {
		b.answerCallback(cq.ID, "Match not found", false)
		return
	}

	b.answerCallback(cq.ID, "", false)
	msg := fmt.Sprintf("To update your score guess for %s vs %s, use:\n/guess N-M\n• N = %s goals, M = %s goals\n• e.g. /guess 2-0 means %s 2-0 %s",
		match.HomeTeam, match.AwayTeam, match.HomeTeam, match.AwayTeam, match.HomeTeam, match.AwayTeam)
	b.SendToChat(cq.Message.Chat.ID, msg)
}
