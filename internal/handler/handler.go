package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	openai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/responses"
	"github.com/openai/openai-go/v2/shared/constant"

	"telegram-chatgpt-bot/internal/logging"
	"telegram-chatgpt-bot/internal/storage"
)

const (
	defaultModel = "gpt-5"
)

var (
	pendingRule       = map[int64]string{}
	pendingModel      = map[int64]string{}
	pendingHistLimit  = map[int64]string{}
	pendingClearHist  = map[int64]string{}
	pendingWebSearch  = map[int64]string{}
	pendingReasoning  = map[int64]string{}
	pendingTranscribe = map[int64]string{}
	allowedUsers      map[int64]bool
	chatGPTKey        string

	// wrappers around storage functions for easier testing
	saveProject            = storage.SaveProject
	projectExists          = storage.ProjectExists
	mapTopic               = storage.MapTopic
	unmapTopic             = storage.UnmapTopic
	saveProjectModel       = storage.SaveProjectModel
	saveProjectInstruction = storage.SaveProjectInstruction
	saveProjectWebSearch   = storage.SaveProjectWebSearch
	saveProjectReasoning   = storage.SaveProjectReasoning
	saveProjectTranscribe  = storage.SaveProjectTranscribe
	saveHistoryLimit       = storage.SaveHistoryLimit
	clearProjectHistory    = storage.ClearProjectHistory

	// wrappers around OpenAI functions for easier testing
	newOpenAIClient = func() *openai.Client {
		c := openai.NewClient(option.WithAPIKey(chatGPTKey))
		return &c
	}
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		resp, err := client.Responses.New(context.Background(), params)
		if err != nil {
			return "", err
		}
		return resp.OutputText(), nil
	}
	openAITranscribe = func(client *openai.Client, r io.Reader) (string, error) {
		tResp, err := client.Audio.Transcriptions.New(context.Background(), openai.AudioTranscriptionNewParams{
			File:  r,
			Model: openai.AudioModelWhisper1,
		})
		if err != nil {
			return "", err
		}
		return tResp.Text, nil
	}
	httpGetFunc = http.Get
	newTicker   = time.NewTicker
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

// Bot wraps the telegram bot methods used by the handler.
type Bot interface {
	SendMessage(ctx context.Context, params *tg.SendMessageParams) (*models.Message, error)
	GetFile(ctx context.Context, params *tg.GetFileParams) (*models.File, error)
	FileDownloadLink(file *models.File) string
	EditMessageText(ctx context.Context, params *tg.EditMessageTextParams) (*models.Message, error)
}

// HandleUpdate processes a Telegram update.
func HandleUpdate(ctx context.Context, b Bot, upd *models.Update) {
	ctx = logging.Context(ctx)

	if upd.CallbackQuery != nil {
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
			if err := saveProject(args); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save failed: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project '" + args + "' registered."})
			log.Info().Str("event", "new_project").Str("project", args).Msg("project registered")
			return

		case "settopic":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /settopic <projectName>"})
				return
			}
			if exists, err := projectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			if err := mapTopic(chatID, topicID, proj); err != nil {
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
			if err := unmapTopic(chatID, topicID); err != nil {
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
			pendingModel[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Enter model name"})
			log.Info().Str("event", "model_request").Str("project", proj).Msg("model requested")
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

		case "websearch":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /websearch <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			setting, _ := storage.LoadProjectWebSearch(proj)
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Web search for project '%s' is %s.", proj, setting)})
			return

		case "setwebsearch":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /setwebsearch <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			pendingWebSearch[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Enter web search setting (high, medium, low, off)."})
			log.Info().Str("event", "websearch_request").Str("project", proj).Msg("websearch requested")
			return

		case "reasoning":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /reasoning <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			effort, _ := storage.LoadProjectReasoning(proj)
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Reasoning effort for project '%s' is %s.", proj, effort)})
			return

		case "setreasoning":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /setreasoning <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			pendingReasoning[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Enter reasoning effort (minimal, low, medium, high)."})
			log.Info().Str("event", "reasoning_request").Str("project", proj).Msg("reasoning requested")
			return

		case "transcribe":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /transcribe <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			setting, _ := storage.LoadProjectTranscribe(proj)
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Audio transcription for project '%s' is %s.", proj, setting)})
			return

		case "settranscribe":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /settranscribe <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			pendingTranscribe[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Enable audio transcription? (on, off)"})
			log.Info().Str("event", "transcribe_request").Str("project", proj).Msg("transcribe requested")
			return

		case "history":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /history <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			limit, _ := storage.LoadHistoryLimit(proj)
			count, _ := storage.CountProjectHistory(proj)
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("For project '%s' history limit is %d and there are %d stored messages.", proj, limit, count)})
			return

		case "historymessages":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /historymessages <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			hist, err := storage.LoadProjectHistory(proj)
			if err != nil || len(hist) == 0 {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "No stored messages."})
				return
			}
			var sb strings.Builder
			for _, h := range hist {
				when := time.Unix(h.When, 0).Format("15:04:05 02.01.2006")
				snippet := []rune(h.Content)
				if len(snippet) > 30 {
					snippet = snippet[:30]
				}
				fmt.Fprintf(&sb, "%s %s:\n%s\n\n", when, h.WhoName, string(snippet))
			}
			out := strings.TrimSpace(sb.String())
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: out})
			return

		case "sethistorylimit":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /sethistorylimit <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			pendingHistLimit[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Enter new history limit (0 to disable)."})
			log.Info().Str("event", "history_limit_request").Str("project", proj).Msg("history limit requested")
			return

		case "clearhistory":
			proj := args
			if proj == "" {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Usage: /clearhistory <projectName>"})
				return
			}
			if exists, err := storage.ProjectExists(proj); err != nil || !exists {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Project not found."})
				return
			}
			count, _ := storage.CountProjectHistory(proj)
			pendingClearHist[msg.From.ID] = proj
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("The %d messages will be removed from the '%s' project. Please type the word 'confirm' to continue.", count, proj)})
			log.Info().Str("event", "clear_history_request").Str("project", proj).Int("count", count).Msg("clear history requested")
			return

		case "listprojects":
			projs, _ := storage.ListProjects()
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Projects: " + strings.Join(projs, ", ")})
			return
		}
	}

	if proj, ok := pendingModel[msg.From.ID]; ok && msg.Text != "" {
		model := strings.TrimSpace(msg.Text)
		delete(pendingModel, msg.From.ID)
		if err := saveProjectModel(proj, model); err != nil {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
			return
		}
		b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Project '%s' uses model '%s'.", proj, model)})
		log.Info().Str("event", "set_model").Str("project", proj).Str("model", model).Msg("model set")
		return
	}

	if proj, ok := pendingRule[msg.From.ID]; ok && msg.Text != "" {
		instr := strings.TrimSpace(msg.Text)
		delete(pendingRule, msg.From.ID)
		if err := saveProjectInstruction(proj, instr); err != nil {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
			return
		}
		b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Instruction saved."})
		log.Info().Str("event", "set_rule").Str("project", proj).Msg("instruction saved")
		return
	}

	if proj, ok := pendingWebSearch[msg.From.ID]; ok && msg.Text != "" {
		val := strings.ToLower(strings.TrimSpace(msg.Text))
		delete(pendingWebSearch, msg.From.ID)
		switch val {
		case "high", "medium", "low", "off":
			if err := saveProjectWebSearch(proj, val); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Web search for project '%s' set to %s.", proj, val)})
			log.Info().Str("event", "set_websearch").Str("project", proj).Str("setting", val).Msg("websearch set")
		default:
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Please enter one of: high, medium, low, off."})
		}
		return
	}

	if proj, ok := pendingReasoning[msg.From.ID]; ok && msg.Text != "" {
		val := strings.ToLower(strings.TrimSpace(msg.Text))
		delete(pendingReasoning, msg.From.ID)
		switch val {
		case "minimal", "low", "medium", "high":
			if err := saveProjectReasoning(proj, val); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Reasoning effort for project '%s' set to %s.", proj, val)})
			log.Info().Str("event", "set_reasoning").Str("project", proj).Str("effort", val).Msg("reasoning set")
		default:
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Please enter one of: minimal, low, medium, high."})
		}
		return
	}

	if proj, ok := pendingTranscribe[msg.From.ID]; ok && msg.Text != "" {
		val := strings.ToLower(strings.TrimSpace(msg.Text))
		delete(pendingTranscribe, msg.From.ID)
		switch val {
		case "on", "off":
			if err := saveProjectTranscribe(proj, val); err != nil {
				b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
				return
			}
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Audio transcription for project '%s' set to %s.", proj, val)})
			log.Info().Str("event", "set_transcribe").Str("project", proj).Str("setting", val).Msg("transcribe set")
		default:
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Please enter one of: on, off."})
		}
		return
	}

	if proj, ok := pendingHistLimit[msg.From.ID]; ok && msg.Text != "" {
		limitStr := strings.TrimSpace(msg.Text)
		delete(pendingHistLimit, msg.From.ID)
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 0 {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Please enter a non-negative integer."})
			return
		}
		if err := saveHistoryLimit(proj, limit); err != nil {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Save error: " + err.Error()})
			return
		}
		b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("History limit for project '%s' set to %d.", proj, limit)})
		log.Info().Str("event", "set_history_limit").Str("project", proj).Int("limit", limit).Msg("history limit set")
		return
	}

	if proj, ok := pendingClearHist[msg.From.ID]; ok && msg.Text != "" {
		resp := strings.ToLower(strings.TrimSpace(msg.Text))
		delete(pendingClearHist, msg.From.ID)
		if resp != "confirm" {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Cancelled."})
			return
		}
		removed, err := clearProjectHistory(proj)
		if err != nil {
			b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: "Clear error: " + err.Error()})
			return
		}
		b.SendMessage(ctx, &tg.SendMessageParams{ChatID: chatID, MessageThreadID: topicID, Text: fmt.Sprintf("Cleared %d messages from project '%s'.", removed, proj)})
		log.Info().Str("event", "clear_history").Str("project", proj).Int("removed", removed).Msg("history cleared")
		return
	}

	if text == "" && len(msg.Photo) == 0 && msg.Voice == nil && msg.Audio == nil {
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
	inputs := responses.ResponseInputParam{}
	if instr != "" {
		inputs = append(inputs, responses.ResponseInputItemParamOfMessage(instr, responses.EasyInputMessageRoleSystem))
	}
	limit, _ := storage.LoadHistoryLimit(proj)
	hist, _ := storage.LoadProjectHistory(proj)
	webSearchSetting, _ := storage.LoadProjectWebSearch(proj)
	reasoningEffort, _ := storage.LoadProjectReasoning(proj)
	transcribeSetting, _ := storage.LoadProjectTranscribe(proj)
	client := newOpenAIClient()
	if limit > 0 && len(hist) > 0 {
		for _, h := range hist {
			if h.Content == "" {
				continue
			}
			when := time.Unix(h.When, 0).Format("2006-01-02 15:04:05")
			prefix := fmt.Sprintf("%s %s:\n", when, h.WhoName)
			inputs = append(inputs, responses.ResponseInputItemParamOfMessage(prefix+h.Content, responses.EasyInputMessageRole(h.Role)))
		}
	}
	userName := msg.From.Username
	if userName == "" {
		userName = msg.From.FirstName
	}
	now := time.Now()
	var transcribed string
	if transcribeSetting == "on" && (msg.Voice != nil || msg.Audio != nil) {
		fileID := ""
		if msg.Voice != nil {
			fileID = msg.Voice.FileID
		} else if msg.Audio != nil {
			fileID = msg.Audio.FileID
		}
		file, err := b.GetFile(ctx, &tg.GetFileParams{FileID: fileID})
		if err != nil {
			log.Error().Err(err).Msg("failed to get audio file")
		} else {
			url := b.FileDownloadLink(file)
			resp, err := httpGetFunc(url)
			if err == nil {
				defer resp.Body.Close()
				tText, err := openAITranscribe(client, resp.Body)
				if err != nil {
					log.Error().Err(err).Msg("transcription failed")
				} else {
					transcribed = tText
				}
			} else {
				log.Error().Err(err).Msg("failed to download audio")
			}
		}
	}
	var parts responses.ResponseInputMessageContentListParam
	if limit > 0 {
		meta := fmt.Sprintf("%s %s:", now.Format("2006-01-02 15:04:05"), userName)
		if text != "" {
			meta += "\n" + text
		}
		if transcribed != "" {
			meta += "\n(Audio transcription)\n" + transcribed
		}
		parts = append(parts, responses.ResponseInputContentParamOfInputText(meta))
	} else {
		if text != "" {
			parts = append(parts, responses.ResponseInputContentParamOfInputText(text))
		}
		if transcribed != "" {
			parts = append(parts, responses.ResponseInputContentParamOfInputText("(Audio transcription)\n"+transcribed))
		}
	}
	if len(msg.Photo) > 0 {
		fileID := msg.Photo[len(msg.Photo)-1].FileID
		file, err := b.GetFile(ctx, &tg.GetFileParams{FileID: fileID})
		if err != nil {
			log.Error().Err(err).Msg("failed to get file")
		} else {
			url := b.FileDownloadLink(file)
			img := responses.ResponseInputImageParam{
				Detail:   responses.ResponseInputImageDetailAuto,
				ImageURL: openai.String(url),
			}
			parts = append(parts, responses.ResponseInputContentUnionParam{OfInputImage: &img})
		}
	}
	inputs = append(inputs, responses.ResponseInputItemParamOfMessage(parts, responses.EasyInputMessageRoleUser))
	if limit > 0 {
		whenUnix := now.Unix()
		if text != "" {
			storage.AddHistoryMessage(proj, storage.HistoryMessage{
				Role:    string(responses.EasyInputMessageRoleUser),
				WhoID:   msg.From.ID,
				WhoName: userName,
				When:    whenUnix,
				Content: text,
			})
		}
		if transcribed != "" {
			storage.AddHistoryMessage(proj, storage.HistoryMessage{
				Role:    string(responses.EasyInputMessageRoleUser),
				WhoID:   msg.From.ID,
				WhoName: userName,
				When:    whenUnix,
				Content: "(Transcribed audio) " + transcribed,
			})
		}
		if len(msg.Photo) > 0 {
			storage.AddHistoryMessage(proj, storage.HistoryMessage{
				Role:    string(responses.EasyInputMessageRoleUser),
				WhoID:   msg.From.ID,
				WhoName: userName,
				When:    whenUnix,
				Content: "(User has attached some image)",
			})
		}
	}
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
		var tools []responses.ToolUnionParam
		if webSearchSetting != "off" {
			size := responses.WebSearchToolSearchContextSizeHigh
			switch webSearchSetting {
			case "medium":
				size = responses.WebSearchToolSearchContextSizeMedium
			case "low":
				size = responses.WebSearchToolSearchContextSizeLow
			case "high":
				size = responses.WebSearchToolSearchContextSizeHigh
			}
			tools = append(tools, responses.ToolUnionParam{
				OfWebSearchPreview: &responses.WebSearchToolParam{
					Type:              responses.WebSearchToolTypeWebSearchPreview,
					SearchContextSize: size,
					UserLocation: responses.WebSearchToolUserLocationParam{
						City:     param.NewOpt("Oulu"),
						Country:  param.NewOpt("FI"),
						Timezone: param.NewOpt("Europe/Helsinki"),
						Type:     constant.ValueOf[constant.Approximate](),
					},
				},
			})
		}
		params := responses.ResponseNewParams{
			Model:     openai.ResponsesModel(model),
			Input:     responses.ResponseNewParamsInputUnion{OfInputItemList: inputs},
			Tools:     tools,
			Reasoning: openai.ReasoningParam{Effort: reasoningEffortToConst(reasoningEffort)},
		}
		reply, err := openAIResponses(client, params)
		if err != nil {
			resultCh <- gptResult{reply: "OpenAI error: " + err.Error(), err: err}
			return
		}
		resultCh <- gptResult{reply: reply}
	}()

	ticker := newTicker(10 * time.Second)
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
		if limit > 0 {
			storage.AddHistoryMessage(proj, storage.HistoryMessage{
				Role:    string(responses.EasyInputMessageRoleAssistant),
				WhoID:   0,
				WhoName: "ChatGPT " + model,
				When:    time.Now().Unix(),
				Content: res.reply,
			})
			storage.TrimProjectHistory(proj, limit)
		}
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
	if limit > 0 {
		storage.AddHistoryMessage(proj, storage.HistoryMessage{
			Role:    string(responses.EasyInputMessageRoleAssistant),
			WhoID:   0,
			WhoName: "ChatGPT " + model,
			When:    time.Now().Unix(),
			Content: reply,
		})
		storage.TrimProjectHistory(proj, limit)
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

func chatName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func reasoningEffortToConst(val string) openai.ReasoningEffort {
	switch val {
	case "minimal":
		return openai.ReasoningEffortMinimal
	case "low":
		return openai.ReasoningEffortLow
	case "high":
		return openai.ReasoningEffortHigh
	default:
		return openai.ReasoningEffortMedium
	}
}
