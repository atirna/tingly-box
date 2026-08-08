"""A "fusion" server: parallel multi-model consensus, then a judge model
synthesizes — the pattern behind Consult7's 2026 Fusion feature (a panel of
frontier models answers in parallel; a judge model merges the answers; a
panel that already agrees skips the judge call).

This is the clearest illustration of why a Python provider is worth more than
the sum of its parts: it is a perfectly ordinary upstream from tb's side, yet
its handler calls BACK into tb more than once, against DIFFERENT rules/models,
concurrently, before answering once. One provider, orchestrating the box.

Run it (serves on :8767):

    pip install -e .                 # from sdk/python
    python examples/fusion_server.py

Add it in tb as a self-hosted provider (the startup banner prints the values),
bind a rule to model `fusion`, then from any tb client the message is the
question.
"""

from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor

from tingly import ChatRequest, Server

# The panel: each entry is a (scenario, request_model) pair — i.e. one tb rule
# — called independently and concurrently. Point these at genuinely different
# rules; a panel of clones of the same model adds latency without adding a
# second opinion. `srv.tb.rules("openai")` lists what this box actually has.
PANEL = [
    ("openai", "auto"),
    ("openai", "auto"),
]
JUDGE_SCENARIO = "openai"
JUDGE_MODEL = "auto"

JUDGE_PROMPT = """Multiple models answered the same question independently. \
Synthesize the single best answer, resolving disagreements and noting when \
the panel disagreed.

--- question ---
{question}

--- panel answers ---
{answers}
"""

srv = Server(
    name="fusion",
    scenario=JUDGE_SCENARIO,  # which rule-set the panel/judge calls run against
    description="Multi-model consensus — panel of rules/models + judge synthesis",
)


@srv.chat
def handle(req: ChatRequest) -> str:
    question = req.last_user_text()
    answers = _poll_panel(question)

    if len(set(answers)) == 1:
        # The panel already agreed — the judge call would just restate this,
        # so skip it and save a hop (mirrors Consult7 skipping the panel
        # entirely for trivial prompts).
        return answers[0]

    answers_block = "\n\n".join(f"[{i + 1}] {a}" for i, a in enumerate(answers))
    return srv.use(JUDGE_SCENARIO).ask(
        JUDGE_PROMPT.format(question=question, answers=answers_block),
        model=JUDGE_MODEL,
    )


def _poll_panel(question: str) -> list:
    with ThreadPoolExecutor(max_workers=len(PANEL)) as pool:
        futures = [
            pool.submit(srv.use(scenario).ask, question, model=model)
            for scenario, model in PANEL
        ]
        return [f.result() for f in futures]


if __name__ == "__main__":
    srv.run(port=8767)
