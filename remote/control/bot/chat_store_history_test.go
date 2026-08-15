package bot_test

import (
	"context"
	"testing"

	"github.com/tingly-dev/tingly-box/remote/control/bot"
)

func TestListChatProjectPaths_FallbackToProjectPath(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	// Simulate a legacy chat written before ProjectHistory existed.
	if err := store.UpsertChat(context.Background(), &bot.Chat{
		ChatID:      "legacy",
		Platform:    "telegram",
		ProjectPath: "/legacy/path",
	}); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}
	got, err := store.ListChatProjectPaths(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("ListChatProjectPaths: %v", err)
	}
	if len(got) != 1 || got[0] != "/legacy/path" {
		t.Errorf("got %v, want [/legacy/path]", got)
	}
}

func TestBindProject_RecordsHistoryPerChat(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	if err := store.BindProject(context.Background(), "c1", "telegram", "/a", "alice"); err != nil {
		t.Fatalf("BindProject /a: %v", err)
	}
	if err := store.BindProject(context.Background(), "c1", "telegram", "/b", "alice"); err != nil {
		t.Fatalf("BindProject /b: %v", err)
	}
	if err := store.BindProject(context.Background(), "c1", "telegram", "/a", "alice"); err != nil {
		t.Fatalf("BindProject /a (re-bind): %v", err)
	}
	got, err := store.ListChatProjectPaths(context.Background(), "c1")
	if err != nil {
		t.Fatalf("ListChatProjectPaths: %v", err)
	}
	want := []string{"/a", "/b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
