package telegram

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"worldcup-bet-bot/internal/config"
	"worldcup-bet-bot/internal/db"
	"worldcup-bet-bot/internal/football"
	"worldcup-bet-bot/internal/models"
	"worldcup-bet-bot/internal/sheets"
)

// pendingGuessKey identifies a user's pending guess keyboard in a specific chat.
type pendingGuessKey struct {
	userID int64
	chatID int64
}

type Bot struct {
	api          *tgbotapi.BotAPI
	db           *db.DB
	cfg          *config.Config
	sheetsClient *sheets.Client
	fbClient     *football.Client
	loc          *time.Location
	poller       *football.Poller
	// pendingGuessKB tracks the last "which match?" keyboard message per user+chat
	// so it can be cleared when the user issues a new /guess.
	pendingGuessKB   map[pendingGuessKey]int
	pendingGuessKBMu sync.Mutex
}

func NewBot(cfg *config.Config, database *db.DB, sheetsClient *sheets.Client, fbClient *football.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Printf("warning: invalid timezone %q, falling back to UTC: %v", cfg.Timezone, err)
		loc = time.UTC
	}

	log.Printf("Authorized on account %s", api.Self.UserName)

	return &Bot{
		api:            api,
		db:             database,
		cfg:            cfg,
		sheetsClient:   sheetsClient,
		fbClient:       fbClient,
		loc:            loc,
		pendingGuessKB: make(map[pendingGuessKey]int),
	}, nil
}

// SetPoller sets the football poller (called after poller creation in main)
func (b *Bot) SetPoller(p *football.Poller) {
	b.poller = p
}

// clearPendingGuessKB removes the old "which match?" keyboard for a user+chat, if any.
func (b *Bot) clearPendingGuessKB(chatID, userID int64) {
	k := pendingGuessKey{userID, chatID}
	b.pendingGuessKBMu.Lock()
	msgID, exists := b.pendingGuessKB[k]
	delete(b.pendingGuessKB, k)
	b.pendingGuessKBMu.Unlock()

	if exists {
		edit := tgbotapi.NewEditMessageReplyMarkup(chatID, msgID, tgbotapi.InlineKeyboardMarkup{})
		b.api.Send(edit)
	}
}

// storePendingGuessKB records the message ID of a pending guess keyboard.
func (b *Bot) storePendingGuessKB(chatID, userID int64, msgID int) {
	k := pendingGuessKey{userID, chatID}
	b.pendingGuessKBMu.Lock()
	b.pendingGuessKB[k] = msgID
	b.pendingGuessKBMu.Unlock()
}

// Start begins long-polling for updates. Blocks until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			log.Println("Bot context cancelled, stopping...")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}

			// Route updates
			if update.Message != nil {
				b.EnsureGroupRegistered(update.Message.Chat)
				b.handleMessage(ctx, update.Message)
			} else if update.CallbackQuery != nil {
				if update.CallbackQuery.Message != nil {
					b.EnsureGroupRegistered(update.CallbackQuery.Message.Chat)
				}
				b.handleCallbackQuery(ctx, update.CallbackQuery)
			}
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg.IsCommand() {
		cmd := msg.Command()
		args := msg.CommandArguments()

		switch cmd {
		case "start":
			b.cmdStart(ctx, msg)
		case "upcoming_match", "upcoming-match", "upcoming":
			b.cmdUpcomingMatch(ctx, msg)
		case "matches":
			b.cmdMatches(ctx, msg, args)
		case "result", "leaderboard":
			b.cmdLeaderboard(ctx, msg)
		case "bets":
			b.cmdBets(ctx, msg)
		case "guess":
			b.cmdGuess(ctx, msg, msg.CommandArguments())
		case "clearbet":
			b.cmdClearBet(ctx, msg)
		case "changebet":
			b.cmdChangeBet(ctx, msg)
		case "reconcile":
			b.cmdReconcile(ctx, msg)
		default:
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Unknown command: /"+cmd)
			b.api.Send(reply)
		}
	}
}

func (b *Bot) handleCallbackQuery(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	b.handleBetCallback(ctx, cq)
}

// SendToGroup sends a message to the configured group chat
func (b *Bot) SendToGroup(text string) error {
	msg := tgbotapi.NewMessage(b.cfg.GroupChatID, text)
	msg.ParseMode = "HTML"

	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("Failed to send message to group: %v", err)
		return err
	}

	return nil
}

// SendMatchToGroup sends a match message with betting keyboard to the group
func (b *Bot) SendMatchToGroup(match *models.Match) error {
	msg := tgbotapi.NewMessage(b.cfg.GroupChatID, FormatMatchMessage(match, b.loc))
	msg.ReplyMarkup = MatchKeyboard(match)
	msg.ParseMode = "HTML"

	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("Failed to send match to group: %v", err)
		return err
	}

	return nil
}

// SendToChat sends a text message to a specific chat ID
func (b *Bot) SendToChat(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"

	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("Failed to send message to chat %d: %v", chatID, err)
		return err
	}

	return nil
}

// SendMatchToChat sends a match message with betting keyboard to a specific chat ID
func (b *Bot) SendMatchToChat(chatID int64, match *models.Match) error {
	msg := tgbotapi.NewMessage(chatID, FormatMatchMessage(match, b.loc))
	msg.ReplyMarkup = MatchKeyboard(match)
	msg.ParseMode = "HTML"

	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("Failed to send match to chat %d: %v", chatID, err)
		return err
	}

	return nil
}

// SendMatchToChatForGroup sends a match message to a group. If bets already exist
// for this match in the group, shows who picked what instead of the betting keyboard.
func (b *Bot) SendMatchToChatForGroup(chatID int64, match *models.Match) error {
	bets, err := b.db.GetBetsForMatchInGroup(match.ID, chatID)
	if err != nil {
		log.Printf("Failed to get bets for match %d in group %d: %v", match.ID, chatID, err)
	}

	if len(bets) == 0 {
		return b.SendMatchToChat(chatID, match)
	}

	// Build summary of who picked what
	text := FormatMatchMessage(match, b.loc)
	for _, bet := range bets {
		user, err := b.db.GetUserByID(bet.UserID)
		if err != nil || user == nil {
			continue
		}
		name := user.DisplayName
		if name == "" {
			name = user.Username
		}
		teamName := match.AwayTeam
		if bet.PickedTeam == "HOME_TEAM" {
			teamName = match.HomeTeam
		}
		text += "\n• " + name + " → " + teamName
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	_, err = b.api.Send(msg)
	if err != nil {
		log.Printf("Failed to send match bet summary to chat %d: %v", chatID, err)
		return err
	}
	return nil
}
