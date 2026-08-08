"""E2E fixture: a Python provider that calls BACK into tb's echo-model.

Demonstrates the full loop with no network and no API keys:
  client → tb (rule rag-demo) → THIS server → srv.use("experiment")
         → tb (rule echo-model → vmodel provider) → echoed text → back to client

Note what is absent: this file performs no registration of any kind. tb learns
about it the same way it learns about Ollama — someone created a provider
pointing at its base URL. `e2e_run.sh` does that with the ordinary
POST /api/v1/providers call, which is exactly what the Connect AI dialog does.
"""

from tingly import Server

srv = Server(name="rag-demo", scenario="experiment")

CORPUS = {
    "tingly-box": "tingly-box is a personal intelligence orchestrator.",
    "provider": "A tingly Server is an Anthropic/OpenAI-compatible upstream tb can route to.",
}


def retrieve(q: str) -> str:
    hits = [t for k, t in CORPUS.items() if k in q.lower()]
    return " ".join(hits) or "(no docs)"


@srv.chat
def handle(req):
    q = req.last_user_text()
    docs = retrieve(q)
    # Call back into tb against the echo-model rule (no real network needed).
    echoed = srv.use("experiment").ask(
        f"[py-rag] docs={docs!r} q={q!r}", model="echo-model"
    )
    return f"RAG via Python provider → tb echo returned: {echoed}"


if __name__ == "__main__":
    srv.run(port=8765)
