"""Tests for the usage / guardrails views (gateway mocked with respx)."""

import httpx
import respx

from tingly.helpers.guardrails import GuardrailsView
from tingly.helpers.usage import UsageView

BASE = "http://tb.test:12580"


def _stats_row(**over):
    row = {
        "key": "claude-sonnet-4-6",
        "model": "claude-sonnet-4-6",
        "provider_name": "anthropic",
        "provider_uuid": "p-1",
        "scenario": "experiment",
        "user_id": "",
        "request_count": 3,
        "total_input_tokens": 1000,
        "total_output_tokens": 500,
        "total_tokens": 1500,
        "cache_read_tokens": 200,
        "cache_write_tokens": 100,
        "avg_input_tokens": 333.3,
        "avg_output_tokens": 166.6,
        "avg_latency_ms": 1200,
        "error_count": 0,
        "error_rate": 0.0,
        "streamed_count": 3,
        "streamed_rate": 1.0,
    }
    row.update(over)
    return row


def _stats_body(rows):
    return {
        "data": rows,
        "meta": {
            "start_time": "2026-01-01T00:00:00Z",
            "end_time": "2026-01-02T00:00:00Z",
            "group_by": "model",
            "total_count": len(rows),
        },
    }


@respx.mock
def test_usage_reports_real_token_numbers():
    """The regression: this used to be structurally incapable of anything but 0.

    The old view read /api/v1/requests, took payload["data"] (that endpoint
    sends "requests"), and summed rec["input_tokens"] (ModelRequestSummary has
    no token fields at all) — inside a bare except that returned an empty
    summary. So it always reported zeros. Assert non-zero totals here so a
    regression to that shape cannot pass.
    """
    respx.get(f"{BASE}/api/v1/usage/stats").mock(
        return_value=httpx.Response(200, json=_stats_body([_stats_row()]))
    )
    view = UsageView(BASE, "admin", "experiment", 5.0)
    got = view.this_session()
    view.close()

    assert got.requests == 3
    assert got.input_tokens == 1000
    assert got.output_tokens == 500
    assert got.total_tokens == 1500
    assert got.cache_read_tokens == 200
    assert got.by_model == {"claude-sonnet-4-6": 3}


@respx.mock
def test_usage_is_scoped_to_the_session_scenario():
    route = respx.get(f"{BASE}/api/v1/usage/stats").mock(
        return_value=httpx.Response(200, json=_stats_body([]))
    )
    view = UsageView(BASE, "admin", "experiment", 5.0)
    view.this_session()
    view.close()

    params = route.calls.last.request.url.params
    assert params["scenario"] == "experiment"
    assert params["group_by"] == "model"


@respx.mock
def test_usage_sums_across_rows():
    respx.get(f"{BASE}/api/v1/usage/stats").mock(
        return_value=httpx.Response(
            200,
            json=_stats_body([
                _stats_row(),
                _stats_row(key="gpt-5", request_count=2, total_input_tokens=7, total_output_tokens=11),
            ]),
        )
    )
    view = UsageView(BASE, "admin", "experiment", 5.0)
    got = view.this_session()
    view.close()

    assert got.requests == 5
    assert got.input_tokens == 1007
    assert got.output_tokens == 511
    assert set(got.by_model) == {"claude-sonnet-4-6", "gpt-5"}


@respx.mock
def test_usage_store_unavailable_degrades_to_empty():
    """503 means "no usage store configured" — a normal state, not an error."""
    respx.get(f"{BASE}/api/v1/usage/stats").mock(return_value=httpx.Response(503))
    view = UsageView(BASE, "admin", "experiment", 5.0)
    got = view.this_session()
    view.close()
    assert got.requests == 0 and got.rows == []


def _guardrails_body(policies, exists=True):
    return {
        "path": "/cfg/guardrails.yaml",
        "exists": exists,
        "content": "",
        "config": {"policies": policies},
        "supported_scenarios": ["claude_code", "experiment"],
    }


@respx.mock
def test_guardrails_counts_only_enabled_policies():
    """The old view counted every policy in the file, enabled or not."""
    respx.get(f"{BASE}/api/v1/guardrails/config").mock(
        return_value=httpx.Response(
            200,
            json=_guardrails_body([
                {"id": "p1", "name": "no-secrets", "kind": "regex", "enabled": True,
                 "match": {}},
                {"id": "p2", "name": "draft", "kind": "regex", "enabled": False,
                 "match": {}},
            ]),
        )
    )
    view = GuardrailsView(BASE, "admin", 5.0)
    got = view.status()
    view.close()

    assert got.enabled is True
    assert got.active_policies == 1
    assert got.policy_names == ["no-secrets"]
    assert got.supported_scenarios == ["claude_code", "experiment"]


@respx.mock
def test_guardrails_falls_back_to_id_when_unnamed():
    respx.get(f"{BASE}/api/v1/guardrails/config").mock(
        return_value=httpx.Response(
            200,
            json=_guardrails_body([
                {"id": "p1", "kind": "regex", "enabled": True, "match": {}},
            ]),
        )
    )
    view = GuardrailsView(BASE, "admin", 5.0)
    got = view.status()
    view.close()
    assert got.policy_names == ["p1"]


@respx.mock
def test_guardrails_absent_config_is_reported_not_raised():
    respx.get(f"{BASE}/api/v1/guardrails/config").mock(
        return_value=httpx.Response(200, json=_guardrails_body([], exists=False))
    )
    view = GuardrailsView(BASE, "admin", 5.0)
    got = view.status()
    view.close()
    assert got.exists is False and got.enabled is False
    assert "no guard-rail config" in got.summary
