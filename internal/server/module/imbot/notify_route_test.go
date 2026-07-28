package imbot

import (
	"testing"

	"github.com/tingly-dev/tingly-box/remote/binding"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func TestApplyNotifyRoute_CreateDefaultsNameToClaudeCode(t *testing.T) {
	out, err := applyNotifyRoute("", &NotifyRouteRequest{ChatID: "dm:ops"})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binding.ListOutboundBindings(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Name != "claude_code" || routes[0].ChatID != "dm:ops" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
}

func TestApplyNotifyRoute_RequiresChatIDUnlessRemoving(t *testing.T) {
	if _, err := applyNotifyRoute("", &NotifyRouteRequest{}); err == nil {
		t.Fatal("expected error for missing chat_id")
	}
	// Removing doesn't need a chat_id.
	if _, err := applyNotifyRoute("", &NotifyRouteRequest{Remove: true}); err != nil {
		t.Fatalf("remove should not require chat_id: %v", err)
	}
}

func TestApplyNotifyRoute_OptionsMapToPermissionPolicy(t *testing.T) {
	out, err := applyNotifyRoute("", &NotifyRouteRequest{
		ChatID:             "dm:ops",
		OnTimeout:          "deny",
		TotalBudgetSeconds: intPtr(120),
	})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binding.ListOutboundBindings(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %+v", routes)
	}
	if routes[0].Options["on_timeout"] != "deny" {
		t.Fatalf("on_timeout lost: %+v", routes[0].Options)
	}
	// Round-tripped through JSON, a number decodes into interface{} as
	// float64, not int — that's the existing binding.Options contract
	// (claudecode.readPolicy already handles both shapes), not a bug here.
	if routes[0].Options["total_budget_seconds"] != float64(120) {
		t.Fatalf("total_budget_seconds lost: %+v", routes[0].Options)
	}
}

func TestApplyNotifyRoute_RemovePreservesOtherBindings(t *testing.T) {
	base := `[{"name":"remote_agent","enabled":true},{"name":"claude_code","chat_id":"dm:ops"}]`
	out, err := applyNotifyRoute(base, &NotifyRouteRequest{Remove: true})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.ScenarioMounted(out, binding.RemoteAgentScenario) {
		t.Fatalf("expected remote_agent to survive route removal, got %q", out)
	}
	routes, err := binding.ListOutboundBindings(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected the route removed, got %+v", routes)
	}
}

func TestApplyNotifyRoute_CustomName(t *testing.T) {
	out, err := applyNotifyRoute("", &NotifyRouteRequest{Name: "claude_code_staging", ChatID: "dm:staging"})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binding.ListOutboundBindings(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Name != "claude_code_staging" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
}

func TestNotifyRouteActivates(t *testing.T) {
	cases := []struct {
		name string
		req  *NotifyRouteRequest
		want bool
	}{
		{"create with nil Enabled activates", &NotifyRouteRequest{ChatID: "c1"}, true},
		{"explicit enabled=true activates", &NotifyRouteRequest{ChatID: "c1", Enabled: boolPtr(true)}, true},
		{"explicit enabled=false does not activate", &NotifyRouteRequest{ChatID: "c1", Enabled: boolPtr(false)}, false},
		{"remove never activates", &NotifyRouteRequest{Remove: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := notifyRouteActivates(tc.req); got != tc.want {
				t.Fatalf("notifyRouteActivates() = %v, want %v", got, tc.want)
			}
		})
	}
}
