package handler

import (
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	openai "github.com/sashabaranov/go-openai"

	"telegram-chatgpt-bot/internal/crypt"
	"telegram-chatgpt-bot/internal/storage"
)

var pendingAuth = map[int64]string{}

// HandleUpdate processes a Telegram update.
func HandleUpdate(bot *tgbotapi.BotAPI, upd tgbotapi.Update) {
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
			prompt := tgbotapi.NewMessage(chatID, fmt.Sprintf("Please send me the OpenAI API key for project '%s' now.", args))
			bot.Send(prompt)
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
			if _, err := storage.LoadProject(proj); err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Project not found."))
				return
			}
			if err := storage.MapTopic(chatID, topicID, proj); err != nil {
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
			if err := storage.UnmapTopic(chatID, topicID); err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Failed to unmap: "+err.Error()))
				return
			}
			bot.Send(tgbotapi.NewMessage(chatID, "Topic unmapped."))
			return

		case "listprojects":
			projs, _ := storage.ListProjects()
			bot.Send(tgbotapi.NewMessage(chatID, "Projects: "+strings.Join(projs, ", ")))
			return
		}
	}

	if pendingProject, ok := pendingAuth[msg.From.ID]; ok && msg.Text != "" {
		key := strings.TrimSpace(msg.Text)
		enc, err := crypt.Encrypt(key)
		delete(pendingAuth, msg.From.ID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Encryption error: "+err.Error()))
			return
		}
		if err := storage.SaveProject(pendingProject, enc); err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Save error: "+err.Error()))
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "Project '"+pendingProject+"' saved."))
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
