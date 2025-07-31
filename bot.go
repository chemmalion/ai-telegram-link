// bot.go
package main

import (
    "log"
    "os"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func runBot() {
    // initialize cipher & storage
    initCipher()
    if err := initStorage("bot.db"); err != nil {
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
        go handleUpdate(bot, upd)
    }
}
