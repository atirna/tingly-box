"""Tests for the example servers (sdk/python/examples/).

Every example is a provider that dispatches to tb rules, so what these pin is
the *dispatch decision* — which rule each request is routed to — rather than
any model output. srv.use() / srv.tb are monkeypatched, so no real tb and no
real model calls. The examples aren't part of the installed `tingly` package,
so they're loaded by file path.
"""

import importlib.util
import sys
from pathlib import Path

from tingly.server.types import ChatRequest

EXAMPLES = Path(__file__).parent.parent / "examples"


def _load(name):
    path = EXAMPLES / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


def _req(content, system=None):
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": content})
    return ChatRequest.from_openai_body({"model": "x", "messages": messages})


class _FakeClient:
    """Stands in for a tingly.Client — records calls, replies with a fixed
    string or (if given a callable) the result of calling it with the prompt."""

    def __init__(self, reply):
        self._reply = reply
        self.calls = []

    def ask(self, prompt, **kwargs):
        self.calls.append((prompt, kwargs))
        return self._reply(prompt) if callable(self._reply) else self._reply


# -- critic -----------------------------------------------------------------

def test_critic_formats_valid_json_verdict(monkeypatch):
    critic = _load("critic_server")
    fake = _FakeClient('{"verdict": "approve", "issues": [], "suggestion": "looks good"}')
    monkeypatch.setattr(critic.srv, "use", lambda scenario: fake)

    result = critic.handle(_req("def f(): return 1/0", system="a python snippet"))

    assert result == "verdict: approve\nsuggestion: looks good"
    prompt, kwargs = fake.calls[0]
    assert "a python snippet" in prompt
    assert "def f(): return 1/0" in prompt
    assert kwargs["model"] == critic.CRITIC_MODEL


def test_critic_lists_issues(monkeypatch):
    critic = _load("critic_server")
    fake = _FakeClient('{"verdict": "revise", "issues": ["divides by zero"], "suggestion": "guard the denominator"}')
    monkeypatch.setattr(critic.srv, "use", lambda scenario: fake)

    result = critic.handle(_req("def f(): return 1/0"))

    assert "verdict: revise" in result
    assert "- divides by zero" in result
    assert "suggestion: guard the denominator" in result


def test_critic_degrades_gracefully_on_non_json(monkeypatch):
    """A critic model that ignores the JSON contract must not crash the
    request — it should surface as a 'revise' verdict carrying the raw text."""
    critic = _load("critic_server")
    fake = _FakeClient("looks fine to me")
    monkeypatch.setattr(critic.srv, "use", lambda scenario: fake)

    result = critic.handle(_req("some code"))

    assert "verdict: revise" in result
    assert "looks fine to me" in result


def test_critic_strips_markdown_code_fence(monkeypatch):
    critic = _load("critic_server")
    fake = _FakeClient('```json\n{"verdict": "approve", "issues": [], "suggestion": ""}\n```')
    monkeypatch.setattr(critic.srv, "use", lambda scenario: fake)

    result = critic.handle(_req("some code"))

    assert result == "verdict: approve"


# -- fusion -------------------------------------------------------------

def test_poll_panel_gathers_one_result_per_panel_entry(monkeypatch):
    fusion = _load("fusion_server")
    fake = _FakeClient("same-answer")
    monkeypatch.setattr(fusion.srv, "use", lambda scenario: fake)

    results = fusion._poll_panel("q")

    assert results == ["same-answer"] * len(fusion.PANEL)
    assert len(fake.calls) == len(fusion.PANEL)


def test_fusion_skips_judge_when_panel_agrees(monkeypatch):
    fusion = _load("fusion_server")
    monkeypatch.setattr(fusion, "_poll_panel", lambda question: ["42", "42"])

    def judge_should_not_be_called(scenario):
        raise AssertionError("judge must not be called when the panel agrees")

    monkeypatch.setattr(fusion.srv, "use", judge_should_not_be_called)

    assert fusion.handle(_req("what is 6*7?")) == "42"


def test_fusion_calls_judge_when_panel_disagrees(monkeypatch):
    fusion = _load("fusion_server")
    monkeypatch.setattr(fusion, "_poll_panel", lambda question: ["A", "B"])
    judge = _FakeClient("SYNTHESIZED")
    monkeypatch.setattr(fusion.srv, "use", lambda scenario: judge)

    result = fusion.handle(_req("question"))

    assert result == "SYNTHESIZED"
    assert len(judge.calls) == 1
    judge_prompt = judge.calls[0][0]
    assert "A" in judge_prompt and "B" in judge_prompt
    assert "question" in judge_prompt


# -- router -----------------------------------------------------------------

class _FakeRule:
    """Stands in for a generated models.Rule — only the fields pick_rule reads."""

    def __init__(self, request_model, active=True):
        self.request_model = request_model
        self.active = active


class _FakeTB:
    def __init__(self, rules):
        self._rules = rules

    def rules(self, scenario=None):
        return self._rules


def _router(monkeypatch, available, reply="answer"):
    router = _load("router_server")
    monkeypatch.setattr(
        type(router.srv), "tb", property(lambda self: _FakeTB(available)), raising=False
    )
    fake = _FakeClient(reply)
    monkeypatch.setattr(router.srv, "use", lambda scenario: fake)
    return router, fake


def test_router_sends_short_prompts_to_a_cheap_rule(monkeypatch):
    router, fake = _router(
        monkeypatch, [_FakeRule("gpt-5"), _FakeRule("claude-haiku-4-5")]
    )
    router.handle(_req("hi"))
    assert fake.calls[0][1]["model"] == "claude-haiku-4-5"


def test_router_sends_long_prompts_to_a_strong_rule(monkeypatch):
    router, fake = _router(
        monkeypatch, [_FakeRule("claude-haiku-4-5"), _FakeRule("claude-opus-4-6")]
    )
    router.handle(_req("x" * (router.LONG_REQUEST_CHARS + 1)))
    assert fake.calls[0][1]["model"] == "claude-opus-4-6"


def test_router_recognises_code(monkeypatch):
    router, fake = _router(
        monkeypatch, [_FakeRule("claude-haiku-4-5"), _FakeRule("claude-sonnet-4-6")]
    )
    router.handle(_req("def f():\n    return 1"))
    assert fake.calls[0][1]["model"] == "claude-sonnet-4-6"


def test_router_skips_rules_this_box_does_not_have(monkeypatch):
    """Preferences are hints matched against real rules, not requirements.

    A box with only one model must still work rather than dispatching to a
    model id that does not exist.
    """
    router, fake = _router(monkeypatch, [_FakeRule("only-model")])
    router.handle(_req("hi"))
    assert fake.calls[0][1]["model"] == "only-model"


def test_router_ignores_inactive_rules(monkeypatch):
    router, fake = _router(
        monkeypatch, [_FakeRule("claude-haiku-4-5", active=False), _FakeRule("gpt-5")]
    )
    router.handle(_req("hi"))
    assert fake.calls[0][1]["model"] == "gpt-5"


def test_router_falls_back_to_auto_on_an_empty_box(monkeypatch):
    """No rules configured -> let tb's own smart routing decide."""
    router, fake = _router(monkeypatch, [])
    router.handle(_req("hi"))
    assert fake.calls[0][1]["model"] == "auto"


def test_router_reports_its_decision(monkeypatch):
    router, _ = _router(monkeypatch, [_FakeRule("gpt-5")], reply="the answer")
    out = router.handle(_req("hi"))
    assert "router:" in out and "gpt-5" in out and "the answer" in out
