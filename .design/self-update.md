# Self-Update: Design and Decisions

> Audience: contributors touching `/info/version/check`, `/info/update`,
> the UpdatePanelDialog, or anything that changes how Tingly Box is
> installed/launched. Related: `.design/shortcut.md` (launch source, npx
> version pinning).

---

## 1. The problem

npx-based shortcuts are pinned to a version on purpose (no surprise
auto-upgrade, works offline — see `.design/shortcut.md` §3). The flip side
is a loop: the shortcut launches the pinned old version → the running
process is the old version → nothing ever moves the pin. A user who only
ever double-clicks the shortcut never updates, and the "update methods"
panel in the UI only offered commands to copy into a terminal — exactly the
interaction the shortcut exists to remove.

## 2. Decisions

- **The shortcut is not an updater.** Launch stays launch: no
  check-on-double-click, no second "Update Tingly Box" icon. The update
  entry point is the web UI — the surface the user already looks at.
- **Update must traverse the real install path.** Each install shape
  updates the way it was installed, or not at all through the UI:

  | install shape        | one-click? | why                                          |
  |----------------------|-----------|-----------------------------------------------|
  | `npx` / `npx-bundle` | yes       | "update" is just relaunching with a newer pin |
  | binary (brew, manual)| no (v1)   | must go through brew / manual download; the copyable commands remain |

- **No `tingly-box update` CLI.** Terminal users already have the real
  command (`npx tingly-box@latest`); a wrapper adds surface without adding
  capability. (Explicit product decision.)
- **Never silent.** Tingly Box sits in the request path; versions change
  only when the user clicks. The check itself is passive (cached npm
  registry lookup, badge in the UI).

## 3. How the npx one-click path works

```
UI button → POST /api/v1/info/update
  guard: launchSource ∈ {npx, npx-bundle}      (else 400 with guidance)
  guard: latest > running                       (else 400 "up to date")
  spawn detached: sh -lc 'npx -y tingly-box@<latest> restart --daemon [--shortcut]'
  → respond {target_version, command}; UI polls /info/version, reloads on change
```

- The relaunch command is built by `internal/shortcut.ResolveLaunchWith` —
  the *same* command a shortcut runs, pinned to the target version instead
  of the running one. No new launch machinery.
- The spawned child is fully detached (`pkg/daemon.DetachAttrs`) because it
  will stop and replace the very server that spawned it (`restart`).
- `launchSource` reaches the server in memory only: global `--source` flag
  → `options.StartFlags` → `server.WithLaunchSource` →
  `info.Handler`. No detection, no persistence — same rule as
  `.design/shortcut.md` §3. The daemon re-exec preserves original args, so
  the daemonized child still knows its source.

### Shortcut repin

`--shortcut` is appended to the relaunch **iff** `shortcut.AnyExists`
finds an existing artifact at the platform's known paths. The new version
then rewrites the user's shortcuts pinned to itself. Users who never
created a shortcut never get one out of an update; users who have one get
it repinned as part of the same explicit action they asked for. Known
coarseness, accepted for v1: Windows OneDrive-redirected folders aren't
resolved by `AnyExists` (missed repin, never a wrongly-created file), and a
repin recreates the full artifact set even if the user had deleted part
of it.

## 4. UI behavior (UpdatePanelDialog)

When `has_update && can_one_click`: a primary "Update to X & Restart"
button above the copyable commands. On click the UI POSTs, then polls
`/info/version` (up to 5 min) and reloads the page when the served version
changes; on poll timeout it tells the user the update is still running in
the background rather than claiming failure. The copyable commands stay —
they are the path for binary installs and the fallback when the one-click
path errors.

## 5. UX checklist (against `.design/ux-principles.md`)

| principle                            | how                                                            |
|--------------------------------------|----------------------------------------------------------------|
| eliminate mode pickers               | the user never selects an install shape — the server knows its own `launchSource` and the UI shows only the applicable action |
| show concrete values not aliases     | the button and responses name the real target version; the API returns the exact command it spawned |
| diagnostics traverse the real path   | update runs the same npx command path the install/shortcut uses |
| scope side effects to current surface| shortcut repin only touches artifacts that already exist        |
| smart defaults over toggles          | no auto-update toggle: check is automatic and passive, applying is always an explicit click |
