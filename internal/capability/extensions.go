package capability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/internal/tools"
	ws "github.com/simon/mneme/internal/workspace"
)

// Extension manifest type is defined in the tools package to avoid
// duplication between capability discovery and tools lifecycle management.
// See tools.ExtensionManifest for the canonical definition.

// ExtensionConfigLoader returns the saved configuration for an extension by
// name. It is invoked once per launched extension and delivered through the
// extension.configure protocol. A nil loader disables config delivery.
type ExtensionConfigLoader func(name string) (map[string]interface{}, error)

// ExtensionFilter reports whether the extension whose manifest name is name is
// disabled by config and must be skipped. A nil filter enables every
// extension.
type ExtensionFilter func(name string) bool

// ExtensionLoadOptions is the single value Bootstrap uses to configure how
// runtime extensions are loaded: whether a warm handshake cache lets an
// extension be declared without a process, how long an idle process may live,
// and where the cache lives.
//
// CacheDir is computed by the caller (internal/boot derives it from the
// workspace data dir) so this package never guesses at the workspace layout. An
// empty CacheDir disables the cache, which makes every extension eager.
type ExtensionLoadOptions struct {
	// Lazy declares extensions with a valid cache entry without starting them;
	// the process starts on first tool use. False keeps the eager path: every
	// discovered extension is started at boot.
	Lazy bool
	// IdleTimeout is how long a lazily started extension may sit unused before
	// the registry stops it. Zero disables reaping.
	IdleTimeout time.Duration
	// CacheDir is the handshake cache directory. Empty disables the cache.
	CacheDir string
}

// NewExtensionFilter builds an ExtensionFilter from the configured
// [bundles] disabled id list. An extension matches when either its manifest
// name or its capability-set id (ExtensionSetID(name)) is listed, so both
// `disabled = ["todos"]` and `disabled = ["extension:todos"]` work. Entries
// that match nothing are harmless: a stale id in config must never fail boot.
//
// The filter is built here from plain strings rather than from
// bundle/config types because internal/bundle imports this package, so the
// dependency must not point the other way.
func NewExtensionFilter(disabled []string) ExtensionFilter {
	if len(disabled) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(disabled))
	for _, id := range disabled {
		if id = strings.TrimSpace(id); id != "" {
			ids[id] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return func(name string) bool {
		return ids[name] || ids[ExtensionSetID(name)]
	}
}

// SetExtensionFilter records the configured disabled-extension filter so the
// install-time load path can warn about an extension that will not start at
// the next boot. Passing nil clears it. Safe on a nil receiver.
func (r *CapabilityRegistry) SetExtensionFilter(f ExtensionFilter) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extensionFilter = f
}

// ExtensionFilter returns the configured disabled-extension filter, or nil
// when every extension is enabled. Safe on a nil receiver.
func (r *CapabilityRegistry) ExtensionFilter() ExtensionFilter {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.extensionFilter
}

// discoverExtensions scans one or more extensions directories for extensions,
// optionally builds each one, launches it as an extension subprocess, and
// registers its tools and agents into the registry.
//
// Directories are tried in order. If a directory doesn't exist, it is silently
// skipped. This allows the same code to work in development (project-root
// extensions/) and production (workspace extensions/). Per-extension failures
// are logged and skipped: one broken extension must not abort discovery.
//
// The traversal is tools.FindExtensionDirs, the same one the UI's extension
// list uses, so bundled (<root>/<name>) and UI-installed
// (<root>/<category>/<name>) extensions are both found.
//
// Extensions the configured filter rejects are skipped before anything is
// built or launched: a disabled extension must not run, and building an
// artifact that will never load would only waste time (and could fail the
// boot).
//
// Discovery runs in two phases: this serial pass resolves manifests, applies the
// filter and auto-builds missing binaries (builds are heavy, so they are not
// overlapped), decides lazy versus eager from the handshake cache, and the
// second phase starts every remaining eager extension with bounded concurrency
// (see launchExtensions).
func discoverExtensions(reg *CapabilityRegistry, dirs []string, loadCfg ExtensionConfigLoader, filter ExtensionFilter, opts ExtensionLoadOptions, log *slog.Logger) {
	var pending []pendingExtension
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		for _, extDir := range tools.FindExtensionDirs(dir) {
			manifestPath := filepath.Join(extDir, tools.ExtensionManifestFile)
			mf, err := tools.ParseManifest(manifestPath)
			if err != nil {
				log.Warn("extension manifest invalid", "dir", extDir, "error", err)
				continue
			}

			// A disabled extension is skipped before its binary is resolved,
			// so its auto-build never runs either.
			if filter != nil && filter(mf.Name) {
				log.Info("extension disabled by config, skipping",
					"name", mf.Name, "set_id", ExtensionSetID(mf.Name), "dir", extDir)
				continue
			}

			log.Info("extension discovered", "name", mf.Name, "version", mf.Version, "category", mf.Category)

			// Determine the binary path. The binary field may be a simple name
			// (relative to the extension dir) or an interpreter command.
			binaryPath := resolveExtensionBinary(extDir, mf.Binary)

			// Runtime discovery loads pre-built binaries. In a development
			// source tree (the extension dir contains a go.mod and a build
			// command), we auto-build once so `wails dev` works without a
			// separate pre-compile step. Production binaries are already
			// present, so the build is never triggered there.
			if !binaryExists(binaryPath) {
				if mf.Build != "" && fileExists(filepath.Join(extDir, "go.mod")) {
					log.Info("building extension from source", "name", mf.Name, "command", mf.Build)
					if err := buildExtension(extDir, mf.Build); err != nil {
						log.Warn("extension build failed", "name", mf.Name, "error", err)
						continue
					}
					binaryPath = resolveExtensionBinary(extDir, mf.Binary)
				} else {
					if mf.Build != "" {
						log.Warn("extension binary not found; build it via cmd/build-extensions before loading",
							"name", mf.Name, "binary", binaryPath)
					} else {
						log.Warn("extension binary not found", "name", mf.Name, "binary", binaryPath)
					}
				}
			}

			if !binaryExists(binaryPath) {
				log.Warn("extension binary not found", "name", mf.Name, "binary", binaryPath)
				continue
			}

			// The cache key covers the extension's own bytes (manifest plus the
			// binary, or every file for an interpreted extension). It is computed
			// for both paths: to look for a lazy declaration, and so a successful
			// eager launch can record one for the next boot.
			var cacheKey string
			if opts.CacheDir != "" {
				if key, ok := tools.ExtensionCacheKey(extDir, manifestPath, binaryPath); ok {
					cacheKey = key
				}
			}

			// `eager: true` in the manifest opts an extension out of lazy
			// activation: it needs its process running for work that is not a
			// tool call.
			if opts.Lazy && cacheKey != "" && !mf.Eager {
				if rec, hit := tools.LoadExtensionCache(opts.CacheDir, cacheKey); hit {
					declareLazyExtension(reg, mf, binaryPath, cacheKey, rec, opts, loadCfg, log)
					continue
				}
			}

			pending = append(pending, pendingExtension{
				name:     mf.Name,
				binary:   binaryPath,
				manifest: mf,
				cacheKey: cacheKey,
			})
		}
	}

	launchExtensions(reg, pending, loadCfg, opts, log)
}

// pendingExtension is an extension that passed discovery and is ready to start.
type pendingExtension struct {
	name     string
	binary   string
	manifest *tools.ExtensionManifest
	// cacheKey is the handshake cache key computed during discovery, or "" when
	// no cache is configured. A successful launch writes the record under it.
	cacheKey string
}

// launchConcurrency bounds how many extension processes are started at once.
// Every extension is an independent process that only handshakes at startup, so
// overlapping them turns the boot cost from the sum of all handshakes into
// roughly one batch. It is bounded because each start is a process spawn plus a
// short read of its binary, and a machine with dozens of extensions should not
// fork them all at once.
const launchConcurrency = 8

// launchExtensionFn starts one extension. It is a variable so tests can observe
// scheduling without spawning processes.
var launchExtensionFn = launchExtensionProc

// launchExtensions starts every discovered extension with bounded concurrency
// and waits for all of them. One extension failing never prevents the others
// from loading; each failure is logged with the same info/warn split as before.
//
// Each successful launch is recorded in the handshake cache, so the next boot
// can declare that extension lazily even when this one was eager. A cache write
// failure is logged and never fails the launch.
func launchExtensions(reg *CapabilityRegistry, pending []pendingExtension, loadCfg ExtensionConfigLoader, opts ExtensionLoadOptions, log *slog.Logger) {
	if len(pending) == 0 {
		return
	}
	sem := make(chan struct{}, launchConcurrency)
	var wg sync.WaitGroup
	for _, p := range pending {
		wg.Add(1)
		go func(p pendingExtension) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			proc, err := launchExtensionFn(reg, p.binary, p.manifest, loadCfg, log)
			if err != nil {
				logExtensionLaunchFailure(log, p, err)
				return
			}
			cacheExtensionHandshake(proc, p, opts, log)
		}(p)
	}
	wg.Wait()
}

// cacheExtensionHandshake records a successful eager launch so the next boot can
// declare the extension from the cache instead of starting it. proc is nil for
// the test seam that stubs the launch.
func cacheExtensionHandshake(proc *tools.ProtoProcess, p pendingExtension, opts ExtensionLoadOptions, log *slog.Logger) {
	if proc == nil || opts.CacheDir == "" || p.cacheKey == "" {
		return
	}
	rec := proc.CacheRecord(p.cacheKey)
	if rec == nil {
		return
	}
	if err := tools.SaveExtensionCache(opts.CacheDir, rec); err != nil {
		log.Warn("extension handshake cache write failed", "name", p.name, "error", err)
	}
}

// declareLazyExtension registers an extension without starting its process: the
// cached handshake supplies the tool and agent surface, and the first call
// through the returned transport starts the process. OnStarted/OnStopped keep
// the capability set's Started flag (and the health monitor) in step with the
// process as it starts and stops on demand.
func declareLazyExtension(reg *CapabilityRegistry, mf *tools.ExtensionManifest, binaryPath, cacheKey string, rec *tools.CachedExtension, opts ExtensionLoadOptions, loadCfg ExtensionConfigLoader, log *slog.Logger) {
	loadConfig := func() (map[string]interface{}, error) {
		if loadCfg == nil {
			return nil, nil
		}
		return loadCfg(mf.Name)
	}
	lp := tools.NewLazyProcess(tools.LazyProcessConfig{
		Name:       mf.Name,
		Command:    binaryPath,
		Manifest:   rec.Manifest,
		Tools:      rec.Tools,
		Agents:     rec.Agents,
		Log:        log,
		LoadConfig: loadConfig,
		CacheDir:   opts.CacheDir,
		CacheKey:   cacheKey,
	})
	set := &CapabilitySet{
		ID:          ExtensionSetID(mf.Name),
		Name:        mf.Name,
		Kind:        KindExtension,
		Description: mf.Description,
		Health:      HealthOK,
		Enabled:     true,
		// Declared, no process yet: the UI shows "starts on demand" rather than
		// "running" or "down".
		Lazy:    true,
		Started: false,
	}
	if _, err := reg.RegisterExtension(set.ID, set, lp, lp.Tools(), lp.Agents()); err != nil {
		log.Warn("lazy extension registration failed", "name", mf.Name, "error", err)
		_ = lp.Stop()
		return
	}
	lp.SetOnStarted(func() { reg.markExtensionStarted(set.ID) })
	lp.SetOnStopped(func() { reg.markExtensionStopped(set.ID) })
	log.Info("extension declared lazily",
		"name", mf.Name, "tools", len(rec.Tools), "agents", len(rec.Agents), "cache_key", cacheKey)
}

// logExtensionLaunchFailure keeps the original severity split: elevation
// requests and missing interpreters are expected on some platforms, everything
// else is a warning worth investigating.
func logExtensionLaunchFailure(log *slog.Logger, p pendingExtension, err error) {
	errStr := err.Error()
	if strings.Contains(errStr, "requires elevation") ||
		strings.Contains(errStr, "exec format error") ||
		strings.Contains(errStr, "no such file") {
		log.Info("extension skipped", "name", p.name, "reason", errStr)
		return
	}
	log.Warn("extension start failed", "name", p.name, "binary", p.binary, "error", err)
}

// launchExtensionProc starts one extension binary via the JSON-RPC protocol,
// collects its tools and agents, and registers it as a single effect. It is
// the single loading path shared by manifest discovery, legacy script
// discovery, and on-demand (install-time) loading. mf is the pre-parsed
// manifest when the caller discovered one on disk; nil means the manifest
// comes from the process handshake.
func launchExtensionProc(reg *CapabilityRegistry, binaryPath string, mf *tools.ExtensionManifest, loadCfg ExtensionConfigLoader, log *slog.Logger) (*tools.ProtoProcess, error) {
	// The spawn goes through the shared seam, so tests in either package can
	// count spawns (eager and lazy alike) without starting a process.
	proc, err := tools.StartExtensionProcess(context.Background(), binaryPath, log)
	if err != nil {
		return nil, err
	}
	if mf == nil {
		// No on-disk manifest (legacy script discovery): the process
		// handshake reports its own identity.
		mf = &tools.ExtensionManifest{
			Name:        proc.Manifest.Name,
			Version:     proc.Manifest.Version,
			Description: proc.Manifest.Description,
		}
	}

	// Install-time path: the user just installed an extension that config
	// disables. Refusing to load it would look like a failed install, so load
	// it for this session and say plainly that it will not start again after a
	// restart (discovery skips it, and its auto-build is skipped too).
	//
	// Discovery never reaches this branch for a disabled extension: it skips
	// before resolving or building the binary.
	if f := reg.ExtensionFilter(); f != nil && f(mf.Name) {
		log.Warn("extension is disabled by config; loaded for this session but will not run at the next start",
			"name", mf.Name, "set_id", ExtensionSetID(mf.Name))
	}

	// Deliver the extension's saved configuration. This is the host half of the
	// extension.configure protocol (the extension half is extsdk's
	// SetConfigHandler); without it, saved per-extension config was written to
	// disk and never reached the running extension. Failure is non-fatal: the
	// extension may work with defaults, and the reason is logged.
	if loadCfg != nil {
		if cfg, cfgErr := loadCfg(mf.Name); cfgErr != nil {
			log.Warn("extension config load failed", "name", mf.Name, "error", cfgErr)
		} else if len(cfg) > 0 {
			if cfgErr := proc.Configure(context.Background(), cfg); cfgErr != nil {
				log.Warn("extension configure failed", "name", mf.Name, "error", cfgErr)
			} else {
				log.Info("extension configured", "name", mf.Name, "keys", len(cfg))
			}
		}
	}

	// Collect tools and agents up front so the whole extension can be
	// registered as a single effect (RegisterExtension reserves the set ID
	// first and returns a dispose func for clean unwinding). A tool-listing
	// failure means the extension is not usable: stop it and report rather
	// than registering a "loaded" extension that provides nothing.
	extTools, err := proc.ListTools(context.Background())
	if err != nil {
		proc.Stop()
		return nil, fmt.Errorf("list tools for extension %s: %w", mf.Name, err)
	}
	var extAgents []*tools.AgentDef
	if as, err := proc.ListAgents(context.Background()); err == nil {
		for _, a := range as {
			aCopy := a
			extAgents = append(extAgents, &aCopy)
		}
	} else {
		log.Warn("extension list agents failed", "name", mf.Name, "error", err)
	}

	set := &CapabilitySet{
		ID:          ExtensionSetID(mf.Name),
		Name:        mf.Name,
		Kind:        KindExtension,
		Description: mf.Description,
		Health:      HealthOK,
		Enabled:     true,
		// The eager path has a live process from this moment.
		Started: true,
	}
	if _, err := reg.RegisterExtension(set.ID, set, proc, extTools, extAgents); err != nil {
		proc.Stop()
		return nil, err
	}
	log.Info("extension loaded", "name", mf.Name, "tools", len(extTools), "agents", len(extAgents))
	return proc, nil
}

// resolveExtensionBinary resolves the binary path from the manifest's binary field.
// If the binary field is a multi-word command (e.g. "python3 script.py"), return
// it as-is. If it's a simple name, resolve relative to the extension directory.
// On Windows, appends .exe when binary has no extension (Go builds produce .exe).
func resolveExtensionBinary(extDir, binary string) string {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return ""
	}
	// Multi-word: interpreter + script (e.g. "python3 myskill.py").
	if strings.Contains(binary, " ") {
		parts := strings.Fields(binary)
		// Resolve the last argument (the script) relative to the extension dir.
		script := parts[len(parts)-1]
		if !filepath.IsAbs(script) {
			parts[len(parts)-1] = filepath.Join(extDir, script)
		}
		return strings.Join(parts, " ")
	}
	// Simple name or path.
	var result string
	if filepath.IsAbs(binary) {
		result = binary
	} else {
		result = filepath.Join(extDir, binary)
	}
	// On Windows: append .exe if the binary has no extension and the .exe file exists.
	if runtime.GOOS == "windows" && filepath.Ext(result) == "" {
		if _, err := os.Stat(result + ".exe"); err == nil {
			return result + ".exe"
		}
	}
	return result
}

// binaryExists returns true if the path refers to an existing regular file.
// For interpreter commands (containing spaces), checks the script file.
// On Windows, also checks path.exe if the path has no extension.
func binaryExists(path string) bool {
	if path == "" {
		return false
	}
	// For "python3 script.py", check script.py.
	if strings.Contains(path, " ") {
		parts := strings.Fields(path)
		path = parts[len(parts)-1]
	}
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return true
	}
	// On Windows, Go builds produce .exe files.
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		info, err := os.Stat(path + ".exe")
		return err == nil && !info.IsDir()
	}
	return false
}

// fileExists reports whether the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// buildExtension runs the extension's build command inside its directory. It
// is only invoked for development source trees (go.mod present) where the
// binary is missing; production never reaches this path because binaries are
// pre-built and present. The command is executed through the platform shell so
// quoted flags (e.g. -ldflags="-s -w") are parsed correctly.
func buildExtension(extDir, buildCmd string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", buildCmd)
	}
	cmd.Dir = extDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// DefaultExtensionDirs returns the standard extension search paths for the
// given workspace, in priority order and de-duplicated by absolute path.
//
// It returns the first candidate directory that actually contains a
// manifest-backed extension — normally the production workspace copy — so the
// development source tree is not scanned as well and the same extension is
// never loaded twice. When no candidate has a manifest, every existing
// candidate is returned so script-only (legacy) extensions are still found;
// when none exists, the workspace path is returned so callers always have a
// sane default.
func DefaultExtensionDirs(workspace string) []string {
	var candidates []string
	if workspace != "" {
		candidates = append(candidates, ws.ExtensionsDir(workspace))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ws.DirExtensions))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "extensions"))
	}

	var existing []string
	seen := make(map[string]bool, len(candidates))
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		if seen[abs] {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[abs] = true
		if dirHasExtensionManifest(dir) {
			return []string{dir}
		}
		existing = append(existing, dir)
	}

	if len(existing) == 0 && len(candidates) > 0 {
		return []string{candidates[0]}
	}
	return existing
}

// dirHasExtensionManifest reports whether dir contains at least one extension
// (a subdirectory at any depth with a manifest.json).
func dirHasExtensionManifest(dir string) bool {
	return len(tools.FindExtensionDirs(dir)) > 0
}
