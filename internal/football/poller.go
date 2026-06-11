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
