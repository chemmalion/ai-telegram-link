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
