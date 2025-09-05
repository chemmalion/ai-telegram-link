package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	openai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/responses"

	"telegram-chatgpt-bot/internal/logging"
	"telegram-chatgpt-bot/internal/storage"
)

// testBot allows customizing bot behaviour for tests.
type testBot struct {
	sent       []string
	sentParams []tg.SendMessageParams
	edits      []tg.EditMessageTextParams
	getFile    func(ctx context.Context, params *tg.GetFileParams) (*models.File, error)
	fileLink   func(file *models.File) string
	edit       func(ctx context.Context, params *tg.EditMessageTextParams) (*models.Message, error)
}

func (b *testBot) SendMessage(ctx context.Context, params *tg.SendMessageParams) (*models.Message, error) {
	b.sent = append(b.sent, params.Text)
	b.sentParams = append(b.sentParams, *params)
	id := 1
	if params.ReplyParameters != nil {
		id = params.ReplyParameters.MessageID + 1
	}
	return &models.Message{ID: id}, nil
}

func (b *testBot) GetFile(ctx context.Context, params *tg.GetFileParams) (*models.File, error) {
	if b.getFile != nil {
		return b.getFile(ctx, params)
	}
	return &models.File{FilePath: "file"}, nil
}

func (b *testBot) FileDownloadLink(file *models.File) string {
	if b.fileLink != nil {
		return b.fileLink(file)
	}
	return "http://example.com/file"
}

func (b *testBot) EditMessageText(ctx context.Context, params *tg.EditMessageTextParams) (*models.Message, error) {
	b.edits = append(b.edits, *params)
	if b.edit != nil {
		return b.edit(ctx, params)
	}
	return &models.Message{ID: params.MessageID}, nil
}

// helper to initialize storage
func initStore2(t *testing.T) {
	dir := t.TempDir()
	if err := storage.Init(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("storage init: %v", err)
	}
	t.Cleanup(func() { storage.Close() })
}

func TestHandleUpdate_EmptyMessageNoMedia(t *testing.T) {
	logging.Init()
	b := &testBot{}
	called := false
	origResp := openAIResponses
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		called = true
		return "", nil
	}
	defer func() { openAIResponses = origResp }()

	upd := &models.Update{Message: &models.Message{Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	HandleUpdate(context.Background(), b, upd)
	if called {
		t.Fatal("openAIResponses should not be called")
	}
	if len(b.sent) != 0 {
		t.Fatalf("unexpected messages: %v", b.sent)
	}
}

func TestHandleUpdate_ProjectRetrievalFails(t *testing.T) {
	logging.Init()
	initStore2(t)
	b := &testBot{}
	called := false
	origResp := openAIResponses
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		called = true
		return "", nil
	}
	defer func() { openAIResponses = origResp }()

	upd := &models.Update{Message: &models.Message{Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	HandleUpdate(context.Background(), b, upd)
	if called {
		t.Fatal("openAIResponses should not be called")
	}
	if len(b.sent) != 0 {
		t.Fatalf("unexpected messages: %v", b.sent)
	}
}

func TestHandleUpdate_DefaultModel(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}

	var model string
	origNew := newOpenAIClient
	origResp := openAIResponses
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		model = string(params.Model)
		return "ok", nil
	}
	defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

	upd := &models.Update{Message: &models.Message{Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	HandleUpdate(context.Background(), &testBot{}, upd)
	if model != defaultModel {
		t.Fatalf("model = %s, want %s", model, defaultModel)
	}
}

func TestHandleUpdate_SystemInstruction(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}
	if err := storage.SaveProjectInstruction("demo", "sys"); err != nil {
		t.Fatalf("save instruction: %v", err)
	}

	var paramsCapture responses.ResponseNewParams
	origNew := newOpenAIClient
	origResp := openAIResponses
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		paramsCapture = params
		return "ok", nil
	}
	defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

	upd := &models.Update{Message: &models.Message{Text: "hello", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	HandleUpdate(context.Background(), &testBot{}, upd)

	inputs := paramsCapture.Input.OfInputItemList
	if len(inputs) < 2 {
		t.Fatalf("expected at least 2 inputs, got %d", len(inputs))
	}
	sys := inputs[0].OfMessage
	if sys == nil || sys.Role != responses.EasyInputMessageRoleSystem {
		t.Fatalf("first input not system: %+v", sys)
	}
	if txt := sys.Content.OfString.Value; txt != "sys" {
		t.Fatalf("system text = %q", txt)
	}
	user := inputs[1].OfMessage
	if user == nil || user.Role != responses.EasyInputMessageRoleUser {
		t.Fatalf("second input not user: %+v", user)
	}
	cont := user.Content.OfInputItemContentList
	if len(cont) == 0 || cont[0].OfInputText.Text != "hello" {
		t.Fatalf("user text missing: %v", cont)
	}
}

func TestHandleUpdate_AudioTranscription(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}
	if err := storage.SaveProjectTranscribe("demo", "on"); err != nil {
		t.Fatalf("save transcribe: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		var transcribed bool
		var paramsCapture responses.ResponseNewParams
		origNew := newOpenAIClient
		origResp := openAIResponses
		origTrans := openAITranscribe
		origHTTP := httpGetFunc
		newOpenAIClient = func() *openai.Client { return &openai.Client{} }
		openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
			paramsCapture = params
			return "ok", nil
		}
		openAITranscribe = func(client *openai.Client, r io.Reader) (string, error) {
			transcribed = true
			return "voice text", nil
		}
		httpGetFunc = func(url string) (*http.Response, error) {
			return &http.Response{Body: io.NopCloser(strings.NewReader("audio"))}, nil
		}
		defer func() {
			newOpenAIClient = origNew
			openAIResponses = origResp
			openAITranscribe = origTrans
			httpGetFunc = origHTTP
		}()

		upd := &models.Update{Message: &models.Message{
			Text:  "hello",
			Voice: &models.Voice{FileID: "v1"},
			Chat:  models.Chat{ID: 1},
			From:  &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), &testBot{}, upd)
		if !transcribed {
			t.Fatal("transcription not called")
		}
		user := paramsCapture.Input.OfInputItemList[len(paramsCapture.Input.OfInputItemList)-1].OfMessage
		cont := user.Content.OfInputItemContentList
		if len(cont) < 2 {
			t.Fatalf("expected transcription in content, got %v", cont)
		}
		if txt := cont[1].OfInputText.Text; !strings.Contains(txt, "voice text") {
			t.Fatalf("transcription text missing: %q", txt)
		}
	})

	t.Run("download error", func(t *testing.T) {
		var transcribed bool
		origNew := newOpenAIClient
		origResp := openAIResponses
		origTrans := openAITranscribe
		origHTTP := httpGetFunc
		newOpenAIClient = func() *openai.Client { return &openai.Client{} }
		openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
			return "ok", nil
		}
		openAITranscribe = func(client *openai.Client, r io.Reader) (string, error) {
			transcribed = true
			return "voice text", nil
		}
		httpGetFunc = func(url string) (*http.Response, error) {
			return nil, io.EOF
		}
		defer func() {
			newOpenAIClient = origNew
			openAIResponses = origResp
			openAITranscribe = origTrans
			httpGetFunc = origHTTP
		}()

		upd := &models.Update{Message: &models.Message{
			Text:  "hello",
			Voice: &models.Voice{FileID: "v1"},
			Chat:  models.Chat{ID: 1},
			From:  &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), &testBot{}, upd)
		if transcribed {
			t.Fatal("transcription should not be called on download error")
		}
	})
}

func TestHandleUpdate_PhotoAttachment(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		var paramsCapture responses.ResponseNewParams
		origNew := newOpenAIClient
		origResp := openAIResponses
		newOpenAIClient = func() *openai.Client { return &openai.Client{} }
		openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
			paramsCapture = params
			return "ok", nil
		}
		defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

		b := &testBot{}
		upd := &models.Update{Message: &models.Message{
			Photo: []models.PhotoSize{{FileID: "p1"}},
			Chat:  models.Chat{ID: 1},
			From:  &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		user := paramsCapture.Input.OfInputItemList[len(paramsCapture.Input.OfInputItemList)-1].OfMessage
		cont := user.Content.OfInputItemContentList
		hasImg := false
		for _, c := range cont {
			if c.OfInputImage != nil {
				hasImg = true
			}
		}
		if !hasImg {
			t.Fatal("expected image content")
		}
	})

	t.Run("getfile error", func(t *testing.T) {
		var paramsCapture responses.ResponseNewParams
		origNew := newOpenAIClient
		origResp := openAIResponses
		newOpenAIClient = func() *openai.Client { return &openai.Client{} }
		openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
			paramsCapture = params
			return "ok", nil
		}
		defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

		b := &testBot{
			getFile: func(ctx context.Context, params *tg.GetFileParams) (*models.File, error) {
				return nil, io.EOF
			},
		}
		upd := &models.Update{Message: &models.Message{
			Photo: []models.PhotoSize{{FileID: "p1"}},
			Chat:  models.Chat{ID: 1},
			From:  &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		user := paramsCapture.Input.OfInputItemList[len(paramsCapture.Input.OfInputItemList)-1].OfMessage
		cont := user.Content.OfInputItemContentList
		for _, c := range cont {
			if c.OfInputImage != nil {
				t.Fatal("image should not be included on error")
			}
		}
	})
}

func TestHandleUpdate_HistoryRecording(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}
	if err := storage.SaveHistoryLimit("demo", 10); err != nil {
		t.Fatalf("save history limit: %v", err)
	}
	if err := storage.SaveProjectTranscribe("demo", "on"); err != nil {
		t.Fatalf("save transcribe: %v", err)
	}

	origNew := newOpenAIClient
	origResp := openAIResponses
	origTrans := openAITranscribe
	origHTTP := httpGetFunc
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		return "reply", nil
	}
	openAITranscribe = func(client *openai.Client, r io.Reader) (string, error) {
		return "voice text", nil
	}
	httpGetFunc = func(url string) (*http.Response, error) {
		return &http.Response{Body: io.NopCloser(strings.NewReader("audio"))}, nil
	}
	defer func() {
		newOpenAIClient = origNew
		openAIResponses = origResp
		openAITranscribe = origTrans
		httpGetFunc = origHTTP
	}()

	upd := &models.Update{Message: &models.Message{
		Text:  "hi",
		Voice: &models.Voice{FileID: "v"},
		Photo: []models.PhotoSize{{FileID: "p"}},
		Chat:  models.Chat{ID: 1},
		From:  &models.User{ID: 1},
	}}
	HandleUpdate(context.Background(), &testBot{}, upd)
	hist, err := storage.LoadProjectHistory("demo")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist) != 4 {
		t.Fatalf("expected 4 history messages, got %d", len(hist))
	}
}

func TestHandleUpdate_HistoryTrim(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}
	if err := storage.SaveHistoryLimit("demo", 3); err != nil {
		t.Fatalf("save history limit: %v", err)
	}

	origNew := newOpenAIClient
	origResp := openAIResponses
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	count := 0
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		count++
		return "r" + strconv.Itoa(count), nil
	}
	defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

	b := &testBot{}
	upd1 := &models.Update{Message: &models.Message{Text: "first", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	HandleUpdate(context.Background(), b, upd1)
	upd2 := &models.Update{Message: &models.Message{Text: "second", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	HandleUpdate(context.Background(), b, upd2)
	hist, err := storage.LoadProjectHistory("demo")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("expected 3 messages after trim, got %d", len(hist))
	}
	if hist[0].Content == "first" {
		t.Fatalf("oldest message was not trimmed: %v", hist)
	}
}

func TestChatGPTRequest_ProgressTickerSuccess(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}

	origNew := newOpenAIClient
	origResp := openAIResponses
	origTicker := newTicker
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		time.Sleep(5 * time.Millisecond)
		return "final reply", nil
	}
	newTicker = func(d time.Duration) *time.Ticker { return time.NewTicker(1 * time.Millisecond) }
	defer func() { newOpenAIClient = origNew; openAIResponses = origResp; newTicker = origTicker }()

	upd := &models.Update{Message: &models.Message{ID: 10, Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	b := &testBot{}
	HandleUpdate(context.Background(), b, upd)

	if len(b.sent) == 0 || b.sent[0] != "Sending to ChatGPT..." {
		t.Fatalf("progress message not sent: %v", b.sent)
	}
	if len(b.edits) < 2 {
		t.Fatalf("expected at least 2 edits, got %d", len(b.edits))
	}
	if !strings.HasPrefix(b.edits[0].Text, "Waiting") {
		t.Fatalf("first edit = %q, want waiting message", b.edits[0].Text)
	}
	if got := b.edits[len(b.edits)-1].Text; got != "final reply" {
		t.Fatalf("final edit = %q, want %q", got, "final reply")
	}
	wantID := upd.Message.ID + 1
	for i, e := range b.edits {
		if e.MessageID != wantID {
			t.Fatalf("edit %d used id %d want %d", i, e.MessageID, wantID)
		}
	}
}

func TestChatGPTRequest_ErrorHandling(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}
	if err := storage.SaveHistoryLimit("demo", 5); err != nil {
		t.Fatalf("save history limit: %v", err)
	}

	origNew := newOpenAIClient
	origResp := openAIResponses
	origTicker := newTicker
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		return "", fmt.Errorf("boom")
	}
	newTicker = func(d time.Duration) *time.Ticker { return time.NewTicker(time.Hour) }
	defer func() { newOpenAIClient = origNew; openAIResponses = origResp; newTicker = origTicker }()

	upd := &models.Update{Message: &models.Message{ID: 5, Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	b := &testBot{}
	HandleUpdate(context.Background(), b, upd)

	if len(b.sent) != 1 || b.sent[0] != "Sending to ChatGPT..." {
		t.Fatalf("sent messages: %v", b.sent)
	}
	if len(b.edits) != 1 || b.edits[0].Text != "OpenAI error: boom" {
		t.Fatalf("edits: %v", b.edits)
	}
	hist, err := storage.LoadProjectHistory("demo")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist) != 2 || hist[1].Content != "OpenAI error: boom" {
		t.Fatalf("history: %v", hist)
	}
}

func TestChatGPTRequest_WebSearchTool(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}

	cases := []struct {
		setting  string
		wantTool bool
		size     responses.WebSearchToolSearchContextSize
	}{
		{"off", false, responses.WebSearchToolSearchContextSizeHigh},
		{"low", true, responses.WebSearchToolSearchContextSizeLow},
		{"medium", true, responses.WebSearchToolSearchContextSizeMedium},
		{"high", true, responses.WebSearchToolSearchContextSizeHigh},
		{"other", true, responses.WebSearchToolSearchContextSizeHigh},
	}

	for _, tc := range cases {
		t.Run(tc.setting, func(t *testing.T) {
			if err := storage.SaveProjectWebSearch("demo", tc.setting); err != nil {
				t.Fatalf("save web search: %v", err)
			}
			origNew := newOpenAIClient
			origResp := openAIResponses
			newOpenAIClient = func() *openai.Client { return &openai.Client{} }
			var paramsCap responses.ResponseNewParams
			openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
				paramsCap = params
				return "ok", nil
			}
			defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

			upd := &models.Update{Message: &models.Message{ID: 1, Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
			HandleUpdate(context.Background(), &testBot{}, upd)
			if tc.wantTool {
				if len(paramsCap.Tools) != 1 {
					t.Fatalf("expected one tool, got %v", paramsCap.Tools)
				}
				tool := paramsCap.Tools[0].OfWebSearchPreview
				if tool == nil || tool.SearchContextSize != tc.size {
					t.Fatalf("tool size = %v, want %v", tool, tc.size)
				}
			} else if len(paramsCap.Tools) != 0 {
				t.Fatalf("expected no tools, got %v", paramsCap.Tools)
			}
		})
	}
}

func TestChatGPTRequest_ReasoningEffort(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}

	cases := []struct {
		effort string
		want   openai.ReasoningEffort
	}{
		{"minimal", openai.ReasoningEffortMinimal},
		{"low", openai.ReasoningEffortLow},
		{"high", openai.ReasoningEffortHigh},
		{"", openai.ReasoningEffortMedium},
	}

	for _, tc := range cases {
		t.Run(tc.effort, func(t *testing.T) {
			if err := storage.SaveProjectReasoning("demo", tc.effort); err != nil {
				t.Fatalf("save reasoning: %v", err)
			}
			origNew := newOpenAIClient
			origResp := openAIResponses
			newOpenAIClient = func() *openai.Client { return &openai.Client{} }
			var paramsCap responses.ResponseNewParams
			openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
				paramsCap = params
				return "ok", nil
			}
			defer func() { newOpenAIClient = origNew; openAIResponses = origResp }()

			upd := &models.Update{Message: &models.Message{ID: 2, Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
			HandleUpdate(context.Background(), &testBot{}, upd)
			if paramsCap.Reasoning.Effort != tc.want {
				t.Fatalf("effort = %v, want %v", paramsCap.Reasoning.Effort, tc.want)
			}
		})
	}
}

func TestResponseHandling_LongMessageSplit(t *testing.T) {
	logging.Init()
	initStore2(t)
	chatGPTKey = "x"
	if err := storage.SaveProject("demo"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.MapTopic(1, 0, "demo"); err != nil {
		t.Fatalf("map topic: %v", err)
	}
	if err := storage.SaveHistoryLimit("demo", 5); err != nil {
		t.Fatalf("save history: %v", err)
	}

	longReply := strings.Repeat("a", 9001)

	origNew := newOpenAIClient
	origResp := openAIResponses
	origTicker := newTicker
	newOpenAIClient = func() *openai.Client { return &openai.Client{} }
	openAIResponses = func(client *openai.Client, params responses.ResponseNewParams) (string, error) {
		return longReply, nil
	}
	newTicker = func(d time.Duration) *time.Ticker { return time.NewTicker(time.Hour) }
	defer func() { newOpenAIClient = origNew; openAIResponses = origResp; newTicker = origTicker }()

	upd := &models.Update{Message: &models.Message{ID: 3, Text: "hi", Chat: models.Chat{ID: 1}, From: &models.User{ID: 1}}}
	b := &testBot{}
	HandleUpdate(context.Background(), b, upd)

	if len(b.sent) != 3 {
		t.Fatalf("sent = %v", b.sent)
	}
	if b.sent[0] != "Sending to ChatGPT..." {
		t.Fatalf("first sent = %q", b.sent[0])
	}
	if len(b.edits) != 1 || b.edits[0].Text != longReply[:4000] {
		t.Fatalf("edits = %v", b.edits)
	}
	if b.sent[1] != longReply[4000:8000] || b.sent[2] != longReply[8000:] {
		t.Fatalf("chunk send mismatch")
	}
	if b.sentParams[1].ReplyParameters == nil || b.sentParams[1].ReplyParameters.MessageID != b.edits[0].MessageID {
		t.Fatalf("second chunk reply id = %v", b.sentParams[1].ReplyParameters)
	}
	if b.sentParams[2].ReplyParameters == nil || b.sentParams[2].ReplyParameters.MessageID != b.edits[0].MessageID+1 {
		t.Fatalf("third chunk reply id = %v", b.sentParams[2].ReplyParameters)
	}
	hist, err := storage.LoadProjectHistory("demo")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist) != 2 || hist[1].Content != longReply {
		t.Fatalf("history: %v", hist)
	}
}
