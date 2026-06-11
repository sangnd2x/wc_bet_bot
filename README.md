# World Cup Betting Telegram Bot

A Telegram bot for managing World Cup betting pools with integration to football-data.org API and Google Sheets for tracking bets and results.

## Prerequisites

- **Go** 1.22 or higher
- **Docker** and Docker Compose (for containerized deployment)
- **football-data.org API Key** (free tier at [football-data.org](https://www.football-data.org/))
- **Google Cloud Service Account** with Sheets API enabled
- **Telegram Bot Token** (from [@BotFather](https://t.me/botfather))

## Configuration

### 1. Environment Setup

Copy the example configuration:

```bash
cp .env.example .env
```

Fill in the required values in `.env`:

| Variable | Description |
|----------|-------------|
| `TELEGRAM_TOKEN` | Bot token from @BotFather |
| `GROUP_CHAT_ID` | Telegram group chat ID for broadcasting |
| `ADMIN_TELEGRAM_ID` | Your Telegram user ID (get from @userinfobot) |
| `FOOTBALL_API_KEY` | API key from football-data.org |
| `COMPETITION_CODE` | Competition code (default: WC for World Cup) |
| `SEASON` | Season year (2026 for next World Cup) |
| `POLL_INTERVAL_MIN` | Polling interval in minutes for match updates |
| `SHEETS_SPREADSHEET_ID` | Google Sheets spreadsheet ID |
| `SHEETS_TAB_NAME` | Tab name in spreadsheet (default: Bets) |
| `SHEETS_CRED_FILE` | Path to service account JSON file |
| `DB_PATH` | Path to SQLite database file |
| `DAILY_BROADCAST_CRON` | Cron expression for daily broadcast (default: 8 AM) |
| `TIMEZONE` | Timezone for scheduling (default: Asia/Ho_Chi_Minh) |
| `USER1_TELEGRAM_ID` | First bettor's Telegram user ID |
| `USER2_TELEGRAM_ID` | Second bettor's Telegram user ID |

### 2. Google Sheets Setup

1. Create a new Google Sheet for tracking bets
2. Create a service account on [Google Cloud Console](https://console.cloud.google.com/)
3. Enable the Google Sheets API
4. Download the service account JSON key
5. Save to `credentials/service_account.json`
6. Share the spreadsheet with the service account email
7. Note the spreadsheet ID (from the URL: `docs.google.com/spreadsheets/d/{SPREADSHEET_ID}`)

### 3. Telegram Setup

1. Create a bot with [@BotFather](https://t.me/botfather)
2. Copy the bot token to `TELEGRAM_TOKEN` in `.env`
3. Get your user ID from [@userinfobot](https://t.me/userinfobot)
4. Create a group chat and add the bot
5. Get the group chat ID and add to `GROUP_CHAT_ID` in `.env`

## Running Locally

Install dependencies:

```bash
go mod download
```

Run the bot:

```bash
go run ./cmd/bot
```

## Running with Docker

Build and start the services:

```bash
docker-compose up -d
```

View logs:

```bash
docker-compose logs -f bot
```

Stop the bot:

```bash
docker-compose down
```

## Commands Reference

| Command | Description | Permissions |
|---------|-------------|-------------|
| `/start` | Initialize bot and get help | All users |
| `/bet <match_id> <pick>` | Place a bet on a match | All users |
| `/bets` | View your active bets | All users |
| `/results` | View all betting results | All users |
| `/admin_sync` | Manually sync with Google Sheets | Admin only |
| `/admin_status` | Check bot and API status | Admin only |

## Project Structure

```
worldcup-bet-bot/
├── cmd/bot/
│   └── main.go              # Entry point
├── internal/
│   ├── sheets/
│   │   └── client.go        # Google Sheets integration
│   ├── football/            # football-data.org API client
│   ├── database/            # SQLite database layer
│   └── handler/             # Telegram message handlers
├── .env.example             # Configuration template
├── Dockerfile               # Container image definition
├── docker-compose.yml       # Docker Compose configuration
├── .gitignore               # Git ignore rules
├── README.md                # This file
├── go.mod                   # Go module definition
└── go.sum                   # Go module checksums
```

## Database Schema

The SQLite database stores bets with the following structure:

- **bets** table: Match ID, user picks, actual result
- **matches** table: Match details, date, teams
- **users** table: User credentials, preferences

## Troubleshooting

### Bot doesn't respond to commands
- Verify `TELEGRAM_TOKEN` is correct
- Ensure bot is added to the group chat
- Check bot permissions in group settings

### Google Sheets sync fails
- Verify service account JSON is accessible
- Check spreadsheet is shared with service account email
- Ensure `SHEETS_SPREADSHEET_ID` matches the spreadsheet URL

### No match data appears
- Verify `FOOTBALL_API_KEY` is correct
- Check `SEASON` and `COMPETITION_CODE` are valid
- Ensure network connectivity to football-data.org

## License

MIT
