// Entry semantics shared by the cli and bundle shims.
// npx / `npm exec` sets npm_command=exec; a bin launched directly (e.g. from
// `npm install -g`) does not. Rationale: .design/cli-entry-semantics.md.

export const IS_NPX = process.env.npm_command === "exec";

// Bare npx invocation = "run it now" (restart into the background, -y being
// the consent a bare `restart` would otherwise prompt for); a bare installed
// bin shows help.
export const DEFAULT_ARGS = IS_NPX ? ["restart", "--daemon", "-y"] : ["--help"];

// Records how this process was launched; also decides which npm package a
// shortcut relaunches. As a global flag it must come before the subcommand.
export function sourceArgs(npxSource, npmSource) {
	return [IS_NPX ? `--source=${npxSource}` : `--source=${npmSource}`];
}
