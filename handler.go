package main

import (
    "context"
    "fmt"
    "log"
    "strings"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    openai "github.com/sashabaranov/go-openai"
)

func handleUpdate(bot *tgbotapi.BotAPI, upd tgbotapi.Update) {
    if upd.Message == nil {
        return
    }
    msg := upd.Message
    chatID := msg.Chat.ID
    topicID := msg.MessageThreadID

    // Command handlers
    if msg.IsCommand() {
        switch msg.Command() {
        case "authproject":
            args := msg.CommandArguments()
            if args == "" {
                bot.Send(tgbotapi.NewMessage(chatID, "Usage: /authproject <projectName>"))
                return
            }
            // ask user for key
            prompt := tgbotapi.NewMessage(chatID, fmt.Sprintf("Please send me the OpenAI API key for project '%s' now.", args))
            bot.Send(prompt)
            // stash in-memory: next message from this user is key for this project
            pendingAuth[msg.From.ID] = args
            return

        case "settopic":
            if topicID == 0 {
                bot.Send(tgbotapi.NewMessage(chatID, "Must be called in a topic thread."))
                return
            }
            proj := msg.CommandArguments()
            if proj == "" {
                bot.Send(tgbotapi.NewMessage(chatID, "Usage: /settopic <projectName>"))
                return
            }
            if _, err := loadProject(proj); err != nil {
                bot.Send(tgbotapi.NewMessage(chatID, "Project not found."))
                return
            }
            if err := mapTopic(chatID, topicID, proj); err != nil {
                bot.Send(tgbotapi.NewMessage(chatID, "Failed to map topic: "+err.Error()))
                return
            }
            bot.Send(tgbotapi.NewMessage(chatID, "Topic mapped to project '"+proj+"'."))
            return

        case "unsettopic":
            if topicID == 0 {
                bot.Send(tgbotapi.NewMessage(chatID, "Must be in a topic thread."))
                return
            }
            if err := unmapTopic(chatID, topicID); err != nil {
                bot.Send(tgbotapi.NewMessage(chatID, "Failed to unmap: "+err.Error()))
                return
            }
            bot.Send(tgbotapi.NewMessage(chatID, "Topic unmapped."))
            return

        case "listprojects":
            projs, _ := listProjects()
            bot.Send(tgbotapi.NewMessage(chatID, "Projects: "+strings.Join(projs, ", ")))
            return
        }
    }

    // If waiting for API key response
    if pendingProject, ok := pendingAuth[msg.From.ID]; ok && msg.Text != "" {
        key := strings.TrimSpace(msg.Text)
        enc, err := encrypt(key)
        delete(pendingAuth, msg.From.ID)
        if err != nil {
            bot.Send(tgbotapi.NewMessage(chatID, "Encryption error: "+err.Error()))
            return
        }
        if err := saveProject(pendingProject, enc); err != nil {
            bot.Send(tgbotapi.NewMessage(chatID, "Save error: "+err.Error()))
            return
        }
        bot.Send(tgbotapi.NewMessage(chatID, "Project '"+pendingProject+"' saved."))
        return
    }

    // Regular message forwarding
    proj, err := getMappedProject(chatID, topicID)
    if err != nil {
        return
    }
    encKey, _ := loadProject(proj)
    apiKey, err := decrypt(encKey)
    if err != nil {
        log.Println("decrypt error:", err)
        return
    }
    client := openai.NewClient(apiKey)
    resp, err := client.CreateChatCompletion(
        context.Background(),
        openai.ChatCompletionRequest{
            Model: openai.GPT3Dot5Turbo,
            Messages: []openai.ChatCompletionMessage{
                {Role: openai.ChatMessageRoleUser, Content: msg.Text},
            },
        },
    )
    if err != nil {
        bot.Send(tgbotapi.NewMessage(chatID, "OpenAI error: "+err.Error()))
        return
    }
    reply := resp.Choices[0].Message.Content
    out := tgbotapi.NewMessage(chatID, reply)
    out.ReplyToMessageID = msg.MessageID
    out.MessageThreadID = topicID
    bot.Send(out)
}
