package binding

import "testing"

func boolPtr(b bool) *bool { return &b }

func TestUpsertBindingCreatesRoute(t *testing.T) {
	out, err := UpsertBinding("", Binding{Name: "claude_code", ChatID: "dm:ops"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "claude_code" || got[0].ChatID != "dm:ops" {
		t.Fatalf("unexpected bindings: %+v", got)
	}
	// A freshly created route with Enabled == nil must read back as mounted
	// (absence-is-on for the row itself; OutboundScenarioMounted requires the
	// row be present at all, which it now is).
	if !OutboundScenarioMounted(out) {
		t.Fatalf("expected route to be mounted, got %q", out)
	}
}

func TestUpsertBindingReplacesByName(t *testing.T) {
	base := `[{"name":"remote_agent","enabled":true},{"name":"claude_code","chat_id":"old","events":["Stop"]}]`

	out, err := UpsertBinding(base, Binding{
		Name:    "claude_code",
		ChatID:  "new",
		Enabled: boolPtr(false),
		Options: map[string]any{"on_timeout": "deny", "total_budget_seconds": 120},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows (remote_agent preserved + updated claude_code), got %+v", got)
	}

	var sawRA, sawCC bool
	for _, b := range got {
		switch b.Name {
		case RemoteAgentScenario:
			sawRA = true
			if b.Enabled == nil || !*b.Enabled {
				t.Fatalf("remote_agent row must survive untouched: %+v", b)
			}
		case "claude_code":
			sawCC = true
			if b.ChatID != "new" {
				t.Fatalf("chat_id not replaced: %+v", b)
			}
			if b.Enabled == nil || *b.Enabled {
				t.Fatalf("expected enabled=false, got %+v", b.Enabled)
			}
			if b.Options["on_timeout"] != "deny" {
				t.Fatalf("options lost: %+v", b.Options)
			}
			// Old "events" field must be gone — UpsertBinding replaces the
			// whole row rather than merging field by field.
			if len(b.Events) != 0 {
				t.Fatalf("expected events cleared by full replace, got %+v", b.Events)
			}
		}
	}
	if !sawRA || !sawCC {
		t.Fatalf("expected both rows present, got %+v", got)
	}

	// The disabled route must not count as mounted.
	if OutboundScenarioMounted(out) {
		t.Fatalf("expected not mounted after disabling the only route, got %q", out)
	}
}

func TestUpsertBindingRequiresName(t *testing.T) {
	if _, err := UpsertBinding("", Binding{ChatID: "dm:ops"}); err == nil {
		t.Fatal("expected error for empty binding name")
	}
}

func TestRemoveBinding(t *testing.T) {
	base := `[{"name":"remote_agent","enabled":true},{"name":"claude_code","chat_id":"c1"}]`

	out, err := RemoveBinding(base, "claude_code")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != RemoteAgentScenario {
		t.Fatalf("expected only remote_agent to remain, got %+v", got)
	}
	if OutboundScenarioMounted(out) {
		t.Fatalf("expected not mounted with no outbound routes, got %q", out)
	}

	// Removing a name that isn't present is a no-op, not an error.
	out2, err := RemoveBinding(out, "does_not_exist")
	if err != nil {
		t.Fatal(err)
	}
	got2, err := parse(out2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 {
		t.Fatalf("expected removing an absent name to be a no-op, got %+v", got2)
	}
}

func TestListOutboundBindings(t *testing.T) {
	base := `[{"name":"remote_agent","enabled":false},{"name":"claude_code","chat_id":"c1"},{"name":"claude_code_staging","chat_id":"c2","enabled":false}]`

	routes, err := ListOutboundBindings(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 outbound routes (remote_agent excluded), got %+v", routes)
	}
	names := map[string]bool{}
	for _, r := range routes {
		names[r.Name] = true
	}
	if !names["claude_code"] || !names["claude_code_staging"] {
		t.Fatalf("expected both outbound routes listed, got %+v", routes)
	}
}

func TestListOutboundBindingsEmpty(t *testing.T) {
	routes, err := ListOutboundBindings("")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected no routes for empty scenarios, got %+v", routes)
	}
}

func TestUpsertThenRemoveRoundTrip(t *testing.T) {
	out, err := UpsertBinding("", Binding{Name: "claude_code", ChatID: "dm:ops", Events: []string{"Stop", "PreToolUse"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err = UpsertBinding(out, Binding{Name: "remote_agent", Enabled: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}

	out, err = RemoveBinding(out, "claude_code")
	if err != nil {
		t.Fatal(err)
	}

	routes, err := ListOutboundBindings(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected claude_code removed, got %+v", routes)
	}
	if !ScenarioMounted(out, RemoteAgentScenario) {
		t.Fatalf("expected remote_agent to survive the claude_code removal")
	}
}
