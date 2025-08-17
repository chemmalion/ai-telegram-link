package bot

import (
	"context"
	"os"
	"os/signal"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"telegram-chatgpt-bot/internal/crypt"
	"telegram-chatgpt-bot/internal/handler"
	"telegram-chatgpt-bot/internal/logging"
	"telegram-chatgpt-bot/internal/storage"
)

// Run starts the Telegram bot and listens for updates.
func Run() {
	logging.Init()
	handler.Init()
	logging.Log.Info().Msg("starting bot")

	// initialize cipher & storage
	crypt.Init()
	if err := storage.Init("bot.db"); err != nil {
		logging.Log.Fatal().Err(err).Msg("storage init")
	}

	// create Telegram API client
	botToken := os.Getenv("TBOT_TELEGRAM_KEY")
	if botToken == "" {
		logging.Log.Fatal().Msg("TBOT_TELEGRAM_KEY env var is required")
	}

	b, err := tg.New(botToken, tg.WithDefaultHandler(func(ctx context.Context, b *tg.Bot, upd *models.Update) {
		handler.HandleUpdate(ctx, b, upd)
	}))
	if err != nil {
		logging.Log.Fatal().Err(err).Msg("failed to create bot")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	me, err := b.GetMe(ctx)
	if err != nil {
		logging.Log.Fatal().Err(err).Msg("failed to get bot info")
	}
	logging.Log.Info().Str("event", "bot_start").Str("username", me.Username).Msg("bot started")

	b.Start(ctx)
}
