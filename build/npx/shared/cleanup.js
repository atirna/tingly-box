// Deletion-adjacent maintenance shared by the npx shims in build/npx/*.
// Safety-critical: keep the guards here in sync with .design/npm.md
// (mitigations B and E) and the T1/T2/T4 cases in test-shim.sh.

import { readdirSync, rmSync, statSync } from "fs";
import { basename, dirname, join } from "path";

// Sweep leftover npm "retire" dirs (.<name>-<hash>) from an interrupted
// `npm update -g`; their deterministic name otherwise makes every later
// update fail with ENOTEMPTY. pkgDir is the caller's own install dir
// (dirname of its bin.js). See .design/npm.md.
export function cleanupRetiredInstallDirs(pkgDir) {
	try {
		const parentDir = dirname(pkgDir);
		if (basename(parentDir) !== "node_modules") return;
		// Fresh parent mtime: an npm transaction may be in flight and the
		// retired dir is its rollback source — skip.
		if (Date.now() - statSync(parentDir).mtimeMs < 5 * 60 * 1000) return;
		// Only npm's exact retire shape (@npmcli/arborist retire-path.js).
		const retired = new RegExp(`^\\.${basename(pkgDir)}-[a-zA-Z0-9]{8}$`);
		for (const entry of readdirSync(parentDir, { withFileTypes: true })) {
			if (entry.isDirectory() && retired.test(entry.name)) {
				rmSync(join(parentDir, entry.name), { recursive: true, force: true });
			}
		}
	} catch {
		// Never block launch on cleanup.
	}
}

// Sweep stale versioned binary caches (<cacheRoot>/<tag>/) left behind by
// earlier releases: every release downloads/extracts into its own tag dir
// and nothing ever removed the old ones. Keeps the tag in use plus the most
// recently touched other tag as rollback. See .design/npm.md.
export function cleanupStaleBinaryCaches(cacheRoot, keepTag) {
	try {
		// Exact release-tag dir shapes only ("latest" or vX.Y.Z[-pre]); files
		// (e.g. a future `current` pointer) and human-made dirs never match.
		const tagShape = /^(?:latest|v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$/;
		const candidates = [];
		for (const entry of readdirSync(cacheRoot, { withFileTypes: true })) {
			if (!entry.isDirectory() || !tagShape.test(entry.name)) continue;
			if (entry.name === keepTag) continue;
			const dirPath = join(cacheRoot, entry.name);
			const mtimeMs = statSync(dirPath).mtimeMs;
			// Fresh mtime: a concurrent launch (e.g. a version-pinned npx) may
			// be downloading into it right now — skip, sweep on a later run.
			if (Date.now() - mtimeMs < 60 * 60 * 1000) continue;
			candidates.push({ dirPath, mtimeMs });
		}
		// Newest non-current tag survives as rollback; the rest go. A binary
		// still running from a swept dir keeps its inode on POSIX; on Windows
		// the locked exe makes rmSync throw and the sweep retries next launch.
		candidates.sort((a, b) => b.mtimeMs - a.mtimeMs);
		for (const { dirPath } of candidates.slice(1)) {
			rmSync(dirPath, { recursive: true, force: true });
		}
	} catch {
		// Never block launch on cleanup.
	}
}
