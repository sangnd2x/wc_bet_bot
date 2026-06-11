package football

import (
	"time"
	"worldcup-bet-bot/internal/models"
)

// API response structs

type APIMatchesResponse struct {
	Matches []APIMatch `json:"matches"`
}

type APIMatch struct {
	ID       int       `json:"id"`
	UTCDate  string    `json:"utcDate"` // "2026-06-11T20:00:00Z"
	Status   string    `json:"status"` // SCHEDULED|IN_PLAY|PAUSED|FINISHED|POSTPONED|CANCELLED
	HomeTeam APITeam   `json:"homeTeam"`
	AwayTeam APITeam   `json:"awayTeam"`
	Score    APIScore  `json:"score"`
}

type APITeam struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type APIScore struct {
	Winner   *string      `json:"winner"` // "HOME_TEAM"|"AWAY_TEAM"|"DRAW"|null
	FullTime APIScoreTime `json:"fullTime"`
}

type APIScoreTime struct {
	Home *int `json:"home"`
	Away *int `json:"away"`
}

// ToMatch converts an APIMatch to a models.Match
func (am *APIMatch) ToMatch() (*models.Match, error) {
	kickoffUTC, err := time.Parse(time.RFC3339, am.UTCDate)
	if err != nil {
		return nil, err
	}

	match := &models.Match{
		ExternalID: am.ID,
		HomeTeam:   am.HomeTeam.Name,
		AwayTeam:   am.AwayTeam.Name,
		KickoffUTC: kickoffUTC,
		MatchDate:  kickoffUTC.Truncate(24 * time.Hour),
		Status:     am.Status,
	}

	// Set score if available
	if am.Score.FullTime.Home != nil && am.Score.FullTime.Away != nil {
		match.HomeScore = *am.Score.FullTime.Home
		match.AwayScore = *am.Score.FullTime.Away
	}

	// Set winner
	if am.Score.Winner != nil {
		match.Winner = *am.Score.Winner
	}

	return match, nil
}
