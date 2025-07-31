package bot

import (
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

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
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal("failed to create bot:", err)
	}
	bot.Debug = false
	log.Printf("Bot started as @%s", bot.Self.UserName)

	// start receiving updates
	updates := bot.GetUpdatesChan(tgbotapi.NewUpdate(0))
	for upd := range updates {
		go handler.HandleUpdate(bot, upd)
	}
}
