package handler

import (
    "context"
    "fmt"
    "log"
    "strings"

    tg "github.com/go-telegram/bot"
    "github.com/go-telegram/bot/models"
    openai "github.com/sashabaranov/go-openai"

    "telegram-chatgpt-bot/internal/crypt"
    "telegram-chatgpt-bot/internal/storage"
)

var pendingAuth = map[int64]string{}

// HandleUpdate processes a Telegram update.
func HandleUpdate(ctx context.Context, b *tg.Bot, upd *models.Update) {
    if upd.Message == nil {
        return
    }
    msg := upd.Message
    chatID := msg.Chat.ID
    topicID := msg.MessageThreadID

    // Command handlers
    if cmd, args, ok := parseCommand(msg); ok {
        switch cmd {
        case "authproject":
            if args == "" {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Usage: /authproject <projectName>"})
                return
            }
            prompt := fmt.Sprintf("Please send me the OpenAI API key for project '%s' now.", args)
            b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: prompt})
            pendingAuth[msg.From.ID] = args
            return

        case "settopic":
            if topicID == 0 {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Must be called in a topic thread."})
                return
            }
            proj := args
            if proj == "" {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Usage: /settopic <projectName>"})
                return
            }
            if _, err := storage.LoadProject(proj); err != nil {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Project not found."})
                return
            }
            if err := storage.MapTopic(chatID, topicID, proj); err != nil {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Failed to map topic: " + err.Error()})
                return
            }
            b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Topic mapped to project '" + proj + "'."})
            return

        case "unsettopic":
            if topicID == 0 {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Must be in a topic thread."})
                return
            }
            if err := storage.UnmapTopic(chatID, topicID); err != nil {
                b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Failed to unmap: " + err.Error()})
                return
            }
            b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Topic unmapped."})
            return

        case "listprojects":
            projs, _ := storage.ListProjects()
            b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Projects: " + strings.Join(projs, ", ")})
            return
        }
    }

    if pendingProject, ok := pendingAuth[msg.From.ID]; ok && msg.Text != "" {
        key := strings.TrimSpace(msg.Text)
        enc, err := crypt.Encrypt(key)
        delete(pendingAuth, msg.From.ID)
        if err != nil {
            b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Encryption error: " + err.Error()})
            return
        }
        if err := storage.SaveProject(pendingProject, enc); err != nil {
            b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Save error: " + err.Error()})
            return
        }
        b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "Project '" + pendingProject + "' saved."})
        return
    }

    proj, err := storage.GetMappedProject(chatID, topicID)
    if err != nil {
        return
    }
    encKey, _ := storage.LoadProject(proj)
    apiKey, err := crypt.Decrypt(encKey)
    if err != nil {
        log.Println("decrypt error:", err)
        return
    }
    client := openai.NewClient(apiKey)
    resp, err := client.CreateChatCompletion(
        context.Background(),
        openai.ChatCompletionRequest{
            Model:    openai.GPT3Dot5Turbo,
            Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: msg.Text}},
        },
    )
    if err != nil {
        b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "OpenAI error: " + err.Error()})
        return
    }
    reply := resp.Choices[0].Message.Content
    b.SendMessage(ctx, &tg.SendMessageParams{
        ChatID:          chatID,
        MessageThreadID: topicID,
        Text:            reply,
        ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
    })
}

func parseCommand(msg *models.Message) (cmd, args string, ok bool) {
    if msg.Text == "" {
        return "", "", false
    }
    for _, e := range msg.Entities {
        if e.Type == models.MessageEntityTypeBotCommand && e.Offset == 0 {
            cmd = strings.TrimPrefix(msg.Text[:e.Length], "/")
            args = strings.TrimSpace(msg.Text[e.Length:])
            return cmd, args, true
        }
    }
    return "", "", false
}
