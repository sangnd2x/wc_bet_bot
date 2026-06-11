package sheets

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type BetRow struct {
	MatchDate    string // DD/MM/YYYY
	MatchID      int
	HomeTeam     string
	AwayTeam     string
	User1Name    string
	User1Pick    string // team name string (not HOME_TEAM/AWAY_TEAM)
	User2Name    string
	User2Pick    string
	ActualWinner string // filled on resolution
	User1Result  string // WIN/LOSS/DRAW
	User2Result  string // WIN/LOSS/DRAW
}

type Client struct {
	svc           *sheets.Service
	spreadsheetID string
	tabName       string
}

func NewClient(ctx context.Context, credJSON []byte, spreadsheetID, tabName string) (*Client, error) {
	svc, err := sheets.NewService(ctx, option.WithCredentialsJSON(credJSON), option.WithScopes("https://www.googleapis.com/auth/spreadsheets"))
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	return &Client{
		svc:           svc,
		spreadsheetID: spreadsheetID,
		tabName:       tabName,
	}, nil
}

// AppendBetRow appends a new row (columns A-H) and returns the row number (1-indexed)
func (c *Client) AppendBetRow(ctx context.Context, row BetRow) (int, error) {
	values := []interface{}{
		row.MatchDate,
		row.MatchID,
		row.HomeTeam,
		row.AwayTeam,
		row.User1Name,
		row.User1Pick,
		row.User2Name,
		row.User2Pick,
		row.ActualWinner,
		row.User1Result,
		row.User2Result,
	}

	rangeStr := fmt.Sprintf("%s!A:K", c.tabName)
	appendReq := c.svc.Spreadsheets.Values.Append(c.spreadsheetID, rangeStr, &sheets.ValueRange{
		Values: [][]interface{}{values},
	})
	appendReq.ValueInputOption("RAW")
	appendReq.InsertDataOption("INSERT_ROWS")

	resp, err := appendReq.Do()
	if err != nil {
		return 0, fmt.Errorf("failed to append bet row: %w", err)
	}

	// Extract row number from UpdatedRange (e.g., "Bets!A1:K1")
	rowNum, err := extractRowNumber(resp.Updates.UpdatedRange)
	if err != nil {
		return 0, fmt.Errorf("failed to extract row number: %w", err)
	}

	return rowNum, nil
}

// UpdateBetResult updates columns I-K for a specific row
func (c *Client) UpdateBetResult(ctx context.Context, rowNum int, actualWinner, user1Result, user2Result string) error {
	values := []interface{}{
		actualWinner,
		user1Result,
		user2Result,
	}

	rangeStr := fmt.Sprintf("%s!I%d:K%d", c.tabName, rowNum, rowNum)
	updateReq := c.svc.Spreadsheets.Values.Update(c.spreadsheetID, rangeStr, &sheets.ValueRange{
		Values: [][]interface{}{values},
	})
	updateReq.ValueInputOption("RAW")

	_, err := updateReq.Do()
	if err != nil {
		return fmt.Errorf("failed to update bet result: %w", err)
	}

	return nil
}

// extractRowNumber parses A1 notation range to extract row number
// Example: "Bets!A1:K1" -> 1
func extractRowNumber(a1Range string) (int, error) {
	// Match the first occurrence of digits after a letter (e.g., A1, A123)
	re := regexp.MustCompile(`[A-Z]+(\d+)`)
	matches := re.FindStringSubmatch(a1Range)
	if len(matches) < 2 {
		return 0, fmt.Errorf("invalid A1 range format: %s", a1Range)
	}

	rowNum, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("failed to parse row number: %w", err)
	}

	return rowNum, nil
}
