package football

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"worldcup-bet-bot/internal/models"
)

const BaseURL = "https://api.football-data.org/v4"

type Client struct {
	apiKey          string
	competitionCode string
	season          int
	httpClient      *http.Client
}

// NewClient creates a new football-data.org API client
func NewClient(apiKey, competitionCode string, season int) *Client {
	return &Client{
		apiKey:          apiKey,
		competitionCode: competitionCode,
		season:          season,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetMatchesByDate fetches matches for a specific calendar date
func (c *Client) GetMatchesByDate(ctx context.Context, date time.Time) ([]models.Match, error) {
	dateStr := date.Format("2006-01-02")
	url := fmt.Sprintf("%s/competitions/%s/matches?dateFrom=%s&dateTo=%s&season=%d",
		BaseURL, c.competitionCode, dateStr, dateStr, c.season)

	matches, err := c.fetchMatches(ctx, url)
	if err != nil {
		return nil, err
	}

	return matches, nil
}

// GetMatchByExternalID fetches a single match by ID
func (c *Client) GetMatchByExternalID(ctx context.Context, externalID int) (*models.Match, error) {
	url := fmt.Sprintf("%s/matches/%d", BaseURL, externalID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Auth-Token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: status %d, response: %s", resp.StatusCode, string(body))
	}

	var apiMatch APIMatch
	if err := json.NewDecoder(resp.Body).Decode(&apiMatch); err != nil {
		return nil, fmt.Errorf("failed to decode match: %w", err)
	}

	return apiMatch.ToMatch()
}

// GetActiveMatches fetches matches with status IN_PLAY or PAUSED for today
func (c *Client) GetActiveMatches(ctx context.Context) ([]models.Match, error) {
	// Fetch IN_PLAY matches
	urlInPlay := fmt.Sprintf("%s/competitions/%s/matches?status=IN_PLAY&season=%d",
		BaseURL, c.competitionCode, c.season)

	inPlayMatches, err := c.fetchMatches(ctx, urlInPlay)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch in-play matches: %w", err)
	}

	// Fetch PAUSED matches
	urlPaused := fmt.Sprintf("%s/competitions/%s/matches?status=PAUSED&season=%d",
		BaseURL, c.competitionCode, c.season)

	pausedMatches, err := c.fetchMatches(ctx, urlPaused)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch paused matches: %w", err)
	}

	// Combine results
	allMatches := append(inPlayMatches, pausedMatches...)
	return allMatches, nil
}

// GetUpcomingMatches fetches SCHEDULED and TIMED matches for the next N days
func (c *Client) GetUpcomingMatches(ctx context.Context, days int) ([]models.Match, error) {
	now := time.Now().UTC()
	endDate := now.AddDate(0, 0, days)

	dateFromStr := now.Format("2006-01-02")
	dateToStr := endDate.Format("2006-01-02")

	scheduledURL := fmt.Sprintf("%s/competitions/%s/matches?status=SCHEDULED&dateFrom=%s&dateTo=%s&season=%d",
		BaseURL, c.competitionCode, dateFromStr, dateToStr, c.season)
	timedURL := fmt.Sprintf("%s/competitions/%s/matches?status=TIMED&dateFrom=%s&dateTo=%s&season=%d",
		BaseURL, c.competitionCode, dateFromStr, dateToStr, c.season)

	scheduled, err := c.fetchMatches(ctx, scheduledURL)
	if err != nil {
		return nil, err
	}
	timed, err := c.fetchMatches(ctx, timedURL)
	if err != nil {
		return nil, err
	}

	all := append(scheduled, timed...)
	// Sort by kickoff
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].KickoffUTC.Before(all[j-1].KickoffUTC); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	return all, nil
}

// fetchMatches is a helper that fetches and decodes matches from a given URL
func (c *Client) fetchMatches(ctx context.Context, url string) ([]models.Match, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Auth-Token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: status %d, response: %s", resp.StatusCode, string(body))
	}

	var apiResp APIMatchesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode matches response: %w", err)
	}

	var matches []models.Match
	for _, apiMatch := range apiResp.Matches {
		match, err := apiMatch.ToMatch()
		if err != nil {
			return nil, fmt.Errorf("failed to convert API match %d: %w", apiMatch.ID, err)
		}
		matches = append(matches, *match)
	}

	return matches, nil
}
