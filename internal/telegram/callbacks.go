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

	// Guard 1: If match status != SCHEDULED → answer "Betting is closed for this match."
	if match.Status != "SCHEDULED" {
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

	// Guard 3: If the OTHER user already picked the SAME side → answer "That pick is taken! Pick [other team] instead."
	if len(existingBets) > 0 {
		for _, bet := range existingBets {
			if bet.PickedTeam == side {
				otherTeam := match.AwayTeam
				if side == "AWAY_TEAM" {
					otherTeam = match.HomeTeam
				}
				b.answerCallback(cq.ID, fmt.Sprintf("That pick is taken! Pick %s instead.", otherTeam), false)
				return
			}
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
	msgText := FormatMatchMessage(match)

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

			msgText = FormatMatchMessage(match)
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
				// Update both bets' sheets_row
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

			msgText = FormatMatchMessage(match)
			msgText += fmt.Sprintf("\n✅ %s → %s\n⏳ Waiting for partner to pick %s", betUserName, pickedTeam, remainingTeam)

			// Edit message: keep keyboard
			editMsg := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, msgText)
			editMsg.ParseMode = "HTML"
			kb := MatchKeyboard(match)
			editMsg.ReplyMarkup = &kb
			b.api.Send(editMsg)
		}
	}
}

// answerCallback sends an answer to a callback query
func (b *Bot) answerCallback(callbackID string, text string, showAlert bool) {
	callback := tgbotapi.NewCallback(callbackID, text)
	callback.ShowAlert = showAlert

	if _, err := b.api.Request(callback); err != nil {
		log.Printf("Failed to answer callback: %v", err)
	}
}
