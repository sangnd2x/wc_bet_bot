package models

import "time"

type User struct {
	ID          int64
	TelegramID  int64
	Username    string
	DisplayName string
	RegisteredAt time.Time
}

type Match struct {
	ID         int64
	ExternalID int
	HomeTeam   string
	AwayTeam   string
	MatchDate  time.Time
	KickoffUTC time.Time
	Status     string // SCHEDULED | IN_PLAY | PAUSED | FINISHED | POSTPONED | CANCELLED
	Winner     string // HOME_TEAM | AWAY_TEAM | DRAW | ""
	HomeScore  int
	AwayScore  int
	LastSyncedAt time.Time
}

type Bet struct {
	ID               int64
	MatchID          int64
	UserID           int64
	PickedTeam       string // HOME_TEAM | AWAY_TEAM
	Outcome          string // WIN | LOSS | DRAW | "" (pending)
	TelegramMessageID int
	SheetsRow        int
	CreatedAt        time.Time
	ResolvedAt       *time.Time
	GroupChatID      int64
}

type UserRecord struct {
	User   User
	Wins   int
	Losses int
	Draws  int
	Total  int
}

type Group struct {
	ChatID    int64
	Title     string
	CreatedAt time.Time
}
