package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramToken       string
	GroupChatID         int64
	AdminUserID         int64
	FootballAPIKey      string
	CompetitionCode     string
	Season              int
	PollIntervalMin     int
	SheetsCredsB64      string
	SheetsSpreadID      string
	SheetsTabName       string
	DBPath              string
	DailyBroadcastCron  string
	Timezone            string
	User1TelegramID     int64
	User2TelegramID     int64
}

func Load() (*Config, error) {
	// Load .env file if it exists
	if err := loadEnvFile(); err != nil {
		return nil, err
	}

	cfg := &Config{}

	// Required fields
	cfg.TelegramToken = os.Getenv("TELEGRAM_TOKEN")
	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_TOKEN is required")
	}

	// Optional field: GroupChatID (no longer required, bot discovers groups dynamically)
	groupChatIDStr := os.Getenv("GROUP_CHAT_ID")
	if groupChatIDStr != "" {
		groupChatID, err := strconv.ParseInt(groupChatIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GROUP_CHAT_ID: %w", err)
		}
		cfg.GroupChatID = groupChatID
	}

	adminUserIDStr := os.Getenv("ADMIN_TELEGRAM_ID")
	if adminUserIDStr == "" {
		return nil, fmt.Errorf("ADMIN_TELEGRAM_ID is required")
	}
	adminUserID, err := strconv.ParseInt(adminUserIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid ADMIN_TELEGRAM_ID: %w", err)
	}
	cfg.AdminUserID = adminUserID

	cfg.FootballAPIKey = os.Getenv("FOOTBALL_API_KEY")
	if cfg.FootballAPIKey == "" {
		return nil, fmt.Errorf("FOOTBALL_API_KEY is required")
	}

	cfg.SheetsSpreadID = os.Getenv("SHEETS_SPREADSHEET_ID")
	if cfg.SheetsSpreadID == "" {
		return nil, fmt.Errorf("SHEETS_SPREADSHEET_ID is required")
	}

	// Optional fields with defaults
	cfg.CompetitionCode = os.Getenv("COMPETITION_CODE")
	if cfg.CompetitionCode == "" {
		cfg.CompetitionCode = "WC"
	}

	seasonStr := os.Getenv("SEASON")
	if seasonStr == "" {
		cfg.Season = 2026
	} else {
		season, err := strconv.Atoi(seasonStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SEASON: %w", err)
		}
		cfg.Season = season
	}

	pollIntervalStr := os.Getenv("POLL_INTERVAL_MIN")
	if pollIntervalStr == "" {
		cfg.PollIntervalMin = 15
	} else {
		pollInterval, err := strconv.Atoi(pollIntervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid POLL_INTERVAL_MIN: %w", err)
		}
		cfg.PollIntervalMin = pollInterval
	}

	cfg.SheetsCredsB64 = os.Getenv("GOOGLE_CREDENTIALS_B64")
	if cfg.SheetsCredsB64 == "" {
		return nil, fmt.Errorf("GOOGLE_CREDENTIALS_B64 is required")
	}

	cfg.SheetsTabName = os.Getenv("SHEETS_TAB_NAME")
	if cfg.SheetsTabName == "" {
		cfg.SheetsTabName = "Bets"
	}

	cfg.DBPath = os.Getenv("DB_PATH")
	if cfg.DBPath == "" {
		cfg.DBPath = "/data/bets.db"
	}

	cfg.DailyBroadcastCron = os.Getenv("DAILY_BROADCAST_CRON")
	if cfg.DailyBroadcastCron == "" {
		cfg.DailyBroadcastCron = "0 8 * * *"
	}

	cfg.Timezone = os.Getenv("TIMEZONE")
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Ho_Chi_Minh"
	}

	// Optional user IDs
	user1Str := os.Getenv("USER1_TELEGRAM_ID")
	if user1Str != "" {
		user1, err := strconv.ParseInt(user1Str, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid USER1_TELEGRAM_ID: %w", err)
		}
		cfg.User1TelegramID = user1
	}

	user2Str := os.Getenv("USER2_TELEGRAM_ID")
	if user2Str != "" {
		user2, err := strconv.ParseInt(user2Str, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid USER2_TELEGRAM_ID: %w", err)
		}
		cfg.User2TelegramID = user2
	}

	return cfg, nil
}

// loadEnvFile loads key=value pairs from .env file if it exists
func loadEnvFile() error {
	envFile := ".env"
	f, err := os.Open(envFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // .env file is optional
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse key=value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			os.Setenv(key, value)
		}
	}

	return scanner.Err()
}
