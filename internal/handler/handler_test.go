package handler

import (
	"context"
	"path/filepath"
	"reflect"
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

func TestHandleUpdateNewProject(t *testing.T) {
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
