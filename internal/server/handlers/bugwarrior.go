package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/furan917/taskwarrior-web-portal/internal/views"
)

// Bugwarrior holds the bugwarrior-pull handler and its last-result state.
// bin is the resolved absolute path to the bugwarrior binary, empty if not found.
type Bugwarrior struct {
	Logger *slog.Logger
	bin    string

	mu     sync.Mutex
	result *views.SyncResult
}

// NewBugwarrior probes for the bugwarrior binary and returns a handler.
// It checks PATH first, then falls back to common install locations so it
// works in systemd services and Docker images where ~/.local/bin is absent
// from the process PATH but bugwarrior may still be installed there.
func NewBugwarrior(logger *slog.Logger) *Bugwarrior {
	return &Bugwarrior{Logger: logger, bin: findBugwarrior()}
}

// Available reports whether the bugwarrior binary was found at startup.
func (b *Bugwarrior) Available() bool { return b.bin != "" }

// Pull handles POST /bugwarrior-pull. Runs `bugwarrior pull`, stores the
// result, and returns the result partial for HTMX to swap in.
func (b *Bugwarrior) Pull(w http.ResponseWriter, r *http.Request) {
	ranAt := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.bin, "pull")
	out, err := cmd.CombinedOutput()

	res := &views.SyncResult{
		Output: fmt.Sprintf("Pulled at %s\n%s", ranAt, strings.TrimSpace(string(out))),
		OK:     err == nil,
	}

	if err != nil && b.Logger != nil {
		b.Logger.Warn("bugwarrior pull failed", "err", err, "bin", b.bin)
	}

	b.mu.Lock()
	b.result = res
	b.mu.Unlock()

	renderHTML(w, r, "BugwarriorResult", views.BugwarriorResultPartial(res), b.Logger)
}

// Result returns the most recent pull result (nil if never run).
func (b *Bugwarrior) Result() *views.SyncResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.result
}

// findBugwarrior returns the absolute path to the bugwarrior binary.
// Resolution order:
//  1. BUGWARRIOR_BIN env var (explicit override for Docker / custom installs)
//  2. PATH lookup
//  3. Common install locations (pip --user, pipx, system) for service
//     environments where ~/.local/bin is absent from the process PATH.
func findBugwarrior() string {
	if p := os.Getenv("BUGWARRIOR_BIN"); p != "" {
		if isExecutable(p) {
			return p
		}
	}

	if p, err := exec.LookPath("bugwarrior"); err == nil {
		return p
	}

	home, _ := os.UserHomeDir()

	candidates := []string{
		filepath.Join(home, ".local/bin/bugwarrior"),
		filepath.Join(home, ".local/pipx/venvs/bugwarrior/bin/bugwarrior"),
		"/usr/local/bin/bugwarrior",
		"/usr/bin/bugwarrior",
		"/opt/bugwarrior/bin/bugwarrior",
	}

	for _, p := range candidates {
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
