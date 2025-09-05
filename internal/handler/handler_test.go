package handler

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"telegram-chatgpt-bot/internal/logging"
	"telegram-chatgpt-bot/internal/storage"
)

func TestChatName(t *testing.T) {
	in := " Hello, World! @2024??"
	got := chatName(in)
	want := "Hello__World___2024__"
	if got != want {
		t.Fatalf("chatName(%q) = %q, want %q", in, got, want)
	}
}

func TestSplitMessage(t *testing.T) {
	parts := splitMessage("abcdef", 2)
	expected := []string{"ab", "cd", "ef"}
	if !reflect.DeepEqual(parts, expected) {
		t.Fatalf("splitMessage got %v want %v", parts, expected)
	}
}

func TestParseCommand(t *testing.T) {
	msg := &models.Message{
		Text: "/newproject proj",
		Entities: []models.MessageEntity{{
			Type:   models.MessageEntityTypeBotCommand,
			Offset: 0,
			Length: len("/newproject"),
		}},
	}
	cmd, args, ok := parseCommand(msg)
	if !ok || cmd != "newproject" || args != "proj" {
		t.Fatalf("parseCommand = %q %q %v", cmd, args, ok)
	}
}

type fakeBot struct{ sent []string }

func (f *fakeBot) SendMessage(ctx context.Context, params *tg.SendMessageParams) (*models.Message, error) {
	f.sent = append(f.sent, params.Text)
	return &models.Message{ID: 1}, nil
}

func (f *fakeBot) GetFile(ctx context.Context, params *tg.GetFileParams) (*models.File, error) {
	return &models.File{}, nil
}

func (f *fakeBot) FileDownloadLink(file *models.File) string { return "" }

func (f *fakeBot) EditMessageText(ctx context.Context, params *tg.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: params.MessageID}, nil
}

func cmdUpdate(text string) *models.Update {
	parts := strings.SplitN(text, " ", 2)
	cmdLen := len(parts[0])
	return &models.Update{Message: &models.Message{
		Text: text,
		Entities: []models.MessageEntity{{
			Type:   models.MessageEntityTypeBotCommand,
			Offset: 0,
			Length: cmdLen,
		}},
		Chat: models.Chat{ID: 1},
		From: &models.User{ID: 1},
	}}
}

func initStore(t *testing.T) {
	dir := t.TempDir()
	if err := storage.Init(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("storage init: %v", err)
	}
	t.Cleanup(func() { storage.Close() })
}

func TestHandleUpdate_IgnoresCallback(t *testing.T) {
	b := &fakeBot{}
	upd := &models.Update{CallbackQuery: &models.CallbackQuery{}, Message: &models.Message{Text: "hi"}}
	HandleUpdate(context.Background(), b, upd)
	if len(b.sent) != 0 {
		t.Fatalf("expected no messages, got %v", b.sent)
	}
}

func TestHandleUpdate_NoMessage(t *testing.T) {
	b := &fakeBot{}
	upd := &models.Update{}
	HandleUpdate(context.Background(), b, upd)
	if len(b.sent) != 0 {
		t.Fatalf("expected no messages, got %v", b.sent)
	}
}

func TestHandleUpdate_AllowedUsers(t *testing.T) {
	logging.Init()
	allowedUsers = map[int64]bool{1: true}
	t.Cleanup(func() { allowedUsers = nil })

	t.Run("unauthorized", func(t *testing.T) {
		b := &fakeBot{}
		called := false
		orig := saveProject
		saveProject = func(name string) error { called = true; return nil }
		defer func() { saveProject = orig }()
		upd := &models.Update{Message: &models.Message{
			Text:     "/newproject demo",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/newproject")}},
			Chat:     models.Chat{ID: 1},
			From:     &models.User{ID: 2},
		}}
		HandleUpdate(context.Background(), b, upd)
		if called {
			t.Fatal("saveProject should not be called")
		}
		if len(b.sent) != 1 || !strings.Contains(b.sent[0], "configured to work only") {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("authorized", func(t *testing.T) {
		b := &fakeBot{}
		called := false
		orig := saveProject
		saveProject = func(name string) error { called = true; return nil }
		defer func() { saveProject = orig }()
		upd := &models.Update{Message: &models.Message{
			Text:     "/newproject ok",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/newproject")}},
			Chat:     models.Chat{ID: 1},
			From:     &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if !called {
			t.Fatal("saveProject should be called")
		}
		if len(b.sent) != 1 || b.sent[0] != "Project 'ok' registered." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateNewProject_MissingName(t *testing.T) {
	logging.Init()
	b := &fakeBot{}
	upd := &models.Update{Message: &models.Message{
		Text:     "/newproject",
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/newproject")}},
		Chat:     models.Chat{ID: 1},
		From:     &models.User{ID: 1},
	}}
	HandleUpdate(context.Background(), b, upd)
	if len(b.sent) != 1 || b.sent[0] != "Usage: /newproject <projectName>" {
		t.Fatalf("unexpected messages: %v", b.sent)
	}
}

func TestHandleUpdateNewProject_SaveError(t *testing.T) {
	logging.Init()
	b := &fakeBot{}
	orig := saveProject
	saveProject = func(name string) error { return fmt.Errorf("boom") }
	defer func() { saveProject = orig }()
	upd := &models.Update{Message: &models.Message{
		Text:     "/newproject demo",
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/newproject")}},
		Chat:     models.Chat{ID: 1},
		From:     &models.User{ID: 1},
	}}
	HandleUpdate(context.Background(), b, upd)
	if len(b.sent) != 1 || !strings.Contains(b.sent[0], "Save failed: boom") {
		t.Fatalf("unexpected messages: %v", b.sent)
	}
}

func TestHandleUpdateNewProject_Success(t *testing.T) {
	logging.Init()
	dir := t.TempDir()
	if err := storage.Init(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("storage init: %v", err)
	}
	b := &fakeBot{}
	upd := &models.Update{Message: &models.Message{
		Text: "/newproject demo",
		Entities: []models.MessageEntity{{
			Type:   models.MessageEntityTypeBotCommand,
			Offset: 0,
			Length: len("/newproject"),
		}},
		Chat: models.Chat{ID: 1},
		From: &models.User{ID: 42},
	}}
	HandleUpdate(context.Background(), b, upd)
	if len(b.sent) != 1 || b.sent[0] != "Project 'demo' registered." {
		t.Fatalf("unexpected messages: %v", b.sent)
	}
	exists, err := storage.ProjectExists("demo")
	if err != nil || !exists {
		t.Fatalf("project not saved: %v %v", exists, err)
	}
}

func TestHandleUpdateSetTopic(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := &models.Update{Message: &models.Message{
			Text:     "/settopic",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/settopic")}},
			Chat:     models.Chat{ID: 1},
			From:     &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /settopic <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		b := &fakeBot{}
		origPE := projectExists
		projectExists = func(name string) (bool, error) { return false, nil }
		defer func() { projectExists = origPE }()
		upd := &models.Update{Message: &models.Message{
			Text:     "/settopic demo",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/settopic")}},
			Chat:     models.Chat{ID: 1},
			From:     &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("map error", func(t *testing.T) {
		b := &fakeBot{}
		origPE := projectExists
		origMT := mapTopic
		projectExists = func(name string) (bool, error) { return true, nil }
		mapTopic = func(chatID int64, topicID int, project string) error { return fmt.Errorf("boom") }
		defer func() { projectExists = origPE; mapTopic = origMT }()
		upd := &models.Update{Message: &models.Message{
			Text:     "/settopic demo",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/settopic")}},
			Chat:     models.Chat{ID: 1},
			From:     &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || !strings.Contains(b.sent[0], "Failed to map topic: boom") {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		b := &fakeBot{}
		origPE := projectExists
		origMT := mapTopic
		projectExists = func(name string) (bool, error) { return true, nil }
		mapTopic = func(chatID int64, topicID int, project string) error { return nil }
		defer func() { projectExists = origPE; mapTopic = origMT }()
		upd := &models.Update{Message: &models.Message{
			Text:            "/settopic demo",
			Entities:        []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/settopic")}},
			Chat:            models.Chat{ID: 1},
			MessageThreadID: 2,
			From:            &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Topic mapped to project 'demo'." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateUnsetTopic(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := &models.Update{Message: &models.Message{
			Text:     "/unsettopic",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/unsettopic")}},
			Chat:     models.Chat{ID: 1},
			From:     &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Must be in a topic thread." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("unmap error", func(t *testing.T) {
		b := &fakeBot{}
		orig := unmapTopic
		unmapTopic = func(chatID int64, topicID int) error { return fmt.Errorf("boom") }
		defer func() { unmapTopic = orig }()
		upd := &models.Update{Message: &models.Message{
			Text:            "/unsettopic",
			Entities:        []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/unsettopic")}},
			Chat:            models.Chat{ID: 1},
			MessageThreadID: 2,
			From:            &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || !strings.Contains(b.sent[0], "Failed to unmap: boom") {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		b := &fakeBot{}
		orig := unmapTopic
		unmapTopic = func(chatID int64, topicID int) error { return nil }
		defer func() { unmapTopic = orig }()
		upd := &models.Update{Message: &models.Message{
			Text:            "/unsettopic",
			Entities:        []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/unsettopic")}},
			Chat:            models.Chat{ID: 1},
			MessageThreadID: 2,
			From:            &models.User{ID: 1},
		}}
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Topic unmapped." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateSetModel(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/setmodel")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /setmodel <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/setmodel demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		pendingModel = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/setmodel demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingModel[1] != "demo" {
			t.Fatalf("pendingModel not set: %v", pendingModel)
		}
		if len(b.sent) != 1 || b.sent[0] != "Enter model name" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateSetRule(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/setrule")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /setrule <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/setrule demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		pendingRule = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/setrule demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingRule[1] != "demo" {
			t.Fatalf("pendingRule not set: %v", pendingRule)
		}
		if len(b.sent) != 1 || b.sent[0] != "Enter your custom instruction" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateShowRule(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/showrule")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /showrule <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("no rule", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		b := &fakeBot{}
		upd := cmdUpdate("/showrule demo")
		HandleUpdate(context.Background(), b, upd)
		want := "No instruction set for project 'demo'."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("rule present", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		if err := storage.SaveProjectInstruction("demo", "be nice"); err != nil {
			t.Fatalf("save rule: %v", err)
		}
		b := &fakeBot{}
		upd := cmdUpdate("/showrule demo")
		HandleUpdate(context.Background(), b, upd)
		want := "Instruction for project 'demo':\nbe nice"
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateWebSearch(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/websearch")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /websearch <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/websearch demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		b := &fakeBot{}
		upd := cmdUpdate("/websearch demo")
		HandleUpdate(context.Background(), b, upd)
		want := "Web search for project 'demo' is off."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateSetWebSearch(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/setwebsearch")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /setwebsearch <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/setwebsearch demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		pendingWebSearch = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/setwebsearch demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingWebSearch[1] != "demo" {
			t.Fatalf("pendingWebSearch not set: %v", pendingWebSearch)
		}
		want := "Enter web search setting (high, medium, low, off)."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateReasoning(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/reasoning")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /reasoning <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/reasoning demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		b := &fakeBot{}
		upd := cmdUpdate("/reasoning demo")
		HandleUpdate(context.Background(), b, upd)
		want := "Reasoning effort for project 'demo' is medium."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateSetReasoning(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/setreasoning")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /setreasoning <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/setreasoning demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		pendingReasoning = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/setreasoning demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingReasoning[1] != "demo" {
			t.Fatalf("pendingReasoning not set: %v", pendingReasoning)
		}
		want := "Enter reasoning effort (minimal, low, medium, high)."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateTranscribe(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/transcribe")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /transcribe <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/transcribe demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		b := &fakeBot{}
		upd := cmdUpdate("/transcribe demo")
		HandleUpdate(context.Background(), b, upd)
		want := "Audio transcription for project 'demo' is off."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateSetTranscribe(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/settranscribe")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /settranscribe <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/settranscribe demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		pendingTranscribe = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/settranscribe demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingTranscribe[1] != "demo" {
			t.Fatalf("pendingTranscribe not set: %v", pendingTranscribe)
		}
		want := "Enable audio transcription? (on, off)"
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateHistory(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/history")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /history <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/history demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		if err := storage.SaveHistoryLimit("demo", 5); err != nil {
			t.Fatalf("save limit: %v", err)
		}
		storage.AddHistoryMessage("demo", storage.HistoryMessage{When: 1, Content: "hi"})
		storage.AddHistoryMessage("demo", storage.HistoryMessage{When: 2, Content: "there"})
		b := &fakeBot{}
		upd := cmdUpdate("/history demo")
		HandleUpdate(context.Background(), b, upd)
		want := "For project 'demo' history limit is 5 and there are 2 stored messages."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateHistoryMessages(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/historymessages")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /historymessages <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/historymessages demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("no messages", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		b := &fakeBot{}
		upd := cmdUpdate("/historymessages demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "No stored messages." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("some messages", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		storage.AddHistoryMessage("demo", storage.HistoryMessage{When: 0, WhoName: "Alice", Content: "hello"})
		storage.AddHistoryMessage("demo", storage.HistoryMessage{When: 1, WhoName: "Bob", Content: "world"})
		b := &fakeBot{}
		upd := cmdUpdate("/historymessages demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || !strings.Contains(b.sent[0], "Alice") || !strings.Contains(b.sent[0], "Bob") {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateSetHistoryLimit(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/sethistorylimit")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /sethistorylimit <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/sethistorylimit demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		pendingHistLimit = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/sethistorylimit demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingHistLimit[1] != "demo" {
			t.Fatalf("pendingHistLimit not set: %v", pendingHistLimit)
		}
		want := "Enter new history limit (0 to disable)."
		if len(b.sent) != 1 || b.sent[0] != want {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateClearHistory(t *testing.T) {
	logging.Init()
	t.Run("usage", func(t *testing.T) {
		b := &fakeBot{}
		upd := cmdUpdate("/clearhistory")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Usage: /clearhistory <projectName>" {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		initStore(t)
		b := &fakeBot{}
		upd := cmdUpdate("/clearhistory demo")
		HandleUpdate(context.Background(), b, upd)
		if len(b.sent) != 1 || b.sent[0] != "Project not found." {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})

	t.Run("success", func(t *testing.T) {
		initStore(t)
		if err := storage.SaveProject("demo"); err != nil {
			t.Fatalf("save project: %v", err)
		}
		storage.AddHistoryMessage("demo", storage.HistoryMessage{When: 1, Content: "hi"})
		storage.AddHistoryMessage("demo", storage.HistoryMessage{When: 2, Content: "there"})
		pendingClearHist = map[int64]string{}
		b := &fakeBot{}
		upd := cmdUpdate("/clearhistory demo")
		HandleUpdate(context.Background(), b, upd)
		if pendingClearHist[1] != "demo" {
			t.Fatalf("pendingClearHist not set: %v", pendingClearHist)
		}
		if len(b.sent) != 1 || !strings.Contains(b.sent[0], "The 2 messages will be removed") {
			t.Fatalf("unexpected messages: %v", b.sent)
		}
	})
}

func TestHandleUpdateListProjects(t *testing.T) {
	logging.Init()
	initStore(t)
	if err := storage.SaveProject("a"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	if err := storage.SaveProject("b"); err != nil {
		t.Fatalf("save project: %v", err)
	}
	b := &fakeBot{}
	upd := cmdUpdate("/listprojects")
	HandleUpdate(context.Background(), b, upd)
	if len(b.sent) != 1 || b.sent[0] != "Projects: a, b" {
		t.Fatalf("unexpected messages: %v", b.sent)
	}
}
