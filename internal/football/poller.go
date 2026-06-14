package football

import (
	"context"
	"fmt"
	"log"
	"time"
	"worldcup-bet-bot/internal/db"
	"worldcup-bet-bot/internal/models"
)

type Poller struct {
	client           *Client
	db               *db.DB
	onMatchResolved  func(match *models.Match)
}

// NewPoller creates a new match poller
func NewPoller(client *Client, db *db.DB, onMatchResolved func(match *models.Match)) *Poller {
	return &Poller{
		client:          client,
		db:              db,
		onMatchResolved: onMatchResolved,
	}
}

// Poll checks for match results and resolves finished matches.
// Call this on a cron schedule.
func (p *Poller) Poll(ctx context.Context) error {
	log.Println("Starting match poll")

	// Get all active matches from DB (status SCHEDULED/IN_PLAY/PAUSED)
	activeMatches, err := p.db.GetActiveMatches()
	if err != nil {
		return fmt.Errorf("failed to get active matches from DB: %w", err)
	}

	now := time.Now().UTC()

	for _, match := range activeMatches {
		// Only fetch from API if kickoff is in the past
		if match.KickoffUTC.After(now) {
			continue
		}

		log.Printf("Checking match %d: %s vs %s (status: %s)",
			match.ExternalID, match.HomeTeam, match.AwayTeam, match.Status)

		// Fetch from API
		apiMatch, err := p.client.GetMatchByExternalID(ctx, match.ExternalID)
		if err != nil {
			log.Printf("Failed to fetch match %d from API: %v", match.ExternalID, err)
			continue
		}

		// Check if status has changed
		if apiMatch.Status == match.Status {
			continue
		}

		log.Printf("Match %d status changed from %s to %s",
			match.ExternalID, match.Status, apiMatch.Status)

		// Update match in DB
		match.Status = apiMatch.Status

		if apiMatch.Status == "FINISHED" {
			match.HomeScore = apiMatch.HomeScore
			match.AwayScore = apiMatch.AwayScore
			match.Winner = apiMatch.Winner
			log.Printf("Match %d finished: %s %d-%d %s",
				match.ExternalID, match.HomeTeam, match.HomeScore, match.AwayScore, match.AwayTeam)
		} else if apiMatch.Status == "POSTPONED" {
			log.Printf("Match %d has been postponed", match.ExternalID)
		} else if apiMatch.Status == "CANCELLED" {
			log.Printf("Match %d has been cancelled", match.ExternalID)
		}

		// Update in DB
		if err := p.db.UpsertMatch(match); err != nil {
			log.Printf("Failed to update match %d in DB: %v", match.ExternalID, err)
			continue
		}

		// Call the callback
		if p.onMatchResolved != nil {
			p.onMatchResolved(match)
		}
	}

	log.Println("Match poll completed")
	return nil
}

// Reconcile finds matches that should be finished (kickoff > 2h ago, still non-terminal)
// and silently resolves their bets without sending any Telegram announcements.
// This catches matches the regular poller missed due to API failures or status edge cases.
func (p *Poller) Reconcile(ctx context.Context) error {
	log.Println("Starting reconciliation")

	cutoff := time.Now().UTC().Add(-2 * time.Hour)
	stale, err := p.db.GetStaleMatches(cutoff)
	if err != nil {
		return fmt.Errorf("failed to get stale matches: %w", err)
	}

	if len(stale) == 0 {
		log.Println("Reconciliation: no stale matches")
	}

	for _, match := range stale {
		log.Printf("Reconcile: checking stale match %d (%s vs %s, status: %s)",
			match.ExternalID, match.HomeTeam, match.AwayTeam, match.Status)

		apiMatch, err := p.client.GetMatchByExternalID(ctx, match.ExternalID)
		if err != nil {
			log.Printf("Reconcile: failed to fetch match %d from API: %v", match.ExternalID, err)
			continue
		}

		if apiMatch.Status != "FINISHED" && apiMatch.Status != "CANCELLED" && apiMatch.Status != "POSTPONED" {
			log.Printf("Reconcile: match %d still not terminal (status: %s), skipping", match.ExternalID, apiMatch.Status)
			continue
		}

		log.Printf("Reconcile: match %d resolved as %s", match.ExternalID, apiMatch.Status)

		match.Status = apiMatch.Status
		if apiMatch.Status == "FINISHED" {
			match.HomeScore = apiMatch.HomeScore
			match.AwayScore = apiMatch.AwayScore
			match.Winner = apiMatch.Winner
		}

		if err := p.db.UpsertMatch(match); err != nil {
			log.Printf("Reconcile: failed to update match %d: %v", match.ExternalID, err)
			continue
		}

		if apiMatch.Status == "FINISHED" {
			if err := p.db.ResolveBets(match.ID, match.Winner, match.HomeScore, match.AwayScore); err != nil {
				log.Printf("Reconcile: failed to resolve bets for match %d: %v", match.ID, err)
			} else {
				log.Printf("Reconcile: silently resolved bets for match %d (%s vs %s)",
					match.ID, match.HomeTeam, match.AwayTeam)
			}
		} else if apiMatch.Status == "CANCELLED" {
			if err := p.db.DeleteBetsForMatch(match.ID); err != nil {
				log.Printf("Reconcile: failed to delete bets for cancelled match %d: %v", match.ID, err)
			}
		}
	}

	// Second pass: resolve any already-FINISHED matches with unresolved bets
	// (catches cases where UpsertMatch set status=FINISHED but ResolveBets never ran)
	finished, err := p.db.GetFinishedMatchesWithUnresolvedBets()
	if err != nil {
		log.Printf("Reconcile: failed to get finished unresolved matches: %v", err)
	} else {
		for _, match := range finished {
			log.Printf("Reconcile: resolving bets for already-finished match %d (%s vs %s)",
				match.ID, match.HomeTeam, match.AwayTeam)
			if err := p.db.ResolveBets(match.ID, match.Winner, match.HomeScore, match.AwayScore); err != nil {
				log.Printf("Reconcile: failed to resolve bets for match %d: %v", match.ID, err)
			} else {
				log.Printf("Reconcile: resolved bets for match %d (%s vs %s)",
					match.ID, match.HomeTeam, match.AwayTeam)
				if p.onMatchResolved != nil {
					p.onMatchResolved(match)
				}
			}
		}
	}

	log.Println("Reconciliation completed")
	return nil
}

// SyncMatches fetches and upserts upcoming matches into DB
func (p *Poller) SyncMatches(ctx context.Context) error {
	log.Println("Starting match sync")

	// Fetch next 30 days of matches
	matches, err := p.client.GetUpcomingMatches(ctx, 30)
	if err != nil {
		return fmt.Errorf("failed to get upcoming matches from API: %w", err)
	}

	log.Printf("Fetched %d upcoming matches from API", len(matches))

	// Upsert into DB
	for _, match := range matches {
		if err := p.db.UpsertMatch(&match); err != nil {
			log.Printf("Failed to upsert match %d: %v", match.ExternalID, err)
			continue
		}
	}

	log.Println("Match sync completed")
	return nil
}
