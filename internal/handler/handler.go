package handler

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	openai "github.com/sashabaranov/go-openai"

	"telegram-chatgpt-bot/internal/logging"
	"telegram-chatgpt-bot/internal/storage"
)

const (
	defaultModel  = "gpt-5"
	fallbackModel = openai.GPT3Dot5Turbo
)

var (
	pendingRule  = map[int64]string{}
	allowedUsers map[int64]bool
	chatGPTKey   string
)

// Init parses the allowed user ids from the environment.
func Init() {
	parseAllowedUsers()
	chatGPTKey = os.Getenv("TBOT_CHATGPT_KEY")
	if chatGPTKey == "" {
		logging.Log.Fatal().Msg("TBOT_CHATGPT_KEY env var is required")
	}
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

	if cq := upd.CallbackQuery; cq != nil {
		ctx = logging.WithUser(ctx, cq.From.ID)
		if strings.HasPrefix(cq.Data, "setmodel:") {
			parts := strings.SplitN(cq.Data, ":", 3)
			if len(parts) == 3 {
				proj, model := parts[1], parts[2]
				if err := storage.SaveProjectModel(proj, model); err != nil {
					b.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{CallbackQueryID: cq.ID, Text: "Save failed"})
				} else {
					b.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{CallbackQueryID: cq.ID, Text: "Model set"})
					if cq.Message.Message != nil {
						b.SendMessage(ctx, &tg.SendMessageParams{
							ChatID:          cq.Message.Message.Chat.ID,
							MessageThreadID: cq.Message.Message.MessageThreadID,
							Text:            fmt.Sprintf("Project '%s' uses model '%s'.", proj, model),
						})
					}
					logging.Ctx(ctx).Info().Str("event", "set_model").Str("project", proj).Str("model", model).Msg("model selected")
				}
			}
		}
		return
	}

	if upd.Message == nil {
		return
	}
	msg := upd.Message
	chatID := msg.Chat.ID
	topicID := msg.MessageThreadID
	if msg.From != nil {
		ctx = logging.WithUser(ctx, msg.From.ID)
	}
	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	log := logging.Ctx(ctx)
	log.Info().Str("event", "telegram_request").Int64("chat_id", chatID).Int("topic_id", int(topicID)).Str("snippet", logging.Snippet(text, 30)).Msg("incoming message")

	if len(allowedUsers) > 0 {
		if msg.From == nil || !allowedUsers[msg.From.ID] {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "This bot is configured to work only with specific users in Telegram. But the bot source is open so that you can setup your own bot."})
			return
		}
	}

	// Command handlers
	if cmd, args, ok := parseCommand(msg); ok {
		switch cmd {
		case "newproject":
			if args == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /newproject <projectName>"})
				return
			}
			if err := storage.SaveProject(args); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save failed: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project '" + args + "' registered."})
			log.Info().Str("event", "new_project").Str("project", args).Msg("project registered")
			return

		case "settopic":
			if topicID == 0 {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Must be called in a topic thread."})
				return
			}
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /settopic <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			if err := storage.MapTopic(chatID, topicID, proj); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Failed to map topic: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Topic mapped to project '" + proj + "'."})
			log.Info().Str("event", "map_topic").Int64("chat_id", chatID).Int("topic_id", int(topicID)).Str("project", proj).Msg("topic mapped")
			return

		case "unsettopic":
			if topicID == 0 {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Must be in a topic thread."})
				return
			}
			if err := storage.UnmapTopic(chatID, topicID); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Failed to unmap: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Topic unmapped."})
			log.Info().Str("event", "unmap_topic").Int64("chat_id", chatID).Int("topic_id", int(topicID)).Msg("topic unmapped")
			return

		case "setmodel":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /setmodel <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			client := openai.NewClient(chatGPTKey)
			modelsList, err := client.ListModels(context.Background())
			var names []string
			if err != nil {
				names = []string{defaultModel, fallbackModel}
			} else {
				for _, m := range modelsList.Models {
					if strings.Contains(m.ID, "gpt") {
						names = append(names, m.ID)
					}
				}
				if len(names) == 0 {
					names = []string{defaultModel, fallbackModel}
				}
			}
			buttons := make([][]models.InlineKeyboardButton, len(names))
			for i, n := range names {
				buttons[i] = []models.InlineKeyboardButton{{Text: n, CallbackData: fmt.Sprintf("setmodel:%s:%s", proj, n)}}
			}
			b.SendMessage(ctx, &tg.SendMessageParams{
				ChatID:          chatID,
				MessageThreadID: topicID,
				Text:            fmt.Sprintf("Select model for project '%s':", proj),
				ReplyMarkup: &models.InlineKeyboardMarkup{
					InlineKeyboard: buttons,
				},
			})
			return

		case "setrule":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /setrule <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			pendingRule[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Enter your custom instruction"})
			log.Info().Str("event", "rule_request").Str("project", proj).Msg("rule requested")
			return

		case "showrule":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /showrule <projectName>"})
				return
			}
			instr, err := storage.LoadProjectInstruction(proj)
			if err != nil || instr == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("No instruction set for project '%s'.", proj)})
			} else {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Instruction for project '%s':\n%s", proj, instr)})
			}
			return

		case "listprojects":
			projs, _ := storage.ListProjects()
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Projects: " + strings.Join(projs, ", ")})
			return
		}
	}

	if proj, ok := pendingRule[msg.From.ID]; ok && msg.Text != "" {
		instr := strings.TrimSpace(msg.Text)
		delete(pendingRule, msg.From.ID)
		if err := storage.SaveProjectInstruction(proj, instr); err != nil {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
			return
		}
		b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Instruction saved."})
		log.Info().Str("event", "set_rule").Str("project", proj).Msg("instruction saved")
		return
	}

	proj, err := storage.GetMappedProject(chatID, topicID)
	if err != nil {
		return
	}
	model, err := storage.LoadProjectModel(proj)
	if err != nil || model == "" {
		model = defaultModel
	}
	instr, _ := storage.LoadProjectInstruction(proj)
	messages := []openai.ChatCompletionMessage{}
	if instr != "" {
		messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: instr})
	}
	var parts []openai.ChatMessagePart
	if text != "" {
		parts = append(parts, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeText, Text: text})
	}
	if len(msg.Photo) > 0 {
		fileID := msg.Photo[len(msg.Photo)-1].FileID
		file, err := b.GetFile(ctx, &tg.GetFileParams{FileID: fileID})
		if err != nil {
			log.Error().Err(err).Msg("failed to get file")
		} else {
			url := b.FileDownloadLink(file)
			parts = append(parts, openai.ChatMessagePart{
				Type:     openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{URL: url},
			})
		}
	}
	if len(parts) == 0 {
		return
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, MultiContent: parts})
	client := openai.NewClient(chatGPTKey)
	log.Info().Str("event", "chatgpt_request").Str("project", proj).Str("model", model).Str("snippet", logging.Snippet(text, 30)).Msg("sending to ChatGPT")

	// send initial progress message and keep its ID for further edits
	progressMsg, _ := b.SendMessage(ctx, &tg.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: topicID,
		Text:            "Sending to ChatGPT...",
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	})

	type gptResult struct {
		reply string
		err   error
	}
	resultCh := make(chan gptResult, 1)

	// run ChatGPT request asynchronously
	go func() {
		resp, err := client.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model:    model,
				Messages: messages,
			},
		)
		if err != nil && model == defaultModel {
			resp, err = client.CreateChatCompletion(
				context.Background(),
				openai.ChatCompletionRequest{
					Model:    fallbackModel,
					Messages: messages,
				},
			)
		}
		if err != nil {
			resultCh <- gptResult{reply: "OpenAI error: " + err.Error(), err: err}
			return
		}
		resultCh <- gptResult{reply: resp.Choices[0].Message.Content}
	}()

	ticker := time.NewTicker(10 * time.Second)
	start := time.Now()
	var res gptResult
	for {
		select {
		case res = <-resultCh:
			ticker.Stop()
			goto done
		case <-ticker.C:
			elapsed := int(time.Since(start).Seconds())
			_, err := b.EditMessageText(ctx, &tg.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: progressMsg.ID,
				Text:      fmt.Sprintf("Waiting %d seconds for ChatGPT answer...", elapsed),
			})
			if err != nil {
				log.Error().Err(err).Msg("failed to edit progress message")
			}
		}
	}

done:
	if res.err != nil {
		b.EditMessageText(ctx, &tg.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: progressMsg.ID,
			Text:      res.reply,
		})
		log.Error().Err(res.err).Msg("chatgpt request failed")
		return
	}

	reply := res.reply
	log.Info().Str("event", "chatgpt_response").Str("project", proj).Str("snippet", logging.Snippet(reply, 30)).Msg("received from ChatGPT")

	const maxMessageLen = 4000
	chunks := splitMessage(reply, maxMessageLen)
	if len(chunks) == 0 {
		return
	}
	b.EditMessageText(ctx, &tg.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: progressMsg.ID,
		Text:      chunks[0],
	})
	for _, chunk := range chunks[1:] {
		b.SendMessage(ctx, &tg.SendMessageParams{
			ChatID:          chatID,
			MessageThreadID: topicID,
			Text:            chunk,
		})
	}
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

func splitMessage(text string, size int) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	var chunks []string
	for len(runes) > 0 {
		end := size
		if len(runes) < end {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}
