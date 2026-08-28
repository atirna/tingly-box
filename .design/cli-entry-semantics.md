# CLI entry semantics: npx vs installed CLI, daemon default

Decided 2026-08, alongside re-enabling `npm install -g` (see `npm.md`).

## Problem

The npm shims historically ran `restart --daemon` when invoked with no
arguments. That default was designed for `npx tingly-box@latest`, where the
invocation itself expresses "run (the version I just fetched) now". Once
`npm install -g` became viable again, the same default made a casually typed
`tingly-box` silently restart a running server — killing in-flight AI
requests, which for an LLM gateway can be minutes-long streams.

## Decision

Split the entry semantics by how the process was invoked; server lifecycle
changes are either explicitly requested or explicitly confirmed.

**Bare invocation:**

- **`npx tingly-box` / `npm exec`** (shim detects `npm_command=exec`): keeps
  the historical `restart --daemon`. The npx invocation is itself the intent
  to run/update now.
- **Installed CLI** (global `npm install -g` bin run directly, or the raw Go
  binary): shows **help**. An installed CLI is a toolbox (like `git`,
  `docker`); the server is started deliberately with `tingly-box start`.
  Implemented twice so both layers agree: the shims pass `--help` when not
  under npx, and `cli/tingly-box/main.go` maps zero args to `--help`.

**`start`:**

- Daemonizes **by default** (`--no-daemon` for foreground). "Start the
  server" is service semantics; the terminal is handed back with the access
  banner. Foreground stays one flag away for debugging.
- Inside a container (PID 1, `/.dockerenv`, `/run/.containerenv`),
  daemonizing would exit PID 1 and kill the container, so `start` auto-falls
  back to foreground with a notice. This keeps existing Docker images and
  compose files working without teaching them `--no-daemon`, and avoids
  binary/Dockerfile version skew (the npx image installs whatever
  `TINGLY_VERSION` says at container start).
- When the server is **already running**, `start` converges instead of
  bailing with a hint:
  - **Same version** → print the access banner (Web UI URL + token, API
    endpoints — the thing the user actually came for) and exit. Never
    restart: there is nothing to update.
  - **Different or unknown version** → on a TTY, ask
    ("Restart the server? In-flight AI requests will be interrupted. [y/N]",
    default No); without a TTY, print how to apply (`tingly-box restart`)
    and exit 0. A restart is never silent.

  The running version comes from `<configDir>/tingly-server.version`
  (`pkg/lock.VersionFile`), a runtime artifact written next to the port file
  after the PID lock is acquired and removed on every shutdown path — same
  lifecycle and reader rules as `runtime-port-file.md`. "Unknown" (a server
  started by a build predating the file) is treated as a mismatch: the
  invoked binary may well be newer, and one confirmed restart makes the
  version known from then on.

**`restart` / `stop`:** remain the explicit, immediate lifecycle verbs.
`restart` inherits the daemon-by-default (container fallback applies).

## Explicitly out of scope (for now)

- Graceful drain (waiting for in-flight requests before restarting) and an
  in-flight request counter. The current guard is consent, not draining;
  `ServerManager.StopTimeout` is still short.
- Distinguishing `npm install -g` from npx in `--source` (both shims still
  report `npx`; only shortcut generation consumes it).
- `tb update` (npm.md plan C) — once it lands, it becomes the primary update
  verb and the npx restart default matters less.

## UX principles applied

- Smart defaults over toggles: daemon default for a service verb; container
  fallback instead of a flag every image must know.
- Scope side effects to the current surface: a bare command never restarts;
  only npx (where invocation = intent) keeps the run-now default.
- Surface the artifact for the next action: `start` on a running same-version
  server prints the access banner instead of "already running".
