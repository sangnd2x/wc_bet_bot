package telegram

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/robfig/cron/v3"
	"worldcup-bet-bot/internal/config"
	"worldcup-bet-bot/internal/football"
	"worldcup-bet-bot/internal/models"
)

type Scheduler struct {
	bot    *Bot
	cron   *cron.Cron
	poller *football.Poller
	cfg    *config.Config
}

func NewScheduler(bot *Bot, poller *football.Poller, cfg *config.Config) (*Scheduler, error) {
	return &Scheduler{
		bot:    bot,
		cron:   cron.New(cron.WithLocation(time.UTC)),
		poller: poller,
		cfg:    cfg,
	}, nil
}

// Start registers all cron jobs and starts the scheduler.
// Returns error if timezone is invalid.
func (s *Scheduler) Start() error {
	// Parse timezone
	loc, err := time.LoadLocation(s.cfg.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %s: %w", s.cfg.Timezone, err)
	}

	// Create a new cron with the specified timezone
	cronWithTZ := cron.New(cron.WithLocation(loc))

	// Job 1: Daily broadcast (cfg.DailyBroadcastCron)
	_, err = cronWithTZ.AddFunc(s.cfg.DailyBroadcastCron, func() {
		s.dailyBroadcast(context.Background())
	})
	if err != nil {
		return fmt.Errorf("failed to add daily broadcast job: %w", err)
	}

	// Job 2: Result poller (every 15 min)
	_, err = cronWithTZ.AddFunc("*/15 * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.poller.Poll(ctx); err != nil {
			log.Printf("Poll error: %v", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add result poller job: %w", err)
	}

	// Job 3: Match sync (every 6 hours)
	_, err = cronWithTZ.AddFunc("0 */6 * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.poller.SyncMatches(ctx); err != nil {
			log.Printf("SyncMatches error: %v", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add match sync job: %w", err)
	}

	// Job 4: Reconciliation (every hour) — silently resolves stale finished matches
	_, err = cronWithTZ.AddFunc("0 * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.poller.Reconcile(ctx); err != nil {
			log.Printf("Reconcile error: %v", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add reconciliation job: %w", err)
	}

	// Job 5: 30-min pre-match reminder (every 5 minutes)
	_, err = cronWithTZ.AddFunc("*/5 * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.preMatchReminder(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to add pre-match reminder job: %w", err)
	}

	s.cron = cronWithTZ
	s.cron.Start()

	log.Printf("Scheduler started with timezone %s", s.cfg.Timezone)
	log.Printf("Daily broadcast cron: %s", s.cfg.DailyBroadcastCron)

	return nil
}

// Stop gracefully shuts down the scheduler
func (s *Scheduler) Stop() {
	if s.cron != nil {
		<-s.cron.Stop().Done()
		log.Println("Scheduler stopped")
	}
}

// dailyBroadcast fetches today's matches and sends them to all known groups
func (s *Scheduler) dailyBroadcast(ctx context.Context) {
	log.Println("Running daily broadcast")

	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Fetch today's matches from DB
	matches, err := s.bot.db.GetMatchesByDate(today)
	if err != nil {
		log.Printf("Failed to get today's matches: %v", err)
		return
	}

	// If empty, try API
	if len(matches) == 0 {
		log.Println("No matches in DB for today, fetching from API...")
		apiMatches, err := s.bot.fbClient.GetMatchesByDate(ctx, today)
		if err != nil {
			log.Printf("Failed to fetch from API: %v", err)
			return
		}

		// Upsert to DB
		for i := range apiMatches {
			apiMatches[i].MatchDate = today
			if err := s.bot.db.UpsertMatch(&apiMatches[i]); err != nil {
				log.Printf("Failed to upsert match: %v", err)
			}
		}

		for i := range apiMatches {
			matches = append(matches, &apiMatches[i])
		}
	}

	// Send each match to all known groups
	if len(matches) == 0 {
		log.Println("No matches for today")
		return
	}

	// Get all registered groups
	groups, err := s.bot.db.GetAllGroups()
	if err != nil {
		log.Printf("Failed to get groups: %v", err)
		return
	}

	// If no groups yet, try fallback to configured group if it exists
	if len(groups) == 0 {
		if s.bot.cfg.GroupChatID != 0 {
			log.Println("No groups registered, using configured fallback GroupChatID")
			groups = append(groups, &models.Group{
				ChatID:    s.bot.cfg.GroupChatID,
				Title:     "Default Group",
				CreatedAt: time.Now(),
			})
		} else {
			log.Println("No groups registered and no fallback GroupChatID configured")
			return
		}
	}

	// Send to each group
	for _, group := range groups {
		msg := fmt.Sprintf("🏆 Today's matches (%s):\n\n", today.Format("02/01/2006"))
		if err := s.bot.SendToChat(group.ChatID, msg); err != nil {
			log.Printf("Failed to send header message to group %d: %v", group.ChatID, err)
		}

		for _, match := range matches {
			if err := s.bot.SendMatchToChatForGroup(group.ChatID, match); err != nil {
				log.Printf("Failed to send match to group %d: %v", group.ChatID, err)
			}
		}
	}
}

// SetMatchResolvedCallback sets the callback that is called when a match is resolved.
// This callback is used by the poller to notify the bot about match results.
func (s *Scheduler) SetMatchResolvedCallback(callback func(match *models.Match)) {
	// Note: The poller is created with a callback in main, so we don't need to
	// set it here. But this is a helper for external use if needed.
	log.Println("Match resolved callback set (via poller initialization)")
}

// preMatchReminder checks for matches starting within 30 minutes and sends
// per-group notifications based on current bet state.
func (s *Scheduler) preMatchReminder(ctx context.Context) {
	matches, err := s.bot.db.GetMatchesStartingIn30Min()
	if err != nil {
		log.Printf("preMatchReminder: failed to get matches: %v", err)
		return
	}
	if len(matches) == 0 {
		return
	}

	groups, err := s.bot.db.GetAllGroups()
	if err != nil {
		log.Printf("preMatchReminder: failed to get groups: %v", err)
		return
	}
	if len(groups) == 0 && s.bot.cfg.GroupChatID != 0 {
		groups = append(groups, &models.Group{
			ChatID:    s.bot.cfg.GroupChatID,
			Title:     "Default Group",
			CreatedAt: time.Now(),
		})
	}
	if len(groups) == 0 {
		return
	}

	for _, match := range matches {
		for _, group := range groups {
			bets, err := s.bot.db.GetBetsForMatchInGroup(match.ID, group.ChatID)
			if err != nil {
				log.Printf("preMatchReminder: get bets err match %d group %d: %v", match.ID, group.ChatID, err)
				continue
			}

			switch len(bets) {
			case 0:
				// No bets — send reminder with betting keyboard
				header := fmt.Sprintf("⚠️ <b>%s vs %s</b> starts in ~30 min!\nNo bets placed yet. Bet now or it counts as a draw:",
					match.HomeTeam, match.AwayTeam)
				s.bot.SendToChat(group.ChatID, header)
				s.bot.SendMatchToChat(group.ChatID, match)

			case 1:
				// One bet placed — remind partner
				bet := bets[0]
				bettor, err := s.bot.db.GetUserByID(bet.UserID)
				if err != nil || bettor == nil {
					continue
				}
				bettorName := bettor.DisplayName
				if bettorName == "" {
					bettorName = bettor.Username
				}
				if bettorName == "" {
					bettorName = "Someone"
				}
				teamName := match.AwayTeam
				if bet.PickedTeam == "HOME_TEAM" {
					teamName = match.HomeTeam
				}
				text := fmt.Sprintf(
					"⚠️ <b>%s vs %s</b> starts in ~30 min!\n<b>%s</b> picked <b>%s</b> but partner hasn't bet yet!\nBet now — if no partner bets, result depends on <b>%s</b>'s pick!",
					match.HomeTeam, match.AwayTeam, bettorName, teamName, bettorName,
				)
				s.bot.SendToChat(group.ChatID, text)
				s.bot.SendMatchToChat(group.ChatID, match)

			default:
				// 2+ bets — bet is on, no notification needed
			}
		}

		// Mark notified so this match isn't re-processed on the next 5-min tick
		if err := s.bot.db.MarkMatchNotified(match.ID); err != nil {
			log.Printf("preMatchReminder: failed to mark match %d notified: %v", match.ID, err)
		}
	}
}
