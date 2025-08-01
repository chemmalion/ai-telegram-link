package bot

import (
    "context"
    "log"
    "os"
    "os/signal"

    tg "github.com/go-telegram/bot"

    "telegram-chatgpt-bot/internal/crypt"
    "telegram-chatgpt-bot/internal/handler"
    "telegram-chatgpt-bot/internal/storage"
)

// Run starts the Telegram bot and listens for updates.
func Run() {
    // initialize cipher & storage
    crypt.Init()
    if err := storage.Init("bot.db"); err != nil {
        log.Fatal("storage init:", err)
    }

    // create Telegram API client
    botToken := os.Getenv("BOT_TOKEN")
    if botToken == "" {
        log.Fatal("BOT_TOKEN env var is required")
    }

    b, err := tg.New(botToken, tg.WithDefaultHandler(handler.HandleUpdate))
    if err != nil {
        log.Fatal("failed to create bot:", err)
    }

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    me, err := b.GetMe(ctx)
    if err != nil {
        log.Fatal("failed to get bot info:", err)
    }
    log.Printf("Bot started as @%s", me.Username)

    b.Start(ctx)
}
