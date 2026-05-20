# taskwarrior-web-portal

Local-only web UI for Taskwarrior 3.x. Single Go binary, served on `127.0.0.1:5050`. Auto-refreshes the list view every 30s; pauses while you're editing so mid-flight inputs aren't clobbered.

<video src="https://github.com/user-attachments/assets/fa2632bf-6050-426f-a0d6-85130952087e" autoplay loop muted playsinline width="800"></video>

## Install from a release

No Go toolchain needed. Detects your OS and architecture, downloads the right binary, and installs it as a user service:

```sh
curl -fsSL https://raw.githubusercontent.com/furan917/taskwarrior-web-portal/main/scripts/get.sh | sh
```

Supports macOS (Intel + Apple Silicon) and Linux (amd64 + arm64). Requires `task` to already be on `$PATH`.

## Stack

- Go 1.25 stdlib `net/http` (pattern routing, no router framework).
- [templ](https://templ.guide/) for typed HTML.
- [HTMX 2.x](https://htmx.org/) for in-place updates without an SPA.
- [Tailwind v4](https://tailwindcss.com/) standalone CLI (no Node).
- [flatpickr 4.6.13](https://flatpickr.js.org/) vendored under `web/static/vendor/flatpickr/` for the date/time pickers.
- macOS launchd / Linux systemd `--user` for auto-start at login.

No Docker, no Node, no database server. Reads/writes go through the existing `task` CLI on the host, which serialises against `~/.task/taskchampion.sqlite3`.

## Build

```sh
make build           # one-shot: templ generate + tailwindcss + go build
```

Output: `bin/taskwarrior-web-portal` (~7 MB stripped binary, all assets embedded).

## Run

```sh
./bin/taskwarrior-web-portal                  # foreground
make run                               # alias for the above
open http://127.0.0.1:5050             # in browser
```

## Configuration

All settings are optional environment variables. Defaults work for a standard desktop install.

| Variable | Default | Description |
|---|---|---|
| `TWP_BIND_HOST` | `127.0.0.1` | Address to listen on. Set to `0.0.0.0` for container or LAN access. |
| `TWP_BIND_PORT` | `5050` | Port to listen on. |
| `TWP_ALLOWED_HOSTS` | _(none)_ | Comma-separated bare hostnames or IPs that are allowed to reach the portal (e.g. `192.168.1.10,myhostname`). The port is appended automatically from `TWP_BIND_PORT` - do not include it here. |
| `BUGWARRIOR_BIN` | _(auto-detect)_ | Absolute path to the `bugwarrior` binary. Only needed when bugwarrior is installed in a custom location (e.g. a virtualenv). Auto-detected via `PATH` and common install directories (`~/.local/bin`, `~/.local/pipx/venvs/bugwarrior/bin`, `/usr/local/bin`, etc.) when unset. |

`TWP_BIND_HOST` and `TWP_BIND_PORT` are the only knobs for the service itself. `TWP_ALLOWED_HOSTS` is only needed when you access the portal from a different machine or hostname — the defaults (`localhost` and `127.0.0.1`) cover a standard single-machine install.

### /config page

The gear icon in the top-right of the header opens a read-only configuration page showing your effective Taskwarrior and portal settings: version, data directory, sync server URL and client ID, date format, time-tracking status, hooks, UDAs, active context, and the portal's effective bind address and allowed hosts. Sensitive values (encryption secrets) are never shown.

## Install as a user service (auto-start at login)

```sh
make install
```

The script branches on `uname -s`:

**macOS** (any version with launchd, i.e. all):

1. Copies `bin/taskwarrior-web-portal` to `~/.local/bin/`.
2. Renders `deploy/local.taskwarrior-web-portal.plist.tmpl` into `~/Library/LaunchAgents/`.
3. Bootstraps it via `launchctl bootstrap gui/$(id -u)`.
4. Creates `~/Library/Logs/taskwarrior-web-portal/`.
5. Enforces `chmod 700 ~/.task`.
6. Curls `/healthz` to verify it's listening.

Restarts on crash (`KeepAlive` on `Crashed=true`); stays down when stopped cleanly via `launchctl bootout`.

**Linux** (any systemd-based distro - Ubuntu, Debian, Arch, RHEL, Fedora, openSUSE, Manjaro, etc.):

1. Copies `bin/taskwarrior-web-portal` to `~/.local/bin/`.
2. Renders `deploy/taskwarrior-web-portal.service.tmpl` into `~/.config/systemd/user/`.
3. Enables + starts it via `systemctl --user enable --now taskwarrior-web-portal`.
4. Creates `${XDG_STATE_HOME:-~/.local/state}/taskwarrior-web-portal/`.
5. Enforces `chmod 700 ~/.task`.
6. Curls `/healthz` to verify it's listening.

Restarts on crash (`Restart=on-failure`); stays down when stopped cleanly via `systemctl --user stop`. Service starts at login by default; for "running before any login", run `sudo loginctl enable-linger $USER` once.

View logs with `journalctl --user -u taskwarrior-web-portal -f` (last few lines from systemd's panic safety net) or tail `${XDG_STATE_HOME:-~/.local/state}/taskwarrior-web-portal/app.log` (the rotated structured slog stream).

Non-systemd Linux (Alpine, Void, Devuan, Slackware) is not supported by the install script - run `bin/taskwarrior-web-portal &` and supervise it with whatever process manager you prefer.

To also append a `tw` alias that opens `http://127.0.0.1:5050`, opt in via
`INSTALL_ALIAS`:

| Value | Behaviour |
|---|---|
| `1` / `auto` | Detect login shell from `$SHELL`, write to that config only |
| `zsh` | Write to `~/.zshrc` |
| `bash` | Write to `~/.bash_profile` (macOS) or `~/.bashrc` (Linux) |
| `fish` | Write to `~/.config/fish/config.fish` |
| `all` | Write to every shell config above |

Examples:

```sh
INSTALL_ALIAS=1 make install            # auto: matches your login shell
INSTALL_ALIAS=fish make install         # explicit
INSTALL_ALIAS=all make install          # every shell at once
```

The alias resolves the URL-opener at install time: macOS gets `open
http://...`, Linux gets `xdg-open http://...`. Off by default so a fresh
install never silently mutates a shell config; idempotent on re-run.

`make uninstall` strips the alias from every known shell config defensively,
regardless of which one the install used.

## Uninstall

```sh
make uninstall
```

Stops the service (launchd `bootout` on macOS, `systemctl --user disable --now` on Linux), removes the plist or systemd unit, removes the binary from `~/.local/bin/`, and strips the `tw` alias from any shell config it can find. Logs (`~/Library/Logs/taskwarrior-web-portal/` on macOS, `${XDG_STATE_HOME:-~/.local/state}/taskwarrior-web-portal/` on Linux) are preserved so you can review the historical record after removal.

## Develop

```sh
make dev
```

Runs three processes in parallel:

- `templ generate --watch` (recompiles templates on save)
- `tailwindcss --watch` (recompiles CSS on class-name changes)
- `go run .` (the server)

Edit `.templ` / `.go` / `.css` / `web/static/js/*.js` files and the running binary picks up changes.

## Verify

```sh
make check     # generators run cleanly with non-empty outputs
make test      # go test ./...
```

A handful of smoke tests in `internal/tw/client_test.go` shell out to the real `task` binary; each one skips cleanly when `task` is not on `$PATH`, and the read-side smokes additionally skip when the user has no tasks. Anything that mutates state creates a throwaway task, exercises the path under test, and deletes/purges the task in a `defer`, so running the suite never leaves residue in your taskrc.

## Security notes

- **Bind**: explicit `tcp4` to the configured address (default `127.0.0.1:5050`). Controlled by `TWP_BIND_HOST` and `TWP_BIND_PORT`. The allowed-host and origin allowlists are derived from the same `bindPort()` call so the port can never drift between the listener and the middleware. `TWP_ALLOWED_HOSTS` accepts bare hostnames only — the port is appended automatically, so `myhostname:5050` and `myhostname:9000` cannot co-exist if only one port is configured.
- **CSRF**: double-submit cookie + `X-CSRF-Token` header (auto-injected by `web/static/js/core_csrf.js` on every HTMX request from the page). `SameSite=Strict`, HttpOnly, 32 random bytes.
- **Command injection**: `tw.AddInput.AddArgs()` always wraps user-supplied description in `description:"<text>"` form. Tokens like `+urgent due:tomorrow rc.data.location=/tmp/x` are stored as literal text, not interpreted as DOM modifiers. `tw.guardArgs` rejects any caller-supplied `rc.*` arg as defence-in-depth.
- **Hostile taskrc**: a context's read filter is composed into argv as `(filter)` for export, which would otherwise let `rc.*` tokens slip past `guardArgs`. `tw.Context.SafeReadFilter()` scans for `rc.*` tokens at point of use and returns empty (no context clause) if any are present.
- **Subprocess**: `context.WithTimeout` per invocation (10s default; bulk handlers get 30s; `_context` lookup gets 2s; all centralised in `internal/config`). 64 MB `io.LimitReader` cap on stdout. Stderr captured into a 4 KiB bounded buffer attached to a typed `*tw.TaskExitError` for in-process classification only - never logged.
- **Validation errors**: typed `*tw.ValidationError{Field, Value, Reason}` instead of string-prefix matched messages, so renaming a message can't silently break field-level highlighting in the UI.
- **Length cap**: descriptions and annotations are bounded to 4 KiB (`tw.MaxDescriptionBytes`) so a multi-megabyte payload can't blow past the platform's argv limit.
- **Logs**: live in `~/Library/Logs/taskwarrior-web-portal/` with mode 700 - never `/tmp` (world-readable on macOS). Logs include only method, path, status, duration, request-id; never form bodies, query strings on writes, or task descriptions.

### Logs

- **Primary**: `~/Library/Logs/taskwarrior-web-portal/app.log` - structured slog text output, size-rotated by [lumberjack](https://github.com/natefinch/lumberjack) baked into the binary. Policy: 10 MB per file, 3 backups, gzip-compressed, 30 day max age. Active file is `app.log`; rotated files become `app-<timestamp>.log.gz`.
- **Panic safety net**: `out.log` and `err.log` are captured by launchd via the plist's `StandardOutPath` / `StandardErrorPath` and are NOT rotated. `err.log` should stay near-empty under normal operation (only Go runtime panics go to stderr). `out.log` will mirror `app.log` because the binary fans slog output through `io.MultiWriter(os.Stdout, app.log)`; if it grows uncomfortably between launchd cycles, truncate it manually with `: > ~/Library/Logs/taskwarrior-web-portal/out.log` - the binary will reopen on next write. The rotated `app.log` is the source of truth; `out.log` exists so anything written before slog initialises still leaves a trace.

## Dark mode

The UI follows the OS `prefers-color-scheme` setting by default. A sun/moon button in the top-right of the header overrides and persists the choice in `localStorage` ("light" or "dark"); clear it to revert to OS-driven behaviour. The `.dark` class is applied to `<html>` synchronously by `web/static/theme.js` (loaded in `<head>` before paint) so there's no flash of light content on reload. The 4-tier WCAG-AA palette (Blue/Yellow/Orange/Red for urgency) and the calendar's solid-due-day vs light-scheduled-day distinction are preserved in both modes - dark mode inverts the "light" tier (very dark fill, light text, visible ring) so the brightness-contrast cue between scheduled and due chips remains colourblind-safe.

## Keybindings

CLI-friendly navigation. All bindings are inert while a text input/textarea is focused or a dialog is open.

| Key     | Action                                                |
| ------- | ----------------------------------------------------- |
| `n`     | Open the add-task modal                               |
| `j`     | Focus the next task in the list                       |
| `k`     | Focus the previous task                               |
| `Enter` | Edit the focused task (opens modal)                   |
| `x`     | Mark the focused task done (with confirm)             |
| `Space` | Toggle the focused row's bulk-select checkbox         |
| `*`     | Select every visible row                              |
| `Esc`   | Close any open dialog, or clear the bulk selection    |
| `?`     | Show the keybindings help dialog                      |

Row focus is preserved across the 30s poll/HTMX swap by task UUID; if the focused task is gone, focus snaps to the first row. Space's fallback (no current focus) dispatches a `taskwarrior:focus-row` CustomEvent so the row it just toggled becomes the focused one and `j`/`k` continues from there.

The keybinding list is the single source of truth for both the help dialog and the small footer cheat-sheet (`internal/views/keybindings.go`).

## Undo

The right-hand side of the nav has an **Undo** button that reverses the last Taskwarrior change (it shells `task undo`). A styled confirmation dialog asks first; on accept the call posts to `POST /undo` and the list refreshes via `HX-Trigger: refresh`. Repeated clicks walk further back through the undo log. Taskwarrior's normal interactive prompt is bypassed by the `rc.confirmation=no` safety arg the server already prepends to every invocation.

## Row chrome

Each row in a list view has, left to right: bulk-select checkbox, mark-done circle (or "completed <when>" pill on the Done page), description + project / tag / due / blocked / waiting badges, urgency bar, **edit** (or **delete** on Done), and a chevron at the far right that expands the info panel. The same `rowFrame(t, done bool)` partial powers both pending and completed rows so the layout stays in sync; the only thing that varies is the action button (`edit` ↔ `delete`) and the absence of the done-circle on completed tasks.

The expanded info panel uses a 4-column grid on `sm:` and up (label / value / label / value) so short fields tile two-up. Date fields are explicitly column-pinned: future-facing dates (Due / Wait / Scheduled for start) on the left, provenance dates (Created / Modified) on the right, regardless of which optional fields are present. Long fields (Tags / Notes / Blocked by) span the full row. UDAs sit at the bottom of the panel under a thin grey rule, one row per UDA (Priority, plus any user-defined fields).

The expanded state is held client-side keyed by row id, so it survives the 30s polling refresh; rows that vanish from the list have their state pruned automatically.

## Bulk operations

Each row has a checkbox at the left. Tick one or more rows and an action bar appears at the top of the list:

- **Mark done** posts to `POST /tasks/bulk/done`.
- **Delete** posts to `POST /tasks/bulk/delete`.
- **Clear** drops the selection without touching any tasks.

The selection persists across the 30s polling refresh: rows that are still rendered are re-ticked after each swap, and ids whose rows have disappeared (because someone else completed/deleted the task) are pruned silently. Cap is 100 ids per request; the server rejects larger batches with 400. Individual failures inside a batch are logged but do not abort the rest of the batch.

## Search

The Next/Ready/Agenda/Forecast views render a search box top-right of the nav. Typing case-insensitive substring matches against description, project, and tags; results stream in via HTMX with a 200ms debounce on `input` events. The filter is server-side (the partial endpoint accepts `?q=...`) and is preserved across the 30s poll - `core_refresh.js` mirrors the current `q` into `#task-list`'s `hx-get` after each swap so the next poll keeps the filter applied. Clear the box to restore the full list. Search is intentionally absent from project/tag drilldowns and the Calendar/Browse pages.

## Sorting

Every list view (Next, Ready, Agenda, Forecast, plus project and tag drill-downs) renders a row of column links above the task list: Urgency, Due, Project, Description, Created. Clicking one of these:

- switches to that key (using its natural default direction) when it isn't currently active, or
- flips the direction (asc <-> desc) when the column is already active.

Defaults: urgency desc, due asc (earliest first), project asc, description asc, entry/created desc (newest first). Tasks without a `due` date always sort last regardless of direction so scheduled work floats to the top.

State lives in `?sort=<key>[:<dir>]` on the partial endpoint - omit it for the default urgency-desc order. After each HTMX swap, `sort.js` mirrors the current `?sort=` into `#task-list`'s `hx-get` so the 30s poll keeps the chosen order. Same pattern as the search-sync handler; the two are independent modules.

## Calendar

`/calendar` renders open tasks (pending or waiting) on a Monday-Sunday grid with three modes: **Month** (default), **Week**, **Day**. Switch via the buttons top-right; step periods with the prev/next arrows; click a day number to drill into Day view.

Tasks appear when they have a `due` date:

- **Multi-day chip** when both `scheduled` and `due` are set (or `wait` and `due`, falling back if there's no `scheduled`): one chip per day in the span, with the corners rounded only at the start day and the end day so the row reads as a continuous bar.
- **Single-day chip** when only `due` is set.
- **Not on the calendar** when `due` is empty.

Chip colour comes from urgency (red >= 8, amber >= 4, blue otherwise). Click any chip to open the existing edit modal. Month cells cap at 3 chips with a "+K more" link into Day view; Week and Day are uncapped.

Query params: `?view=<month|week|day>&date=YYYY-MM-DD`. Bad input returns 400.

## Done view

`/done` lists tasks completed in the last N days (default 14, configurable via `?days=N`, clamped 1-90), sorted by completion timestamp desc. Each row reuses the same `rowFrame` chrome as the live lists - same chevron, same expand panel, same completed-on pill - so navigating from Ready to Done feels continuous. The action button is **delete** (red on hover) so a stray completion can be purged from history; same `/tasks/{id}` DELETE endpoint as the edit modal.

## UDAs

User-Defined Attributes are discovered automatically from `~/.taskrc` and TTL-cached on the `tw.Client` (60s). Define one with:

```
uda.estimate.type=duration
uda.estimate.label=Estimate
```

The add/edit modals render an input for every UDA the next time the cache refreshes (or immediately on server restart). Values are submitted as `<name>:"<value>"` so embedded DOM tokens stay literal. Empty values on modify CLEAR the attribute; empty on add stays unset.

Type mapping for input controls:

- `string` -> `<input type="text">`
- `numeric` -> `<input type="number">` (validated as a float server-side)
- `date` -> `<input type="date">` (accepts YYYY-MM-DD plus Taskwarrior keywords like `tomorrow` / `due-3d`)
- `duration` -> `<input type="text">` with placeholder "PT4H / 2d / 1w"

UDA names must match `^[a-zA-Z][a-zA-Z0-9_]{0,63}$`; entries with shell metacharacters or parser tokens are dropped at discovery time.

UDA values render as first-class rows in the row info panel, one per UDA, separated from the built-in fields by a thin grey rule. `priority` is treated as a UDA on read (Taskwarrior 3.x emits it at the top level of the export JSON even when redeclared as a UDA); the form's Priority dropdown stays in sync.

## Bugwarrior

[bugwarrior](https://bugwarrior.readthedocs.io/) is a separate tool that pulls issues from bug trackers (GitHub, GitLab, Jira, Bugzilla, Linear, and 20+ others) into Taskwarrior as tasks with service-specific UDAs (`githubnumber`, `jiraid`, etc.).

When `bugwarrior` is installed and detectable, the portal surfaces it in two places:

- **Config page** - a **Pull now** button that runs `bugwarrior pull` and streams the result inline. The button only appears when the binary is found.
- **Task UI** - tasks imported by bugwarrior show a tracker chip (e.g. `GH #142`) in the row, kanban card, and expanded detail panel. The edit modal displays an amber banner ("Managed externally: GitHub - GH #142. Edits may be overwritten.") and hides the service UDA fields from the form since bugwarrior owns them.

bugwarrior is auto-detected on startup. If it is installed in a non-standard location (a virtualenv, a Docker volume mount, etc.), set `BUGWARRIOR_BIN` to its absolute path. When using `make install`, pass it at install time so it is baked into the service unit:

```sh
BUGWARRIOR_BIN=/home/user/.local/bin/bugwarrior make install
```

## Contexts

Taskwarrior contexts are persistent filters defined via the CLI; once active, every list / export / add is implicitly scoped to the context's read filter. taskwarrior-web-portal surfaces this as a coloured pill in the top nav, between the search box and Undo:

- **Inactive** - outline-only, greyed funnel icon, label "all", trailing chevron. Click to open the dropdown.
- **Active** - solid hashed-colour fill, leading status dot with a soft pulse, funnel icon, bold context name, trailing "x" for one-click clear.

Click the pill to open a dropdown listing every defined context (plus "(none)" at the top to clear). Picking an entry POSTs to `/context` and the server replies with `HX-Refresh: true`, reloading the page so the pill, the browser title hint (`Next [work] · taskwarrior-web-portal`), the empty-state copy ("No tasks match in context 'work'.") and the Add Task modal all re-render against the new active state.

### Managing contexts

**More ▾ → Contexts** (or `/contexts`) opens the manage page listing every defined context with its read filter, write filter, and active status. From there:

- **New context** - opens a modal with a Name field, a Read filter field (required; supports any Taskwarrior filter expression like `+work`, `project:acme`, `+team or project:team`), and an optional Write filter. Submits to `POST /contexts`.
- **Edit** - pre-fills the same modal for an existing context. Renaming (changing the Name field) defines the new name and deletes the old one atomically. Submits to `PUT /contexts/{name}`.
- **Delete** - removes the context with a confirmation prompt. `DELETE /contexts/{name}`.

After any mutation the context cache is invalidated immediately so the pill dropdown reflects the new state without waiting for the 60s TTL.

Pill colour is hashed from the context name into an 18-slot palette (six base hues - blue, teal, purple, amber, orange, pink - times three rounds: base, lighter, darker), so the same context always gets the same hue across reloads. Red is reserved for urgency-critical signals elsewhere and never used for contexts. Yellow is always paired with dark text to meet WCAG AA.

The active context name is read fresh on every page render (cheap `task _get rc.context` subprocess call); the list of defined contexts is TTL-cached on the `tw.Client` (60s).

**Filter composition.** Taskwarrior 3.x's `task export` does NOT honour the active context implicitly (unlike `task list` / `task next`). Reports rendered through the web UI compose the active read filter into every export argv via `(v *Views) exportWithContext` so the filter applies consistently. Empty filter = no clause prepended.

### Add modal context picker

The Add Task modal carries its own context dropdown in the header, defaulting to whichever context is currently active. Picking a different one silently overwrites the Tags / Project inputs with the new context's prefill values, and an italic helper line under the dropdown explains what's about to be attached ("Adds +client tag", "Sets Project = team", or "No context tag/project will be added"). This lets you stay in (say) the personnel context while capturing a one-off vendor task, without flipping the global context and back.

The prefill is derived from each context's read filter via `views.ContextPrefill`: first lowercase `+tag` wins, otherwise first `project:value` wins, ALL-UPPERCASE virtual tags are skipped. For an OR-shaped filter like `+team or project:team or project:hiring`, the picker prefills `+team`.

Note: Taskwarrior's per-context **write** filter is NOT applied automatically by the binary for OR-shaped filters - it gets confused and mangles the description. The form-level prefill is the only reliable way to keep new tasks consistent with the lens the user is working in.

## Dependencies

Each task in Taskwarrior carries an optional `depends:` list - the UUIDs of tasks that must finish before this one is actionable. taskwarrior-web-portal surfaces this end-to-end:

- **Row badge.** Any task with at least one `depends` entry shows a small "lock N blocked" badge inline with its tags / project / due chips. The count is the raw size of `t.Depends`; whether each prerequisite is still open is left to the `+READY` virtual tag (Ready view already filters them out).
- **Expand panel.** "Blocked by" section listing each prerequisite's truncated UUID (first 8 hex chars + ellipsis). Each entry is a link that opens that task's edit modal in-place via HTMX.
- **Multi-select picker.** The add/edit modals carry a "Depends on" field rendered as a tag-input style picker: pills for the currently-selected dependencies, a text input that autocompletes from the themed dropdown described below, and a hidden form field carrying the comma-joined UUID list. Press Enter on the typing input to add a pill once the typed value resolves to a UUID; click the x on any pill to remove it. The hidden field stays in sync with the live pill set, so submitting the form posts the right `depends:UUID,UUID` argv. An empty submission on edit clears every dependency (Taskwarrior's `depends:` clear-arg).
- **Validation.** Each UUID is checked against `tw.IDPattern` server-side before the call; malformed entries return 400. The picker's option list excludes the task currently being edited (a task cannot depend on itself).

Server fetches the open-tasks list once per modal render via `task export "(status:pending or status:waiting)"` - same query as the Browse page. If the export fails the modal still renders without a populated picker rather than 500ing the entire edit.

Known v2 TODO: the row's expand panel only shows "Blocked by"; the inverse "Blocks" (dependents) view is intentionally absent because computing it per-row would N+1 the list render. A future pass could prefetch via one `task export depends.any:` call and join client-side.

## Themed autocomplete

The Project, Tags, and Depends-on inputs in the add/edit modals use a custom-themed dropdown component (replacing the previous native `<datalist>`). The component is a styled `<input>` followed by a hidden `<ul>` of options that the autocomplete IIFE in `web/static/js/autocomplete.js` opens, filters as the user types, and lets them navigate by keyboard (Arrow up/down, Enter, Esc) or mouse. Selecting an option:

- **single mode** (Project) replaces the input value.
- **tokens mode** (Tags) appends as a new comma-separated token.
- **deps mode** (Depends-on) dispatches an `autocomplete:select` CustomEvent that the dep-picker module intercepts to add a pill instead of mutating the input.

Project / tags lists are TTL-cached on the `tw.Client` (60s); virtual tags (`+OVERDUE`, `+READY`, etc.) are filtered out at discovery so the dropdown only suggests tags you can actually set. Light and dark mode are both fully styled, so dropdown appearance is consistent regardless of OS theme.

## Form validation

When `task add` / `task <id> modify` rejects an input, the server responds 400 + an HTML error fragment swapped into `#task-form-errors` at the bottom of the modal. The fragment carries `data-field-error="<name>"`, which the form-validation module reads to:

1. Add a red border + ring to the offending input.
2. Auto-focus the input.

Highlight clears on the first `input` event so it doesn't feel sticky once the user starts fixing the value. Date fields carry a small `?` help tooltip (native `<details>`/`<summary>`, no JS) that documents the accepted forms - absolute (`2026-05-09`), keywords (`tomorrow`, `eom`), relative offsets (`+2d`, `due-3d`).

Validation errors are typed (`*tw.ValidationError{Field, Value, Reason}`) so the field classifier uses `errors.As` rather than parsing message text - renaming a message can't silently break the highlight.

## Recurrence

The add/edit modal carries first-class **Recur** and **Until** inputs alongside the date fields. Recur accepts the full Taskwarrior duration vocabulary (keywords like `weekly` / `monthly` / `quarterly`, durations like `1d` / `2w` / `1mo`, ISO 8601 like `P7D` / `P1M`); Until accepts the same syntax as Due / Wait / Scheduled. A `?` disclosure on each field lists the accepted forms; below each input, click-to-fill chips offer the common cases (`weekly`, `biweekly`, `monthly`, `quarterly`, `annually` for Recur; `+3mo`, `+6mo`, `+1y`, `eoy` for Until). Empty submission on edit clears the field.

When you open the modal on a recurring **child** instance, both fields are rendered read-only with a hint pointing at the parent template (find it under More ▾ Built-in ▾ Recurring) - Taskwarrior accepts edits to the schedule on the child but they don't take effect, so the form mirrors the actual semantics rather than the surface API. Editing the parent's Recur / Until propagates to all future instances on the next `task` refresh.

Deleting a `status:recurring` parent cascades to its `status:pending` children (`parent:<uuid> status:pending delete`) so the modal's Delete button on a recurring template doesn't leave orphan instances behind. The styled confirmation dialog explains the cascade before you commit.

## Duplicate

The edit modal carries a **Duplicate** action in its overflow (kebab) menu, hitting `POST /tasks/{id}/duplicate` (which shells `task <id> duplicate`). The clone copies description, project, tags, due / wait / scheduled, dependencies and UDAs as a fresh `status:pending` task; recurrence (`recur` / `until`) is intentionally **not** copied, so duplicating a recurring template gives you a one-off, not a second template - the button title says so when the source is recurring. A styled confirm appears first so the action isn't single-click destructive.

## Mark done from edit modal

The edit modal carries a green **Mark done** button next to **Save** (with a styled confirmation), so completing a task from the calendar's chip-click flow doesn't need to drop back to the row's done circle. Same `/tasks/{id}/done` endpoint as the inline circle. The modal closes on success via a delegated `data-close-on-success` handler (replaces inline `hx-on::after-request` so CSP `unsafe-eval` stays a defence-in-depth signal rather than an active sink).

On mobile the button collapses to a green checkmark icon to keep the action bar on one line; the desktop text label returns at the `sm:` breakpoint. `aria-label="Mark done"` is preserved on the icon variant.

## Inline annotation on save

Typing a note in the **Add a note (or click Save to attach it)...** input then clicking **Save** attaches the note as an annotation in addition to whatever else changed. The annotation input is part of the modal's main form; the modify handler picks up the `text` field after the structured modify lands and calls `task <id> annotate`. The dedicated **Add** button still works for the "add note, keep modal open" flow.

If the modify lands but the annotation fails (very rare; only the second `task` call), the response is a soft warning ("Task saved, but the note couldn't be attached") rather than a 500 - the structured edits already persisted, no need to roll back.

## Time tracking

Each row carries a small play / stop button between the bulk-select checkbox and the description: a grey play triangle when the task is inactive (POSTs to `/tasks/{id}/start`, marking it `+ACTIVE`), an amber stop square when the task is currently being tracked (POSTs to `/tasks/{id}/stop`, clearing the `start` timestamp). The button is intentionally hidden on `status:recurring` parents - you can't track time on a template, only on its child instances. The same Start / Stop pair appears in the edit modal next to the Save row, so kicking off a session from the calendar's chip-click flow is one click.

Active tasks render the `+ACTIVE` virtual tag in the row's chip strip and are surfaced as a count card on `/stats` ("Active N"). An amber pill in the top-right of the nav shows the currently tracked task(s) across every page: click it to expand a dropdown listing active tasks with inline stop buttons, or click the task description to open its edit modal.

### Retroactive sessions editor

The edit modal carries a **Tracked time (N)** trigger button in its top-left (or "Add time entry" when N=0). Clicking it stacks a second modal on top - the sessions editor - listing every recorded session for the task, newest-first, grouped by local calendar day with a per-day duration roll-up. From there:

- **Edit start / end** of any session - pick via the calendar icon (flatpickr) or type the value directly.
- **Add entry** - stages a new session in the bottom panel; submitted on Save alongside any edits.
- **Delete** - existing rows confirm first; staging-area rows remove instantly.
- **Stop tracking** - shown only when the task is currently active; shells `task <id> stop` and closes both modals.

Save submits a delta payload (`{edits, creates, deletes}`) to `PUT /tasks/{id}/intervals`, NOT a full snapshot - so a save while only the first page of session history is loaded leaves the older pages alone. Server validates the resulting interval set (no overlaps, at most one open interval) and returns 422 + structured conflict JSON on rejection; the editor renders a conflict panel at the top of the modal with editable mini-rows for each pair, hiding the source rows in the main list so the duplicate is the single source of truth for that submission.

Pagination: the list loads 14 days at a time; an "Earlier days" button at the bottom appends one page via HTMX outerHTML swap, leaving every already-rendered row intact (no scroll-loss, no datetime-local input reset). Clicking a chip on the `/timesheet` view opens the same editor scoped to that day, with a back-chevron in the header that pivots to the full task editor.

flatpickr powers the date/time picker on every session row's Start and End fields. Custom green-tick / red-X footer replaces the plugin's default "OK" button: the X reverts to the value the picker had on open, the tick commits. The hour/minute number inputs are switched from `type="number"` to `type="text"` + `inputmode="numeric"` and have their auto-selection cleared on focus, suppressing the iOS "Look Up / Copy / Paste" action sheet that would otherwise pop over the picker.

## Timesheet

`/timesheet` shows a log of every start/stop session recorded by Taskwarrior's `journal.time` annotation mechanism. Two modes:

- **Week** (default) - 7-column grid (Mon–Sun), each cell showing session chips with description, time range, and duration. Today gets a blue tint. Mobile renders as a vertical day list; days with no sessions are omitted.
- **Day** - flat chronological list for a single day with description, project, time range, and duration per row.

Step periods with the prev/next arrows; jump to the current period with Today. Total duration for the week (or day) appears in the column headers.

Active sessions (no stop time yet) render with an amber left border so in-progress work is visually distinct from completed sessions.

Each session block carries a 2px top border whose colour is hashed deterministically from the task UUID (12 hues × 3 shades = 36 palette entries; first 8 hex chars of UUID parsed as uint32 → modulo palette length). Same task → same hue across the week, so a single task tracked over 5 sessions reads as a connected ribbon. The active-session amber left border and the task-identity top border are independent axes - they coexist via side-specific `border-{l,t}-{colour}` utilities.

Timesheet honours the active context filter (uses the same `exportWithContext` plumbing as every other read view), so flipping to a context narrows the timesheet to that context's tasks. Clicking any chip opens the retroactive sessions editor scoped to that day (see [Retroactive sessions editor](#retroactive-sessions-editor)).

If `journal.time` is not enabled in `~/.taskrc`, the page shows a prompt explaining what's needed and a one-click **Enable time tracking** button that writes `journal.time=yes` to `~/.taskrc`. Past sessions cannot be recovered; only future start/stop events are recorded.

## Known limitations

- **No SSE.** Polling at 30s only.
- **No drag/drop reordering.** Sort is via column headers, not by hand.
- **Search is substring-only.** No way to compose `priority:H and project:hiring` ad-hoc through the search box; use a context or a defined report.
- **No SSE for timesheet.** The timesheet page doesn't live-update the duration of in-progress sessions; reload or navigate away and back to see the current elapsed time.

## Where things live

```
internal/tw/        Taskwarrior CLI wrapper. Only place that touches `task` or sqlite.
internal/server/    HTTP server, middleware, routes, CSRF.
internal/server/handlers/  HTTP handlers split by concern (views, tasks, forms, contexts, partials, calendar, form_errors).
internal/views/     templ files + small Go helpers (urls, format, palette, keybindings, styles).
internal/config/    Centralised timeouts, bind addr, derived host/origin allowlists.
web/static/         Embedded static assets (htmx.min.js, theme.js, app.css, favicon.svg).
web/static/js/      Per-feature JS modules (core_*, keys, bulk, search, sort, row_expand,
                    context_pill, context_picker, autocomplete, deps, form_validation,
                    sessions_modal, form_date_picker).
web/static/vendor/  Vendored third-party libraries (flatpickr 4.6.13).
scripts/            Install/uninstall + the standalone tailwindcss binary.
deploy/             LaunchAgent plist template.
```

JS is split per-feature instead of a single `app.js` so each module has a clear responsibility, and the browser shows them per-source in DevTools. All scripts are loaded individually via `<script src defer>` (no bundle step). `core_*.js` modules set up cross-feature plumbing (CSRF, confirm dialog, modal lifecycle, refresh, theme); the rest are self-contained feature modules that hook into HTMX events.

Production code currently sits around 4,000 LOC of Go plus ~1,500 lines of templ; the original 500-line v0 budget was abandoned once contexts, dependencies, custom autocomplete, themed validation, calendar, the config package, and the per-feature JS split landed.
