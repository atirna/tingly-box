# npm distribution

How tingly-box ships through npm, and the plan to make `npm install -g` viable
again.

## Architecture today

Three packages under `build/npx/`, published by `.github/workflows/npm.yml` on
each GitHub release:

- **`tingly-box`** — thin shim (`bin.js` + `package.json`, ~16 KB published).
  On first run it downloads the platform zip from GitHub Releases into
  `~/.cache/tingly-box/<tag>/bin/` and execs the Go binary from there. CI bakes
  the release tag into `BINARY_RELEASE_BRANCH` at publish time, so npm package
  version ↔ binary version are 1:1 coupled.
- **`tingly-box-bundle`** — same shim plus the platform zips inside the package
  (~70 MB, offline-ready). npx-oriented; not a global-install target.
- **`tingly-box-gui`** — shim variant for the desktop UI, published on demand.

All three expose `tingly-box` and `tb` bins. The shim never writes into its own
install dir — binaries and caches live under `~/.cache/tingly-box/`.

## Making `npm install -g` viable again

Status: proposal (2026-08). Today the README recommends `npx` only, because
`npm update -g tingly-box` intermittently fails with `ENOTEMPTY` and leaves the
global install broken. The rest of this doc explains the failure and lays out
the path to re-enable global installs.

### The failure, precisely

```
npm error code ENOTEMPTY
npm error syscall rename
npm error path .../lib/node_modules/tingly-box
npm error dest .../lib/node_modules/.tingly-box-BweFGdMf
```

When npm updates a global package it first "retires" the old copy by renaming
`node_modules/tingly-box` → `node_modules/.tingly-box-<hash>`, installs the new
tree, then deletes the retired dir. Two properties of npm's implementation make
this fragile and *sticky*:

1. **The retire name is deterministic** — `<hash>` is derived from the path,
   not random. So if one update is interrupted (Ctrl-C, crash, disk full) and
   leaves `.tingly-box-<hash>` behind, **every subsequent update fails** with
   ENOTEMPTY (rename dest is a non-empty dir) until the leftover is removed by
   hand. This matches the observed "fails once, then tb is broken until manual
   cleanup".
2. **The rename moves the whole package dir including its nested
   `node_modules`** — our published shim is 2 files, but at install time npm
   materializes `undici` (hundreds of files) + `unzipper` under
   `node_modules/tingly-box/node_modules/`. A big tree widens the window for
   partial failures (AV scanners, NFS/OneDrive-backed homes, concurrent npx).

Note what is *not* the cause: the shim never writes into its own install dir
(binaries go to `~/.cache/tingly-box/<tag>/bin`), so we aren't corrupting our
own package. This is an npm-side weakness we have to engineer around, same
class of pain that pushed Claude Code off npm-global installs to a native
installer + self-update.

Immediate user-facing workaround (already in README):

```bash
NPM_GLOBAL_DIR=$(npm root -g)
rm -rf "$NPM_GLOBAL_DIR/tingly-box" "$NPM_GLOBAL_DIR"/.tingly-box-*
npm install -g tingly-box@latest
```

### Plan

Ordered by cost; A+B remove the sharp edges, C removes the *need* to ever run
`npm update -g`, which is the real fix.

#### A. Ship the shim as a zero-dependency single file

Bundle `bin.js` with esbuild at publish time (CI step in `npm.yml`, before
`npm publish`) so `undici` + `unzipper` are inlined and published
`dependencies` become `{}`:

```bash
npx esbuild bin.js --bundle --platform=node --target=node18 \
  --format=esm --banner:js="import{createRequire}from'module';const require=createRequire(import.meta.url);" \
  --outfile=bin.js --allow-overwrite
```

(The `createRequire` banner is needed because `unzipper` is CJS and esbuild's
ESM output otherwise emits bare `require` calls.)

Effect: the global package dir holds 2 files (~1–2 MB), no nested
`node_modules` is ever created, the retire-rename is near-atomic, and
`npm i -g` / `npx` get faster (no dep resolution). This shrinks the ENOTEMPTY
window to almost nothing but does not fix stickiness (B does).

Applies to `tingly-box` and `tingly-box-gui`. `tingly-box-bundle` (70 MB of
zips) should stay npx-oriented; don't advertise global install for it.

#### B. Self-heal retired leftovers in the shim

On startup, `bin.js` knows its own install location
(`dirname(fileURLToPath(import.meta.url))`). If its parent directory contains
stale `.tingly-box-*` retire dirs, remove them. This un-bricks the *next*
`npm update -g` automatically — the deterministic-name stickiness disappears.

Scope guard: only delete siblings matching `.{tingly-box,tingly-box-gui,tingly-box-bundle}-*`
directly next to our own package dir; ignore errors silently (a concurrent npm
run will clean up after itself anyway).

#### C. Decouple binary version from npm version: `tb update`

Today CI pins `BINARY_RELEASE_BRANCH` to the release tag inside the published
`bin.js`, so getting a new Go binary requires a new npm package — that's why
users run `npm update -g` at all. Invert this:

1. **Go CLI grows `tingly-box update`** (and the webui "new version" banner
   points at it): query latest release, download the platform zip into
   `~/.cache/tingly-box/<new-tag>/bin/` (each version in its own dir — no
   in-place overwrite, so no Windows locked-exe problem, and a running daemon
   keeps its old inode), verify it runs (`--version`), then atomically write a
   `~/.cache/tingly-box/current` pointer file (write temp + rename).
2. **Shim resolves `current` first**: if the pointer exists and the binary it
   names exists, exec that; otherwise fall back to the baked-in
   `BINARY_RELEASE_BRANCH` tag (first-run bootstrap, and the floor version).
   The stale `getLatestVersion()` in `bin.js` (it fetches the download URL and
   would fail to parse) gets deleted — "what is latest" is the Go side's job.
3. `update` also GCs old `~/.cache/tingly-box/<tag>` dirs, keeping the
   previous one as rollback.

After C, the npm package is a thin installer/launcher that changes rarely
(only when shim logic changes); users update via `tb update` and never touch
`npm update -g`. This is the same endgame as Claude Code's native installer,
reached without leaving npm as the distribution channel.

#### D. README posture (after A+B ship)

- Update instruction becomes `tb update` (once C lands); until then, prefer
  `npm install -g tingly-box@latest` over `npm update -g` — the install path
  re-resolves cleanly and also crosses major versions.
- Keep the ENOTEMPTY cleanup snippet as troubleshooting.

### Rollout

1. A + B in the next shim release (no Go changes; CI + bin.js only).
2. C behind a normal feature PR (Go `update` command + shim `current`
   resolution); ship shim change in the same release train as the Go command.
3. Flip README to "npm install -g supported" once C has soaked for a release.
