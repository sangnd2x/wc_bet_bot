package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"worldcup-bet-bot/internal/config"
	"worldcup-bet-bot/internal/db"
	"worldcup-bet-bot/internal/football"
	"worldcup-bet-bot/internal/models"
	"worldcup-bet-bot/internal/sheets"
	"worldcup-bet-bot/internal/telegram"
)

func main() {
	// 1. Load config (reads .env if present, then env vars)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// 2. Open SQLite DB and run migrations
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db error: %v", err)
	}
	defer database.Close()

	// 3. Pre-seed users from config if IDs are set
	ctx := context.Background()
	if cfg.User1TelegramID != 0 {
		if _, err := database.UpsertUser(cfg.User1TelegramID, "", "User1"); err != nil {
			log.Printf("warning: could not seed user1: %v", err)
		}
	}
	if cfg.User2TelegramID != 0 {
		if _, err := database.UpsertUser(cfg.User2TelegramID, "", "User2"); err != nil {
			log.Printf("warning: could not seed user2: %v", err)
		}
	}

	// 4. Init Google Sheets client
	credJSON, err := base64.StdEncoding.DecodeString(cfg.SheetsCredsB64)
	if err != nil {
		log.Fatalf("failed to decode GOOGLE_CREDENTIALS_B64: %v", err)
	}
	sheetsClient, err := sheets.NewClient(ctx, credJSON, cfg.SheetsSpreadID, cfg.SheetsTabName)
	if err != nil {
		log.Fatalf("sheets error: %v", err)
	}

	// 5. Init football API client
	fbClient := football.NewClient(cfg.FootballAPIKey, cfg.CompetitionCode, cfg.Season)

	// 6. Init Telegram bot
	bot, err := telegram.NewBot(cfg, database, sheetsClient, fbClient)
	if err != nil {
		log.Fatalf("bot error: %v", err)
	}

	// 7. Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 8. Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		cancel()
	}()

	// 9. Create poller with callback that will be called when matches are resolved
	onMatchResolvedFn := func(match *models.Match) {
		log.Printf("Match resolved: %s vs %s (status: %s)", match.HomeTeam, match.AwayTeam, match.Status)

		// Handle match resolution based on status
		switch match.Status {
		case "FINISHED":
			// Resolve bets for this match
			if err := database.ResolveBets(match.ID, match.Winner, match.HomeScore, match.AwayScore); err != nil {
				log.Printf("error resolving bets for match %d: %v", match.ID, err)
			} else {
				log.Printf("resolved bets for match %d", match.ID)
			}
		case "CANCELLED":
			// Delete all bets for cancelled matches
			if err := database.DeleteBetsForMatch(match.ID); err != nil {
				log.Printf("error deleting bets for cancelled match %d: %v", match.ID, err)
			} else {
				log.Printf("deleted bets for cancelled match %d", match.ID)
			}
		case "POSTPONED":
			// Log postponement, bets remain in DB
			log.Printf("match %d has been postponed", match.ID)
		}

		// Notify all registered groups about match resolution
		baseMsg := formatMatchResolutionMessage(match)
		if baseMsg != "" {
			groups, err := database.GetAllGroups()
			if err != nil {
				log.Printf("error fetching groups for match resolution notification: %v", err)
				return
			}
			if len(groups) == 0 && cfg.GroupChatID != 0 {
				groups = append(groups, &models.Group{ChatID: cfg.GroupChatID})
			}
			for _, group := range groups {
				msg := baseMsg
				if match.Status == "FINISHED" {
					msg += buildBetOutcomeLines(database, match, group.ChatID)
				}
				if err := bot.SendToChat(group.ChatID, msg); err != nil {
					log.Printf("error notifying group %d about match resolution: %v", group.ChatID, err)
				}
			}
		}
	}

	poller := football.NewPoller(fbClient, database, onMatchResolvedFn)
	bot.SetPoller(poller)

	// 10. Run initial match sync in background
	go func() {
		if err := poller.SyncMatches(ctx); err != nil {
			log.Printf("initial sync error: %v", err)
		} else {
			log.Println("initial match sync complete")
		}
	}()

	// 11. Start scheduler (daily broadcast + polling + sync cron jobs)
	scheduler, err := telegram.NewScheduler(bot, poller, cfg)
	if err != nil {
		log.Fatalf("scheduler error: %v", err)
	}
	if err := scheduler.Start(); err != nil {
		log.Fatalf("scheduler start error: %v", err)
	}
	defer scheduler.Stop()

	// 12. Start bot (blocks until ctx cancelled)
	log.Println("bot started, listening for messages...")
	bot.Start(ctx)
	log.Println("bot stopped")
}

// formatMatchResolutionMessage formats a message for match resolution
func formatMatchResolutionMessage(match *models.Match) string {
	switch match.Status {
	case "FINISHED":
		return formatFinishedMatch(match)
	case "CANCELLED":
		return "<b>Match Cancelled</b>\n" +
			formatTeams(match) +
			"\nThis match has been cancelled. All bets have been deleted."
	case "POSTPONED":
		return "<b>Match Postponed</b>\n" +
			formatTeams(match) +
			"\nThis match has been postponed. Bets remain active."
	default:
		return ""
	}
}

func formatFinishedMatch(match *models.Match) string {
	var result string
	if match.Winner == "DRAW" {
		result = "DRAW"
	} else if match.Winner == "HOME_TEAM" {
		result = match.HomeTeam + " won"
	} else if match.Winner == "AWAY_TEAM" {
		result = match.AwayTeam + " won"
	} else {
		result = "Unknown result"
	}

	return "<b>Match Finished</b>\n" +
		formatTeams(match) +
		"\n<b>Score:</b> " + formatScore(match) +
		"\n<b>Result:</b> " + result +
		"\nBets have been resolved."
}

func formatTeams(match *models.Match) string {
	return "<b>" + match.HomeTeam + "</b> vs <b>" + match.AwayTeam + "</b>"
}

func formatScore(match *models.Match) string {
	return fmt.Sprintf("%d - %d", match.HomeScore, match.AwayScore)
}

// buildBetOutcomeLines appends per-user bet outcome lines for a group
func buildBetOutcomeLines(database *db.DB, match *models.Match, groupChatID int64) string {
	bets, err := database.GetBetsForMatchInGroup(match.ID, groupChatID)
	if err != nil || len(bets) == 0 {
		return ""
	}

	lines := "\n"
	for _, bet := range bets {
		user, err := database.GetUserByID(bet.UserID)
		if err != nil || user == nil {
			continue
		}
		name := user.DisplayName
		if name == "" {
			name = user.Username
		}
		if name == "" {
			name = "User"
		}
		teamName := match.AwayTeam
		if bet.PickedTeam == "HOME_TEAM" {
			teamName = match.HomeTeam
		}
		var outcomeText string
		switch bet.Outcome {
		case "WIN":
			outcomeText = "✅ " + name + " won this bet"
		case "LOSS":
			outcomeText = "❌ " + name + " lost this bet"
		case "DRAW":
			outcomeText = "🤝 " + name + " drew (picked " + teamName + ")"
		default:
			outcomeText = "⏳ " + name + " (pending)"
		}
		lines += outcomeText + "\n"
	}
	return lines
}
