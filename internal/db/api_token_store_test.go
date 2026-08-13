package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func setupTestAPITokenStore(t *testing.T) (*APITokenStore, string) {
	t.Helper()

	tmpDir := t.TempDir()

	store, err := NewAPITokenStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create API token store: %v", err)
	}

	return store, tmpDir
}

func TestAPITokenStore_GetToken_NotFound(t *testing.T) {
	store, _ := setupTestAPITokenStore(t)
	defer store.Close()

	_, err := store.GetToken("non-existent-token")
	assert.Error(t, err)
}

func TestAPITokenStore_RevokeToken_NotFound(t *testing.T) {
	store, _ := setupTestAPITokenStore(t)
	defer store.Close()

	err := store.RevokeToken("non-existent", "reason")
	assert.Error(t, err)
}

func TestAPITokenStore_UpdateLastUsed_FirstCallPersists(t *testing.T) {
	store, _ := setupTestAPITokenStore(t)
	defer store.Close()

	_, err := store.CreateTokenWithTokenID("user-1", "token-1", "display", "admin", nil)
	assert.NoError(t, err)

	assert.NoError(t, store.UpdateLastUsed("token-1"))

	cached, err := store.GetToken("token-1")
	assert.NoError(t, err)
	assert.NotNil(t, cached.LastUsedAt)

	// Confirm it was actually persisted to SQLite, not just cached.
	var dbRecord APITokenRecord
	assert.NoError(t, store.db.Where("token_id = ?", "token-1").First(&dbRecord).Error)
	assert.NotNil(t, dbRecord.LastUsedAt)
}

func TestAPITokenStore_UpdateLastUsed_DebouncesWithinWindow(t *testing.T) {
	store, _ := setupTestAPITokenStore(t)
	defer store.Close()

	_, err := store.CreateTokenWithTokenID("user-1", "token-1", "display", "admin", nil)
	assert.NoError(t, err)

	assert.NoError(t, store.UpdateLastUsed("token-1"))
	first, err := store.GetToken("token-1")
	assert.NoError(t, err)

	// A second call inside the debounce window must not move last_used_at.
	assert.NoError(t, store.UpdateLastUsed("token-1"))
	second, err := store.GetToken("token-1")
	assert.NoError(t, err)

	assert.True(t, first.LastUsedAt.Equal(*second.LastUsedAt))
}

func TestAPITokenStore_UpdateLastUsed_WritesAgainAfterWindow(t *testing.T) {
	store, _ := setupTestAPITokenStore(t)
	defer store.Close()

	_, err := store.CreateTokenWithTokenID("user-1", "token-1", "display", "admin", nil)
	assert.NoError(t, err)
	assert.NoError(t, store.UpdateLastUsed("token-1"))

	// Simulate the debounce window having elapsed without sleeping in the test.
	store.mu.Lock()
	stale := time.Now().Add(-2 * defaultLastUsedDebounce)
	store.cache["token-1"].LastUsedAt = &stale
	store.mu.Unlock()

	assert.NoError(t, store.UpdateLastUsed("token-1"))
	refreshed, err := store.GetToken("token-1")
	assert.NoError(t, err)
	assert.True(t, refreshed.LastUsedAt.After(stale))
}
