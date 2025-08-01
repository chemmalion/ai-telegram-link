package handler

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	openai "github.com/sashabaranov/go-openai"

	"telegram-chatgpt-bot/internal/crypt"
	"telegram-chatgpt-bot/internal/logging"
	"telegram-chatgpt-bot/internal/storage"
)

var (
	pendingAuth  = map[int64]string{}
	allowedUsers map[int64]bool
)

// Init parses the allowed user ids from the environment.
func Init() {
	parseAllowedUsers()
}

func parseAllowedUsers() {
	idsEnv := os.Getenv("TBOT_ALLOWED_USER_IDS")
	if idsEnv == "" {
		return
	}
	allowedUsers = make(map[int64]bool)
	for _, p := range strings.Split(idsEnv, ",") {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			logging.Log.Warn().Str("user_id", s).Msg("invalid user id in TBOT_ALLOWED_USER_IDS")
			continue
		}
		allowedUsers[id] = true
	}
}

// HandleUpdate processes a Telegram update.
func HandleUpdate(ctx context.Context, b *tg.Bot, upd *models.Update) {
	ctx = logging.Context(ctx)
	if upd.Message == nil {
		return
	}
	msg := upd.Message
	chatID := msg.Chat.ID
	topicID := msg.MessageThreadID
	if msg.From != nil {
		ctx = logging.WithUser(ctx, msg.From.ID)
	}
	log := logging.Ctx(ctx)
	log.Info().Str("event", "telegram_request").Int64("chat_id", chatID).Int("topic_id", int(topicID)).Str("snippet", logging.Snippet(msg.Text, 30)).Msg("incoming message")

	if len(allowedUsers) > 0 {
		if msg.From == nil || !allowedUsers[msg.From.ID] {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "This bot is configured to work only with specific users in Telegram. But the bot source is open so that you can setup your own bot."})
			return
		}
	}

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
			log.Info().Str("event", "authorization_request").Str("project", args).Msg("authorization requested")
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
			log.Info().Str("event", "map_topic").Int64("chat_id", chatID).Int("topic_id", int(topicID)).Str("project", proj).Msg("topic mapped")
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
			log.Info().Str("event", "unmap_topic").Int64("chat_id", chatID).Int("topic_id", int(topicID)).Msg("topic unmapped")
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
		log.Info().Str("event", "authorization_attempt").Str("project", pendingProject).Msg("api key saved")
		return
	}

	proj, err := storage.GetMappedProject(chatID, topicID)
	if err != nil {
		return
	}
	encKey, _ := storage.LoadProject(proj)
	apiKey, err := crypt.Decrypt(encKey)
	if err != nil {
		logging.Ctx(ctx).Error().Err(err).Msg("decrypt error")
		return
	}
	client := openai.NewClient(apiKey)
	log.Info().Str("event", "chatgpt_request").Str("project", proj).Str("snippet", logging.Snippet(msg.Text, 30)).Msg("sending to ChatGPT")
	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:    openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: msg.Text}},
		},
	)
	if err != nil {
		b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, Text: "OpenAI error: " + err.Error()})
		log.Error().Err(err).Msg("chatgpt request failed")
		return
	}
	reply := resp.Choices[0].Message.Content
	log.Info().Str("event", "chatgpt_response").Str("project", proj).Str("snippet", logging.Snippet(reply, 30)).Msg("received from ChatGPT")
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
