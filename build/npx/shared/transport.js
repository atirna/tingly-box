// --transport-version parsing shared by the cli and gui shims.

// Validate transport version format
function validateTransportVersion(version) {
	if (version === "latest") {
		return version;
	}

	// Check if version matches v{x.x.x} format
	const versionRegex = /^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;
	if (versionRegex.test(version)) {
		return version;
	}

	console.error(`Invalid transport version format: ${version}`);
	console.error(`Transport version must be either "latest", "v1.2.3", or "v1.2.3-prerelease1"`);
	process.exit(1);
}

// Parse transport version from command line arguments
export function parseTransportVersion() {
	const args = process.argv.slice(2);
	let transportVersion = "latest"; // Default to latest

	// Find --transport-version argument
	const versionArgIndex = args.findIndex((arg) => arg.startsWith("--transport-version"));

	if (versionArgIndex !== -1) {
		const versionArg = args[versionArgIndex];

		if (versionArg.includes("=")) {
			// Format: --transport-version=v1.2.3
			transportVersion = versionArg.split("=")[1];
		} else if (versionArgIndex + 1 < args.length) {
			// Format: --transport-version v1.2.3
			transportVersion = args[versionArgIndex + 1];
		}

		// Remove the transport-version arguments from args array so they don't get passed to the binary
		if (versionArg.includes("=")) {
			args.splice(versionArgIndex, 1);
		} else {
			args.splice(versionArgIndex, 2);
		}
	}

	return { version: validateTransportVersion(transportVersion), remainingArgs: args };
}
