package handler

import (
	"reflect"
	"testing"
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
