package db

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tingly-dev/tingly-box/remote/access"
)

// queryCounter is a gorm logger that counts the statements a call issues, so
// a test can assert the query count rather than trusting that a refactor kept
// it flat.
type queryCounter struct {
	mu         sync.Mutex
	statements []string
}

func (c *queryCounter) LogMode(logger.LogLevel) logger.Interface      { return c }
func (c *queryCounter) Info(context.Context, string, ...interface{})  {}
func (c *queryCounter) Warn(context.Context, string, ...interface{})  {}
func (c *queryCounter) Error(context.Context, string, ...interface{}) {}

func (c *queryCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, sql)
}

func (c *queryCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = nil
}

func (c *queryCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.statements)
}

func (c *queryCounter) dump() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.statements, "\n  ")
}

// setupGroupWithActors builds a group carrying n actors and returns a store
// whose queries are counted.
func setupGroupWithActors(t *testing.T, n int) (*BotAccessStore, *queryCounter, string, string) {
	t.Helper()

	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sm.Close() })

	bot := createAccessBot(t, sm, "grp")
	store := sm.BotAccess()
	ctx := context.Background()

	group, err := store.DiscoverGroup(ctx, bot.UUID, "telegram", "external-group", "Group")
	require.NoError(t, err)

	for i := 0; i < n; i++ {
		_, err := store.AddGroupActor(ctx, bot.UUID, group.ID,
			"actor-"+string(rune('a'+i)), "Actor", "label")
		require.NoError(t, err)
	}

	// Re-wrap the shared handle with a counting logger. Same connection, so
	// the data above is visible; only logging differs.
	counter := &queryCounter{}
	counted := &BotAccessStore{
		db:        sm.db.Session(&gorm.Session{Logger: counter}),
		transport: store.transport,
	}
	return counted, counter, bot.UUID, group.ID
}

// TestListGroupActorsIsNotNPlusOne pins the query count flat as the group
// grows. The previous shape issued one actor lookup plus one permission
// lookup per binding, so a 20-actor group cost 41 queries.
func TestListGroupActorsIsNotNPlusOne(t *testing.T) {
	ctx := context.Background()

	counts := map[int]int{}
	for _, n := range []int{1, 5, 20} {
		store, counter, botUUID, groupID := setupGroupWithActors(t, n)

		counter.reset()
		actors, err := store.ListGroupActors(ctx, botUUID, groupID)
		require.NoError(t, err)
		require.Len(t, actors, n)

		counts[n] = counter.count()
		t.Logf("n=%2d -> %d queries", n, counts[n])
	}

	if counts[20] != counts[1] {
		t.Errorf("query count grows with group size: n=1 -> %d, n=5 -> %d, n=20 -> %d",
			counts[1], counts[5], counts[20])
	}
	// GetGroup + bindings + actors + permissions. Kept as an explicit bound so
	// a future change that reintroduces a per-actor query is caught here.
	if counts[20] > 5 {
		store, counter, botUUID, groupID := setupGroupWithActors(t, 20)
		counter.reset()
		_, _ = store.ListGroupActors(ctx, botUUID, groupID)
		t.Errorf("ListGroupActors issued %d queries for 20 actors, want a small constant:\n  %s",
			counts[20], counter.dump())
	}
}

// TestListGroupActorsContent verifies the batched form returns the same
// content the per-binding form did: every actor, its label, and its
// permissions ordered by capability then action.
func TestListGroupActorsContent(t *testing.T) {
	ctx := context.Background()
	store, _, botUUID, groupID := setupGroupWithActors(t, 3)

	actors, err := store.ListGroupActors(ctx, botUUID, groupID)
	require.NoError(t, err)
	require.Len(t, actors, 3)

	seen := map[string]bool{}
	for _, ga := range actors {
		require.Equal(t, groupID, ga.GroupID)
		require.Equal(t, "label", ga.Label)
		require.NotEmpty(t, ga.Actor.ID)
		require.False(t, seen[ga.Actor.ID], "actor %s returned twice", ga.Actor.ID)
		seen[ga.Actor.ID] = true

		// AddGroupActor seeds start+approve allow and privileged deny.
		require.Len(t, ga.Permissions, 3)

		effects := map[access.ActionName]access.AccessEffect{}
		for _, p := range ga.Permissions {
			require.Equal(t, access.CapabilityRemoteControl, p.Capability)
			effects[p.Action] = p.Effect
		}
		require.Equal(t, access.EffectAllow, effects[access.ActionRemoteControlStart])
		require.Equal(t, access.EffectAllow, effects[access.ActionRemoteControlApprove])
		require.Equal(t, access.EffectDeny, effects[access.ActionRemoteControlPrivileged])

		// Ordered by capability, then action -- the order the per-binding
		// query produced.
		for i := 1; i < len(ga.Permissions); i++ {
			prev, cur := ga.Permissions[i-1], ga.Permissions[i]
			prevKey := string(prev.Capability) + "\x00" + string(prev.Action)
			curKey := string(cur.Capability) + "\x00" + string(cur.Action)
			require.Less(t, prevKey, curKey, "permissions are not ordered by capability, action")
		}
	}
}

// TestListGroupActorsEmptyAndMissing covers the two edge cases: a group with
// no actors, and a group that does not belong to the bot.
func TestListGroupActorsEmptyAndMissing(t *testing.T) {
	ctx := context.Background()
	store, _, botUUID, groupID := setupGroupWithActors(t, 0)

	actors, err := store.ListGroupActors(ctx, botUUID, groupID)
	require.NoError(t, err)
	require.Empty(t, actors)
	require.NotNil(t, actors, "an empty group should return an empty slice, not nil")

	_, err = store.ListGroupActors(ctx, botUUID, "no-such-group")
	require.ErrorIs(t, err, ErrAccessTargetNotFound)

	_, err = store.ListGroupActors(ctx, "no-such-bot", groupID)
	require.ErrorIs(t, err, ErrAccessTargetNotFound)
}
