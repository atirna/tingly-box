package interaction

import (
	"testing"

	"github.com/tingly-dev/tingly-box/imbot/core"
)

// TestBuildAndParseAgree is the property the five hand-written per-platform
// implementations failed to hold: Feishu's builder wrote a button value of
// {"action": value}, dropping the namespace and the interaction ID its own
// parser looked for, so no Feishu interaction could ever resolve. One builder
// and one parser cannot drift apart.
func TestBuildAndParseAgree(t *testing.T) {
	set := BuildActions([]Interaction{
		{ID: "perm", Type: ActionConfirm, Label: "Approve", Value: "yes"},
		{ID: "perm", Type: ActionCancel, Label: "Deny", Value: "no"},
	})

	for _, action := range set.Flatten() {
		msg := core.Message{Payload: action.Payload, Metadata: map[string]any{"is_callback": true}}
		resp, err := ParseActionResponse(msg)
		if err != nil {
			t.Fatalf("%s: %v", action.Label, err)
		}
		if resp == nil {
			t.Fatalf("%s: parsed to nothing", action.Label)
		}
		if resp.Action.ID != "perm" {
			t.Errorf("%s: ID = %q, want perm", action.Label, resp.Action.ID)
		}
	}
}

// TestNavigateActionsShareARow: navigation reads as a strip, selections as a
// stack. Layout is the only thing the old per-platform builders agreed on and
// it is worth keeping.
func TestNavigateActionsShareARow(t *testing.T) {
	set := BuildActions([]Interaction{
		{ID: "p", Type: ActionNavigate, Label: "Prev", Value: "prev"},
		{ID: "p", Type: ActionNavigate, Label: "Next", Value: "next"},
		{ID: "p", Type: ActionCancel, Label: "Cancel", Value: "x"},
	})
	if len(set.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(set.Rows))
	}
	if len(set.Rows[0]) != 2 {
		t.Errorf("navigation should share a row, got %d", len(set.Rows[0]))
	}
}

// TestInputActionsHaveNoButton: an input action is answered by typing, so
// rendering a button for it would offer the user something inert.
func TestInputActionsHaveNoButton(t *testing.T) {
	set := BuildActions([]Interaction{{ID: "q", Type: ActionInput, Label: "Type it", Value: "v"}})
	if !set.IsEmpty() {
		t.Errorf("expected no buttons, got %v", set.Rows)
	}
}

// TestParseRejectsForeignNamespace: the application's own buttons share the
// platform with interaction buttons and must not be swallowed here.
func TestParseRejectsForeignNamespace(t *testing.T) {
	msg := core.Message{
		Payload:  core.NewPayload("bind", "up"),
		Metadata: map[string]any{"is_callback": true},
	}
	if _, err := ParseActionResponse(msg); err != ErrNotInteraction {
		t.Errorf("err = %v, want ErrNotInteraction", err)
	}
}

// TestParseIgnoresNonCallbacks: a typed message is not a button press, and
// must fall through to the numbered-text path rather than erroring.
func TestParseIgnoresNonCallbacks(t *testing.T) {
	resp, err := ParseActionResponse(core.Message{Content: core.NewTextContent("1")})
	if err != nil || resp != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", resp, err)
	}
}

// TestParseAcceptsLegacyFlatCallbackData covers platforms whose inbound
// adapters do not fill Message.Payload yet.
func TestParseAcceptsLegacyFlatCallbackData(t *testing.T) {
	msg := core.Message{Metadata: map[string]any{
		"is_callback":   true,
		"callback_data": "ia:perm:req-1:yes",
	}}
	resp, err := ParseActionResponse(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RequestID != "req-1" || resp.Action.Value != "yes" {
		t.Errorf("parsed = %+v", resp)
	}
}
