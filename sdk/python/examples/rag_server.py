"""A RAG model server, consumed by tingly-box as an ordinary provider.

Run it:

    pip install -e .                 # from sdk/python
    python examples/rag_server.py

Then add it in tingly-box → Connect AI → Self-hosted → Python Server (tingly)
(the startup banner prints the exact values), and bind a rule to the model
`rag-demo`. From that point it is a provider like any other: Claude Code,
Cursor, the tb UI and `tingly.connect()` experiments can all select it, with
guard rails, quota, logging and tier-failover applied — and when the process
is down, the same per-service circuit breaker that covers every provider
fails traffic over.

The handler calls *back* into tb via `srv.tb` for the generation step, so no
provider or key is hard-coded here.
"""

from tingly import Server

srv = Server(
    name="rag-demo",
    scenario="experiment",  # which rule-set the callbacks below run against
    description="Answers from a toy in-memory corpus",
)

CORPUS = {
    "tingly-box": "tingly-box is a personal intelligence orchestrator: an LLM "
    "gateway with remote control and guard rails.",
    "provider": "A tingly Server is an Anthropic/OpenAI-compatible upstream "
    "that tingly-box consumes as a self-hosted provider.",
}


def retrieve(question: str) -> str:
    q = question.lower()
    hits = [text for key, text in CORPUS.items() if key in q]
    return "\n".join(hits) or "(no matching documents)"


@srv.chat
def handle(req):
    question = req.last_user_text()
    docs = retrieve(question)
    # Generation goes back through tingly-box — no provider/key hard-coded here.
    return srv.tb.ask(
        f"Using only these documents:\n{docs}\n\nAnswer: {question}",
        model="auto",
    )


if __name__ == "__main__":
    srv.run()
