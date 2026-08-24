// Package shortcut creates desktop / start-menu shortcuts that launch
// Tingly Box with a double-click. It is callable from the CLI today and from
// a future HTTP handler, so it has no Kong / cobra / command-layer dependency.
package shortcut

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	windowsShortcutTemplate = template.Must(template.ParseFS(templateFS, "templates/windows_shortcut.ps1.tmpl"))
	macCommandTemplate      = template.Must(template.ParseFS(templateFS, "templates/macos_command.sh.tmpl"))
	linuxDesktopTemplate    = template.Must(template.ParseFS(templateFS, "templates/linux_desktop.desktop.tmpl"))
	linuxLauncherTemplate   = template.Must(template.ParseFS(templateFS, "templates/linux_launcher.sh.tmpl"))
)

// render executes a parsed template against data and returns the result.
// Errors here would only come from a template/data mismatch, i.e. a coding
// mistake caught by the package's own tests — not something callers need to
// handle at runtime, so we panic like template.Must does for parsing.
func render(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		panic(fmt.Sprintf("shortcut: template %s: %v", t.Name(), err))
	}
	return b.String()
}

// Launch sources. They describe how Tingly Box is installed/started and which
// command a shortcut should run.
const (
	SourceBinary    = "binary"
	SourceNpx       = "npx"
	SourceNpxBundle = "npx-bundle"
)

// NpxPackage returns the npm package name a given launch source installs
// from ("tingly-box", or "tingly-box-bundle" for the bundle wrapper; any
// other source gets the main package). It is the single mapping used both to
// build relaunch commands and to decide which registry entry a version check
// should query — the two must never diverge, or an update could pin a
// version the source's own package doesn't have.
func NpxPackage(source string) string {
	if source == SourceNpxBundle {
		return "tingly-box-bundle"
	}
	return "tingly-box"
}

// IsNpxSource reports whether source is one of the npx-based launch sources
// (SourceNpx or SourceNpxBundle) rather than a plain binary. This is the
// canonical "is this an npx launch" check — ResolveLaunchWith and the
// self-update package's CanOneClickUpdate both need exactly this predicate,
// and must never diverge on it (an update deciding "yes" while the launch
// spec builder decides "no" would relaunch as a plain binary invocation of a
// package spec, which isn't a valid command).
func IsNpxSource(source string) bool {
	return source == SourceNpx || source == SourceNpxBundle
}

// ResolveExePath returns this process's own executable path, with symlinks
// resolved. It is the "exePath" ResolveLaunch/ResolveLaunchWith expect for a
// binary-install LaunchSpec. Callers that need "our own binary path" (the
// `shortcut` command and self-update) previously each carried their own copy
// of this os.Executable+EvalSymlinks pair with a different error-handling
// policy; both now call this instead, so that policy can't drift between
// them by accident. ResolveLaunch itself still takes exePath as an explicit
// parameter rather than calling this internally, so it stays trivially
// testable without a real executable on disk.
func ResolveExePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return exePath, nil
}

// npxPackageForSource returns the npm package + version spec an npx-based
// launch should run. It pins to the currently-running version so the
// shortcut relaunches the exact build the user is already on — not whatever
// happens to be newest on the registry at double-click time — and so it
// works offline once npm has that version cached. version is empty (or the
// "dev"/"unknown" placeholders used by unversioned builds) falls back to
// "@latest".
func npxPackageForSource(source, version string) string {
	pkg := NpxPackage(source)
	if version == "" || version == "dev" || version == "unknown" {
		return pkg + "@latest"
	}
	return pkg + "@" + version
}

// LaunchArgs are the CLI args the shortcut runs: restart the daemon and
// (since --browser defaults to true) open the web UI.
func LaunchArgs() []string {
	return []string{"restart", "--daemon"}
}

// LaunchSpec describes how the shortcut should invoke Tingly Box on each
// platform. Argv is the POSIX-style command vector used for macOS .command and
// Linux .desktop entries; WinTarget/WinArgs are the .lnk TargetPath/Arguments.
type LaunchSpec struct {
	Argv      []string
	WinTarget string
	// WinArgs is WinArgv pre-joined into one string, for embedding into the
	// .lnk PowerShell template's Arguments field (which wants a single
	// string, not an argv slice).
	WinArgs string
	// WinArgv is the same arguments as WinArgs, as a real argv slice. Use
	// this — not WinArgs — for anything that spawns the command directly via
	// exec.Command: re-splitting WinArgs (e.g. strings.Fields) only works by
	// the accident of every token here being space-free, whereas WinArgv
	// carries the real boundaries no matter what a future argument contains.
	WinArgv []string
	WorkDir string
}

// Options controls which shortcuts get written.
type Options struct {
	Name      string
	NoDesktop bool
	NoMenu    bool
}

// ResolveLaunch decides whether the shortcut runs the binary directly or goes
// through npx, then builds the platform-specific launch vectors. source is how
// the *current* process was invoked (SourceNpx / SourceNpxBundle / anything
// else meaning a plain binary) — the caller always knows this first-hand, so
// there is no detection or persistence to do here. version pins an npx-based
// shortcut to the currently-running release (see npxPackageForSource).
func ResolveLaunch(exePath, source, version string) LaunchSpec {
	return ResolveLaunchWith(exePath, source, version, LaunchArgs())
}

// ResolveLaunchWith is ResolveLaunch with the CLI args made explicit, for
// callers that need extra flags on the relaunch (e.g. self-update passing
// --shortcut so the new version repins the launcher artifacts).
func ResolveLaunchWith(exePath, source, version string, args []string) LaunchSpec {
	if IsNpxSource(source) {
		// e.g. "npx -y tingly-box@1.4.2 restart --daemon"
		npxArgv := append([]string{"npx", "-y", npxPackageForSource(source, version)}, args...)
		cmdStr := strings.Join(npxArgv, " ")
		home, _ := os.UserHomeDir()

		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = "cmd.exe"
		}

		return LaunchSpec{
			// Wrap in a login shell so GUI launches pick up node/npx on PATH.
			Argv:      []string{"sh", "-lc", cmdStr},
			WinTarget: comspec,
			WinArgs:   "/c " + cmdStr,
			WinArgv:   append([]string{"/c"}, npxArgv...),
			WorkDir:   home,
		}
	}

	return LaunchSpec{
		Argv:      append([]string{exePath}, args...),
		WinTarget: exePath,
		WinArgs:   strings.Join(args, " "),
		WinArgv:   args,
		WorkDir:   filepath.Dir(exePath),
	}
}

// AnyExists reports whether any shortcut artifact for the given display name
// already exists at this platform's known locations. Callers use it to decide
// whether a refresh would touch anything (e.g. self-update only repins
// shortcuts the user actually has — it never creates new ones for users who
// never asked). Best-effort: Windows folder redirection (OneDrive) isn't
// resolved here, so a redirected Desktop can be missed — the cost is a
// skipped repin, never a wrongly-created file.
func AnyExists(name string) bool {
	var candidates []string

	switch runtime.GOOS {
	case "windows":
		// No Go-side path computation to share here: createWindowsShortcuts
		// resolves Desktop/Programs entirely inside the PowerShell template
		// via .NET's [Environment]::GetFolderPath (see windowsShortcutScript),
		// which is also what makes it OneDrive-redirection-aware — this
		// best-effort guess doesn't replicate that resolution.
		slug := slugName(name)
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, "Desktop", slug+".lnk"))
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", slug+".lnk"))
		}
	case "darwin":
		if path, err := macCommandPath(name); err == nil {
			candidates = append(candidates, path)
		}
	default:
		if path, err := linuxDesktopMenuPath(name); err == nil {
			candidates = append(candidates, path)
		}
		if path, err := linuxDesktopFilePath(name); err == nil {
			candidates = append(candidates, path)
		}
		if path, err := linuxLauncherScriptPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// Create dispatches to the platform-specific implementation and returns the
// paths of the shortcuts it created.
func Create(opts Options, spec LaunchSpec) ([]string, error) {
	switch runtime.GOOS {
	case "windows":
		return createWindowsShortcuts(opts, spec)
	case "darwin":
		return createMacShortcuts(opts, spec)
	default:
		return createLinuxShortcuts(opts, spec)
	}
}

// ---------------- Windows ----------------

func createWindowsShortcuts(opts Options, spec LaunchSpec) ([]string, error) {
	script := windowsShortcutScript(opts, spec)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create Windows shortcut: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	var created []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			created = append(created, line)
		}
	}
	return created, nil
}

// windowsShortcutScript renders internal/shortcut/templates/windows_shortcut.ps1.tmpl,
// a PowerShell script that resolves the Desktop and Start Menu Programs
// folders at runtime (handling OneDrive redirection) and writes a .lnk via
// the WScript.Shell COM object. It points TargetPath directly at the real
// command (the binary, or cmd.exe for the npx case) — deliberately not
// through a generated script that launches something with a hidden window.
// That's the standard shape of a malware dropper (write a .vbs, run it via
// wscript with window style 0) and real antivirus/SmartScreen heuristics
// flag it; VBScript is also being phased out on Windows. WindowStyle=7
// (start minimized) is the one mitigation that stays inside IShellLink's own
// documented, unsuspicious surface — it won't stop `cmd /c` from lingering
// on a terminal host with "close on exit" set to never, but it keeps the
// window out of the way while it's up. It prints each created path on its
// own line.
func windowsShortcutScript(opts Options, spec LaunchSpec) string {
	return render(windowsShortcutTemplate, struct {
		Target, Arguments, WorkDir, Name string
		IncludeDesktop, IncludeMenu      bool
	}{
		Target:         psQuote(spec.WinTarget),
		Arguments:      psQuote(spec.WinArgs),
		WorkDir:        psQuote(spec.WorkDir),
		Name:           psQuote(slugName(opts.Name)),
		IncludeDesktop: !opts.NoDesktop,
		IncludeMenu:    !opts.NoMenu,
	})
}

// psQuote wraps a string as a PowerShell single-quoted literal, escaping single
// quotes by doubling them.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ---------------- macOS ----------------

// createMacShortcuts only ever writes a Desktop shortcut. There's no macOS
// equivalent of a Start Menu for a plain .command script: Launchpad and
// Spotlight's "Applications" category only index real .app bundles (they
// read the bundle's Info.plist), so dropping a .command file into
// ~/Applications doesn't make it launchable from either — it would just be
// an inert, harder-to-find copy of the Desktop one. opts.NoMenu is a no-op
// here as a result (see .design/shortcut.md for the full writeup).
func createMacShortcuts(opts Options, spec LaunchSpec) ([]string, error) {
	if opts.NoDesktop {
		return nil, nil
	}

	path, err := macCommandPath(opts.Name)
	if err != nil {
		return nil, nil
	}
	content := commandScriptContent(spec.Argv)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return nil, fmt.Errorf("failed to write shortcut %s: %w", path, err)
	}
	return []string{path}, nil
}

// macCommandPath is the ~/Desktop/<slug>.command path createMacShortcuts
// writes and AnyExists checks for — one function so the two can't drift on
// the filename.
func macCommandPath(name string) (string, error) {
	dir, err := userSubdir("Desktop")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slugName(name)+".command"), nil
}

// commandScriptContent renders internal/shortcut/templates/macos_command.sh.tmpl,
// a macOS .command shell script that launches the binary. Double-clicking a
// .command file runs it in Terminal.app, which by default leaves the window
// open showing "[Process completed]" after the script exits — the user has
// to close it by hand every time. We close it for them on success
// (identifying "this" window by its tty so we don't touch any other open
// Terminal window); on failure the window stays open so the error is
// visible instead of vanishing.
func commandScriptContent(argv []string) string {
	return render(macCommandTemplate, struct{ Command string }{Command: shJoin(argv)})
}

// ---------------- Linux ----------------

func createLinuxShortcuts(opts Options, spec LaunchSpec) ([]string, error) {
	content := desktopEntryContent(opts.Name, spec.Argv)

	var targets []string
	if !opts.NoMenu {
		if path, err := linuxDesktopMenuPath(opts.Name); err == nil {
			if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
				targets = append(targets, path)
			}
		}
	}
	if !opts.NoDesktop {
		if path, err := linuxDesktopFilePath(opts.Name); err == nil {
			if _, statErr := os.Stat(filepath.Dir(path)); statErr == nil {
				targets = append(targets, path)
			}
		}
	}

	var created []string
	for _, path := range targets {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return created, fmt.Errorf("failed to write shortcut %s: %w", path, err)
		}
		created = append(created, path)
	}

	// A .desktop entry is only launchable from a graphical session, so on
	// Linux always also write a plain executable launcher script — on
	// headless boxes (servers, containers, SSH) it is the only artifact the
	// user can actually run, and detecting "headless" from the environment
	// (DISPLAY etc.) is unreliable enough that conditioning on it would just
	// make the command's output unpredictable.
	if !(opts.NoDesktop && opts.NoMenu) {
		path, err := writeLinuxLauncherScript(opts, spec)
		if err != nil {
			return created, err
		}
		if path != "" {
			created = append(created, path)
		}
	}
	return created, nil
}

// writeLinuxLauncherScript writes the executable launcher script to
// ~/.local/bin (commonly on PATH per the systemd file-hierarchy convention,
// and user-owned either way). An unresolvable home or an uncreatable
// directory skips the script rather than failing shortcut creation.
func writeLinuxLauncherScript(opts Options, spec LaunchSpec) (string, error) {
	path, err := linuxLauncherScriptPath(opts.Name)
	if err != nil {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(launcherScriptContent(spec.Argv)), 0o755); err != nil {
		return "", fmt.Errorf("failed to write shortcut %s: %w", path, err)
	}
	return path, nil
}

// linuxDesktopMenuPath, linuxDesktopFilePath and linuxLauncherScriptPath are
// the paths createLinuxShortcuts/writeLinuxLauncherScript write and AnyExists
// checks for — one function per artifact so the writer and the existence
// check can't drift on the filename or location.
func linuxDesktopMenuPath(name string) (string, error) {
	dir, err := userDataSubdir("applications")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slugName(name)+".desktop"), nil
}

func linuxDesktopFilePath(name string) (string, error) {
	dir, err := userSubdir("Desktop")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slugName(name)+".desktop"), nil
}

func linuxLauncherScriptPath(name string) (string, error) {
	dir, err := userSubdir(filepath.Join(".local", "bin"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slugName(name)+".sh"), nil
}

// desktopEntryContent renders internal/shortcut/templates/linux_desktop.desktop.tmpl,
// a freedesktop .desktop entry.
func desktopEntryContent(name string, argv []string) string {
	return render(linuxDesktopTemplate, struct{ Name, Exec string }{Name: name, Exec: shJoin(argv)})
}

// launcherScriptContent renders internal/shortcut/templates/linux_launcher.sh.tmpl,
// a plain shell script that exec's the launch command.
func launcherScriptContent(argv []string) string {
	return render(linuxLauncherTemplate, struct{ Command string }{Command: shJoin(argv)})
}

// ---------------- shared helpers ----------------

// slugName converts a display name like "Tingly Box" into the space-free
// base filename every generated shortcut uses ("tingly-box"), regardless of
// platform. Filenames with spaces need extra quoting wherever they're
// referenced (shell commands, other generated scripts, path arguments) and
// are an easy source of subtle bugs; the display "Name" (used inside a
// .desktop entry's Name= field, or just passed via --name) is free to keep
// spaces since it's never used as a path component.
func slugName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}

func userSubdir(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, name), nil
}

func userDataSubdir(name string) (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, name), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", name), nil
}

// shQuote wraps a string as a POSIX single-quoted literal.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shQuote(a)
	}
	return strings.Join(quoted, " ")
}
