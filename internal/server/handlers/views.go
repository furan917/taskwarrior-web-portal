package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/furan917/taskwarrior-web-portal/internal/config"
	"github.com/furan917/taskwarrior-web-portal/internal/tw"
	"github.com/furan917/taskwarrior-web-portal/internal/views"
)

// Views holds dependencies for read-side handlers. Constructed once in
// server.New and shared across goroutines (TW.Client is goroutine-safe; the
// logger is from slog).
type Views struct {
	TW     *tw.Client
	Logger *slog.Logger
}

// reportSpec is the static metadata for a named report. The filter field for
// "agenda" and "forecast" is treated as a fallback; the live value comes from
// `task _get rc.report.<name>.filter` and is cached on tw.Client.
type reportSpec struct {
	filter string
	title  string
	active string
}

// curatedReportSpecs holds the four reports the UI surfaces in its main
// nav. They get fixed URLs (/ready, /next, /agenda, /forecast) and
// curated titles. "next" and "ready" override Taskwarrior's stored
// filters with crafted ones the web view needs; "agenda" and "forecast"
// prefer the user's own taskrc filter when present and fall back to
// these defaults.
//
// Any OTHER report defined in the user's taskrc surfaces dynamically via
// `/r/<name>` - see ReportByName below.
var curatedReportSpecs = map[string]reportSpec{
	// Next is the broad pending umbrella (urgency-sorted, top 50). The OR
	// clause catches lapsed-wait tasks that Taskwarrior 3.x with taskchampion
	// hasn't auto-promoted in storage yet - pure query-time fix, no writes.
	"next":     {"(status:pending or (status:waiting and wait.before:now)) limit:50", "Next", "next"},
	"ready":    {"+READY", "Ready", "ready"},
	"agenda":   {"(status:pending or status:waiting) (due.before:14d or wait.before:14d)", "Agenda · 14 days", "agenda"},
	"forecast": {"(status:pending or status:waiting) (due.before:30d or wait.before:30d)", "Forecast · 30 days", "forecast"},
	// /r/recurring overrides TW's stock report.recurring filter (which
	// surfaces both parents and pending children) to show ONLY parent
	// templates. The page is the "manage your recurring series" surface;
	// instances are reachable through the regular /next, /agenda, etc.
	// Slug uses "r-recurring" so it lights up under "More ▾" rather than
	// the curated four (which use bare slugs).
	"recurring": {"status:recurring", "Recurring", "r-recurring"},
}

// reportFilterTimeout aliases config.ReportFilterTimeout to keep the
// existing site comment readable; the canonical source of truth is the
// config package.
const reportFilterTimeout = config.ReportFilterTimeout

// specForReport returns the report spec for one of the curated four,
// preferring the user's taskrc filter for agenda/forecast where it exists.
// Returns ok=false for any other name; dynamic reports go through
// dynamicReportSpec instead.
func (v *Views) specForReport(name string) (reportSpec, bool) {
	spec, ok := curatedReportSpecs[name]
	if !ok {
		return reportSpec{}, false
	}
	switch name {
	case "agenda", "forecast":
		ctx, cancel := context.WithTimeout(context.Background(), reportFilterTimeout)
		defer cancel()
		if got := v.TW.ReportFilterCached(ctx, name); got != "" {
			spec.filter = got
		}
	}
	return spec, true
}

// builtinReportNames is the curated subset of Taskwarrior's stock reports
// surfaced under the nav's "More > Built-in" section. Filters come from
// ReportFilterCached at request time so we don't duplicate TW's own
// definitions; if the user has redefined any of these in ~/.taskrc the
// override wins automatically. The full TW set is much larger (~25
// reports including list/long/ls/minimal/all/completed/...) but most are
// noisy or redundant against the curated five top-level tabs - this
// shortlist is the useful subset that actually answers a different
// question than the top-level views.
// "recurring" is in BOTH this list AND curatedReportSpecs. The curated
// entry overrides TW's stock report.recurring filter (which conflates
// parents and children) with status:recurring (templates only); keeping
// it here ensures it still surfaces in the More dropdown alongside its
// peers. customReports() dedupes against curatedReportSpecs so it never
// ends up in the Custom section.
var builtinReportNames = []string{"active", "blocked", "overdue", "recurring", "waiting"}

// isBuiltinReport reports whether name is in the curated TW built-in set.
// Used both to admit /r/<name> routes for built-ins and to exclude them
// from the "Custom" dropdown section (since they get their own header).
func isBuiltinReport(name string) bool {
	return slices.Contains(builtinReportNames, name)
}

// dynamicReportSpec resolves a report name to a reportSpec when the name is
// either (a) in the curated built-in shortlist or (b) discovered from the
// user's taskrc via ReportsCached. Returns ok=false otherwise (defence in
// depth - rejects names that pass the URL pattern but aren't actually
// defined). Filter comes from ReportFilterCached; title is the capitalised
// name. The active slug is "r-<name>" so the nav highlight stays distinct
// from the curated four.
func (v *Views) dynamicReportSpec(ctx context.Context, name string) (reportSpec, bool) {
	known := isBuiltinReport(name)
	if !known {
		for _, n := range v.TW.ReportsCached(ctx) {
			if n == name {
				known = true
				break
			}
		}
	}
	if !known {
		return reportSpec{}, false
	}
	filterCtx, cancel := context.WithTimeout(ctx, reportFilterTimeout)
	defer cancel()
	// An empty filter is fine: report defined but with no filter clause
	// (custom report using default columns/sort). exportWithContext still
	// applies the active context, so we pass it through unchanged.
	filter := v.TW.ReportFilterCached(filterCtx, name)
	return reportSpec{
		filter: filter,
		title:  strings.ToUpper(name[:1]) + name[1:],
		active: "r-" + name,
	}, true
}

func (v *Views) Home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/ready", http.StatusFound)
}

// Report returns a handler for one of the curated reports.
func (v *Views) Report(name string) http.HandlerFunc {
	if _, ok := curatedReportSpecs[name]; !ok {
		return func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		spec, _ := v.specForReport(name)
		v.renderReport(w, r, name, spec)
	}
}

// ReportByName handles `/r/{name}` for any user-defined report discovered
// in the taskrc. Curated reports stay on their dedicated paths so existing
// bookmarks keep working; this route surfaces everything else.
func (v *Views) ReportByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.ReportNamePattern.MatchString(name) {
		http.Error(w, "invalid report name", http.StatusBadRequest)
		return
	}
	// Prefer curated spec when the name overlaps - keeps semantics
	// consistent whether the user lands on /ready or /r/ready.
	if spec, ok := v.specForReport(name); ok {
		v.renderReport(w, r, name, spec)
		return
	}
	spec, ok := v.dynamicReportSpec(r.Context(), name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	v.renderReport(w, r, name, spec)
}

// renderReport is the shared body of Report and ReportByName: fetch with
// the spec's filter, sort, render via ListPage. Pulled out so the two
// entry points stay one-liners.
func (v *Views) renderReport(w http.ResponseWriter, r *http.Request, name string, spec reportSpec) {
	filterArgs := append(splitFilter(spec.filter), v.userFilter(r))
	tasks, err := v.fetch(r, filterArgs...)
	if err != nil {
		v.Logger.Error("report fetch failed", "report", name, "err", err)
		http.Error(w, exportErrMsg(err), http.StatusInternalServerError)
		return
	}
	tasks = retainPendingDeps(tasks)
	page := v.buildPage(r, spec.title, spec.active, true)
	renderHTML(w, r, "Report",
		views.ListPage(page, name, "", tasks, views.ParseSort(r.URL.Query()), v.userFilter(r)),
		v.Logger, "report", name)
}

// retainPendingDeps filters each task's Depends list to only include UUIDs
// that belong to another pending/waiting task in the same slice. This mirrors
// Taskwarrior's own rule: deleted and completed tasks never block a dependent.
// The row template and stats counter both use len(Depends)>0 to decide whether
// a task is blocked, so without this step a stale dep on a deleted task would
// wrongly mark the task as blocked.
func retainPendingDeps(tasks []tw.Task) []tw.Task {
	live := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if t.Status == "pending" || t.Status == "waiting" {
			live[t.UUID] = true
		}
	}
	for i, t := range tasks {
		if len(t.Depends) == 0 {
			continue
		}
		filtered := t.Depends[:0]
		for _, dep := range t.Depends {
			if live[dep] {
				filtered = append(filtered, dep)
			}
		}
		tasks[i].Depends = filtered
	}
	return tasks
}

// Labels lists every project and every tag currently attached to open
// (pending or waiting) tasks, with counts. Each entry links into the
// corresponding /project/{name} or /tag/{name} drilldown.
func (v *Views) Labels(w http.ResponseWriter, r *http.Request) {
	open, err := v.exportWithContext(r, "(status:pending or status:waiting)")
	if err != nil {
		v.Logger.Error("labels fetch failed", "err", err)
		http.Error(w, "fetch failed", http.StatusInternalServerError)
		return
	}
	done, err := v.exportWithContext(r, "status:completed")
	if err != nil {
		v.Logger.Error("labels completed fetch failed", "err", err)
		http.Error(w, "fetch failed", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	type projectStats struct {
		pending      int
		completed    int
		totalAgeSecs int64
	}
	pm := map[string]*projectStats{}
	ensure := func(name string) *projectStats {
		if pm[name] == nil {
			pm[name] = &projectStats{}
		}
		return pm[name]
	}

	tagCounts := map[string]int{}
	for _, t := range open {
		if t.Project != "" {
			ps := ensure(t.Project)
			ps.pending++
			if entry, err := tw.ParseTime(t.Entry); err == nil && !entry.IsZero() {
				ps.totalAgeSecs += int64(now.Sub(entry).Seconds())
			}
		}
		for _, tag := range t.Tags {
			tagCounts[tag]++
		}
	}
	for _, t := range done {
		if t.Project != "" {
			ensure(t.Project).completed++
		}
	}

	inputs := make([]views.ProjectInput, 0, len(pm))
	for name, ps := range pm {
		inputs = append(inputs, views.ProjectInput{
			Name:         name,
			Pending:      ps.pending,
			Completed:    ps.completed,
			TotalAgeSecs: ps.totalAgeSecs,
		})
	}
	// Sort by pending desc, name asc - matches the old sortedCounted order.
	sort.SliceStable(inputs, func(i, j int) bool {
		if inputs[i].Pending != inputs[j].Pending {
			return inputs[i].Pending > inputs[j].Pending
		}
		return inputs[i].Name < inputs[j].Name
	})

	page := v.buildPage(r, "Browse", "browse", false)
	roots := views.BuildProjectTree(inputs)
	renderHTML(w, r, "Labels",
		views.LabelsPage(page, roots, sortedCounted(tagCounts)),
		v.Logger)
}

// sortedCounted returns name/count pairs sorted by count desc, then name asc.
func sortedCounted(m map[string]int) []views.Counted {
	out := make([]views.Counted, 0, len(m))
	for k, v := range m {
		out = append(out, views.Counted{Name: k, Count: v})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Tag handles /tag/{name} - filters open tasks by `+name`. Status is pinned
// to pending-or-waiting so deleted instances, completed history, and
// recurring parents (status:recurring template rows) don't leak into a
// browse-by-tag list.
func (v *Views) Tag(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.TagPattern.MatchString(name) {
		http.Error(w, "invalid tag name", http.StatusBadRequest)
		return
	}
	tasks, err := v.fetch(r, "(status:pending or status:waiting) +"+name, v.userFilter(r))
	if err != nil {
		v.Logger.Error("tag fetch failed", "tag", name, "err", err)
		http.Error(w, exportErrMsg(err), http.StatusInternalServerError)
		return
	}
	page := v.buildPage(r, "+"+name, "", true)
	renderHTML(w, r, "Tag",
		views.ListPage(page, "tag-"+name, "", tasks, views.ParseSort(r.URL.Query()), v.userFilter(r)),
		v.Logger, "tag", name)
}

// Project handles /project/{name}.
func (v *Views) Project(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.ProjectPattern.MatchString(name) {
		http.Error(w, "invalid project name", http.StatusBadRequest)
		return
	}
	tasks, err := v.fetch(r, "(status:pending or status:waiting) project:"+name, v.userFilter(r))
	if err != nil {
		v.Logger.Error("project fetch failed", "project", name, "err", err)
		http.Error(w, exportErrMsg(err), http.StatusInternalServerError)
		return
	}
	page := v.buildPage(r, "project: "+name, "", true)
	renderHTML(w, r, "Project",
		views.ListPage(page, "", name, tasks, views.ParseSort(r.URL.Query()), v.userFilter(r)),
		v.Logger, "project", name)
}

// Calendar renders the month/week/day calendar view. Open tasks (pending or
// waiting) with a `due` date appear; if `scheduled` (or `wait`, falling back)
// is also set and earlier than `due`, the task spans every day in that range
// as a per-cell chip with rounded-corner cues at the edges.
//
// Query params (both optional):
//   - view=month|week|day  (default: month)
//   - date=YYYY-MM-DD       (default: today)
func (v *Views) Calendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	view := q.Get("view")
	if view == "" {
		view = views.CalendarMonth
	}
	switch view {
	case views.CalendarMonth, views.CalendarWeek, views.CalendarDay:
	default:
		http.Error(w, "invalid view", http.StatusBadRequest)
		return
	}

	now := time.Now()
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if d := q.Get("date"); d != "" {
		parsed, err := time.ParseInLocation("2006-01-02", d, now.Location())
		if err != nil {
			http.Error(w, "invalid date (want YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		anchor = parsed
	}

	tasks, err := v.exportWithContext(r, "(status:pending or status:waiting)")
	if err != nil {
		v.Logger.Error("calendar fetch failed", "err", err)
		http.Error(w, "fetch failed", http.StatusInternalServerError)
		return
	}

	page := BuildCalendarPage(view, anchor, tasks)
	page.Page = v.buildPage(r, "Calendar", "calendar", false)

	renderHTML(w, r, "Calendar", views.Calendar(page), v.Logger)
}

// splitFilter splits a Taskwarrior filter string into individual argv tokens
// while keeping parenthesised groups intact. Simple whitespace split breaks
// filters like "(status:pending or status:waiting) (due.before:14d or ...)"
// because the parens span multiple words; splitting only at depth-0 spaces
// preserves each group as one argument that Taskwarrior can parse correctly.
func splitFilter(f string) []string {
	if f == "" {
		return nil
	}
	var tokens []string
	depth, start := 0, 0
	for i := 0; i < len(f); i++ {
		switch f[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ':
			if depth == 0 && i > start {
				tokens = append(tokens, f[start:i])
				start = i + 1
			} else if depth == 0 {
				start = i + 1
			}
		}
	}
	if start < len(f) {
		tokens = append(tokens, f[start:])
	}
	return tokens
}

// exportWithContext wraps tw.Client.Export, prepending the active context's
// read filter as a parenthesised AND clause when one is set. Taskwarrior
// 3.x's `task export` does NOT honour the active context implicitly (unlike
// report commands like `task list` / `task next`), so reports rendered in
// the web UI would otherwise leak tasks from outside the active context.
// Composing explicitly here is the only reliable fix.
//
// Empty filters are dropped so we don't pass empty argv elements; an empty
// active-context name means no context clause is added.
func (v *Views) exportWithContext(r *http.Request, filters ...string) ([]tw.Task, error) {
	args := make([]string, 0, len(filters)+1)
	if cf := v.activeContextFilter(r); cf != "" {
		args = append(args, "("+cf+")")
	}
	for _, f := range filters {
		if f != "" {
			args = append(args, f)
		}
	}
	return v.TW.Export(r.Context(), args...)
}

// fetch is exportWithContext + the URL's sort spec applied. Used by the
// list-style read handlers (Report / Tag / Project / partials) where the
// URL carries an optional ?sort=key:dir. Accepts variadic filters so callers
// can pass the view filter and the user-supplied ad-hoc filter independently;
// empty strings are dropped by exportWithContext.
func (v *Views) fetch(r *http.Request, filters ...string) ([]tw.Task, error) {
	tasks, err := v.exportWithContext(r, filters...)
	if err != nil {
		return nil, err
	}
	applySort(tasks, views.ParseSort(r.URL.Query()))
	return tasks, nil
}

// userFilter reads the raw ?filter= query param and sanitizes it before use.
// rc.* override tokens are stripped silently; anything else is passed through
// to Taskwarrior which returns an error the caller can surface.
func (v *Views) userFilter(r *http.Request) string {
	return tw.SanitizeUserFilter(r.URL.Query().Get("filter"))
}

// exportErrMsg maps a task export error to a user-facing message. Keeps all
// the error-classification logic in one place instead of duplicated across
// every fetch handler.
func exportErrMsg(err error) string {
	switch {
	case tw.IsNotInitialised(err):
		return "Taskwarrior is not initialised - run `task` once in a terminal to create ~/.taskrc, then reload"
	case tw.IsInvalidFilter(err):
		return "A context filter could not be evaluated - run `task context none` to clear it, or redefine the context with export-compatible syntax (use -tag not -+tag)"
	default:
		return "task export failed"
	}
}

// buildPage assembles the views.Page envelope shared by every read handler.
// Replaces the duplicated CSRFToken / ActiveContext / Contexts plumbing
// that used to live in five copies (Report / Labels / Tag / Project /
// Calendar / Done). hasTaskList drives the keyHint footer's row-action
// keybindings; pages without a task list (Labels, Calendar, Done before
// the v5 row chrome unification) pass false to suppress them.
func (v *Views) buildPage(r *http.Request, title, activeView string, hasTaskList bool) views.Page {
	return buildPage(v.TW, r, title, activeView, hasTaskList)
}

// buildPage is the canonical Page-envelope assembler shared across every
// read handler in this package. Lives as a free function (rather than only
// a *Views method) so the contexts handler - which doesn't hold a *Views -
// can use it too without re-implementing the BuiltinReports / CustomReports
// wiring. Adding a Page field stays a one-place change; before this lift,
// /contexts silently rendered without the More-dropdown report sections.
func buildPage(twc *tw.Client, r *http.Request, title, activeView string, hasTaskList bool) views.Page {
	active := activeContext(twc, r)
	return views.Page{
		Title:          title,
		ActiveView:     activeView,
		CSRFToken:      csrfToken(r),
		HasTaskList:    hasTaskList,
		ActiveContext:  active,
		Contexts:       namedContextsForRender(twc, r, active),
		BuiltinReports: builtinReportNames,
		CustomReports:  customReports(twc.ReportsCached(r.Context())),
	}
}

// customReports filters the cached report list down to user-defined entries:
// anything beyond the curated four (next/ready/agenda/forecast) AND beyond
// the built-in shortlist (those get their own dropdown section). Order is
// preserved from filterStringList so names appear alphabetically.
func customReports(all []string) []string {
	out := make([]string, 0, len(all))
	for _, n := range all {
		if _, curated := curatedReportSpecs[n]; curated {
			continue
		}
		if isBuiltinReport(n) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// activeContextFilter returns the read filter of the currently-active
// context, or empty string if no context is active or the filter contains
// rc.* override tokens (defence in depth - see tw.Context.SafeReadFilter).
// Reads the per-request active name (live, never cached) and looks up
// the cached read filter.
func (v *Views) activeContextFilter(r *http.Request) string {
	name := activeContext(v.TW, r)
	if name == "" {
		return ""
	}
	for _, c := range v.TW.ContextsCached(r.Context()) {
		if c.Name == name {
			return c.SafeReadFilter()
		}
	}
	return ""
}

// applySort orders tasks in place per the spec. Stable sort preserves prior
// ordering for equal keys, which matters when sorting by description / project
// with many empty values.
func applySort(tasks []tw.Task, s views.SortSpec) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		var less bool
		switch s.Key {
		case "urgency":
			less = a.Urgency < b.Urgency
		case "due":
			// Empty due strings sort last regardless of direction so tasks with
			// a date always surface above undated ones.
			if a.Due == "" && b.Due == "" {
				return false
			}
			if a.Due == "" {
				return false
			}
			if b.Due == "" {
				return true
			}
			less = a.Due < b.Due
		case "project":
			less = a.Project < b.Project
		case "description":
			less = strings.ToLower(a.Description) < strings.ToLower(b.Description)
		case "entry":
			less = a.Entry < b.Entry
		default:
			return false
		}
		if s.Asc {
			return less
		}
		return !less
	})
}

// csrfToken extracts the per-request token set by the CSRF middleware.
func csrfToken(r *http.Request) string { return CSRFToken(r.Context()) }

// ConfigInfo renders the read-only /config page showing non-sensitive
// Taskwarrior and portal configuration values.
func (v *Views) ConfigInfo(s *Sync, bw *Bugwarrior) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := v.TW.GetConfigInfo(r.Context())
		portal := views.PortalConfig{
			BindAddr:            config.Addr(),
			AllowedHosts:        config.AllowedHosts(),
			BugwarriorAvailable: bw.Available(),
		}
		page := v.buildPage(r, "Configuration", "config", false)
		renderHTML(w, r, "Config", views.ConfigInfoPage(page, info, portal, s.Result(), bw.Result()), v.Logger)
	}
}

// Done shows recently completed tasks. Default window: last 14 days; the
// window can be widened via `?days=N` (clamped to 1..90). Sorted by End
// descending (the exact completion timestamp); falls back to Modified for
// tasks that lack an End field.
func (v *Views) Done(w http.ResponseWriter, r *http.Request) {
	days := 14
	if s := r.URL.Query().Get("days"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 90 {
			days = n
		}
	}
	// Pass the two clauses as separate argv elements so Taskwarrior treats
	// them as a clean implicit-AND. Combined into a single argv the parser
	// loses the AND when a `status:` reset is involved and silently returns
	// zero rows - which is why marked-done tasks weren't appearing here.
	tasks, err := v.exportWithContext(r, "status:completed", fmt.Sprintf("end.after:now-%dd", days))
	if err != nil {
		v.Logger.Error("done fetch failed", "err", err)
		http.Error(w, exportErrMsg(err), http.StatusInternalServerError)
		return
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].CompletedAt() > tasks[j].CompletedAt()
	})
	page := v.buildPage(r, fmt.Sprintf("Done · last %d days", days), "done", false)
	renderHTML(w, r, "Done", views.DonePage(page, tasks, days), v.Logger)
}

// Stats renders the dashboard at /stats: count cards, completion history,
// burndown charts, monthly history table, and net fix rate. Five Export calls
// back the page (open, completed-30d, completed-365d, recurring, deleted);
// everything else is computed in Go from those slices to keep wall time low.
const statsHistoryDays = 30

func (v *Views) Stats(w http.ResponseWriter, r *http.Request) {
	open, err := v.exportWithContext(r, "(status:pending or status:waiting)")
	if err != nil {
		v.Logger.Error("stats: open fetch failed", "err", err)
		http.Error(w, "task export failed", http.StatusInternalServerError)
		return
	}
	completed, err := v.exportWithContext(r, "status:completed", fmt.Sprintf("end.after:now-%dd", statsHistoryDays))
	if err != nil {
		v.Logger.Error("stats: completed fetch failed", "err", err)
		http.Error(w, "task export failed", http.StatusInternalServerError)
		return
	}
	// Wider completed window for the monthly history table (12 months).
	// Separate from the daily-chart export so we don't balloon the chart window.
	allCompleted, err := v.exportWithContext(r, "status:completed", "end.after:now-365d")
	if err != nil {
		v.Logger.Error("stats: history fetch failed", "err", err)
		http.Error(w, "task export failed", http.StatusInternalServerError)
		return
	}
	// Recurring count needs a separate query: parents are status:recurring
	// (excluded from the open pool) and that's what users mean by "active
	// recurring series" - not "pending instances that happen to carry a
	// recur field". exportWithContext applies the active context filter so
	// /stats stays scoped consistently with the other tabs.
	recurring, err := v.exportWithContext(r, "status:recurring")
	if err != nil {
		v.Logger.Error("stats: recurring fetch failed", "err", err)
		http.Error(w, "task export failed", http.StatusInternalServerError)
		return
	}
	// Deleted tasks must be included in the peak-scan for fix-rate calculation:
	// Taskwarrior's CmdBurndown::scanForPeak processes ALL tasks (including
	// deleted) to find the highest simultaneous open count. Without them the
	// peak is too low and the rate is understated.
	deleted, err := v.exportWithContext(r, "status:deleted")
	if err != nil {
		v.Logger.Error("stats: deleted fetch failed", "err", err)
		http.Error(w, "task export failed", http.StatusInternalServerError)
		return
	}
	open = retainPendingDeps(open)
	stats := computeStats(open, completed, statsHistoryDays)
	stats.Recurring = len(recurring)
	now := time.Now()
	stats.MonthlyHistory = computeMonthlyHistory(open, allCompleted)
	stats.BurndownDaily = computeBurndown(open, allCompleted, true, 30, now)
	stats.BurndownWeekly = computeBurndown(open, allCompleted, false, 13, now)
	stats.NetFixRate, stats.EstimatedCompletion = computeBurndownRates(open, allCompleted, deleted, recurring, now)
	if v.TW.JournalTimeEnabled(r.Context()) {
		stats.TimeHistory = computeTimeHistory(append(open, allCompleted...), statsHistoryDays, now)
	}
	page := v.buildPage(r, "Stats", "stats", false)
	renderHTML(w, r, "Stats", views.StatsPage(page, stats), v.Logger)
}

// computeStats derives every count + the per-day history slice from the
// two Export results. Pure function so tests can drive it without a fake
// binary - the only impurity is `now`, which we capture once at entry so
// the cutoffs and the History day labels all line up.
//
// open: every task with status pending or waiting (the work-in-progress
// pool). completed: every task with status:completed end.after:now-Nd.
// days: the trailing window for the in-window count + per-day chart.
func computeStats(open, completed []tw.Task, days int) views.Stats {
	now := time.Now()
	cutoff7 := now.Add(-7 * 24 * time.Hour)
	cutoffWindow := now.Add(-time.Duration(days) * 24 * time.Hour)

	s := views.Stats{WindowDays: days}

	for _, t := range open {
		switch t.Status {
		case "pending":
			s.Pending++
		case "waiting":
			s.Waiting++
		}
		if t.IsOverdue() {
			s.Overdue++
		}
		if t.IsActive() {
			s.Active++
		}
		// Recurring count is set by Stats() from a dedicated status:recurring
		// query; the open-pool loop does NOT increment it. Counting pending
		// children here would conflate "active recurring series" with "open
		// instances that happen to carry an inherited recur field".
		if len(t.Depends) > 0 {
			s.Blocked++
		}
	}

	// Bucket completed tasks per local-time day. Bucket key is the local
	// YYYY-MM-DD so we line up with the day labels the chart renders.
	buckets := make(map[string]int, days)
	for _, t := range completed {
		end, err := tw.ParseTime(t.CompletedAt())
		if err != nil || end.IsZero() {
			continue
		}
		if end.Before(cutoffWindow) {
			continue
		}
		if end.After(cutoff7) {
			s.Completed7d++
		}
		s.CompletedInWindow++
		buckets[end.Local().Format("2006-01-02")]++
	}

	// History lands newest-first (today at index 0, day-N-back at index N).
	// completionChartSVG reverses to oldest-first for left-to-right
	// rendering; we keep the handler-side order conventional so other
	// future consumers don't have to re-reverse.
	s.History = make([]views.DayCount, 0, days)
	for i := range days {
		day := now.AddDate(0, 0, -i)
		key := day.Format("2006-01-02")
		s.History = append(s.History, views.DayCount{
			Label: day.Format("1/2"),
			Date:  key,
			Count: buckets[key],
		})
	}
	return s
}

// computeBurndown returns a snapshot of the pending-task count at the end of
// each period in the window. daily=true gives 30 one-day periods; false gives
// 13 one-week periods. Result is oldest-first so the SVG renders left-to-right
// without reversing.
//
// For each period boundary P:
//   - An open (still-pending) task counts if entry <= P.
//   - A completed task counts as pending if entry <= P AND end > P;
//     it counts as done (and is excluded from pending) once end <= P.
//
// allCompleted is bounded by the caller's 365-day window, so tasks completed
// more than a year ago are not reflected. For a 13-week burndown this is
// acceptable: the trend shape is correct even if absolute values are slightly
// lower than reality.
// computeBurndown's `now` is injected so tests can pin a deterministic
// clock and not drift near the UTC/local midnight crossover (the bucket
// math clamps local-time-of-day while fixture timestamps are UTC, so the
// previous time.Now() call introduced ~1h/day of theoretical flake).
// Production call site passes time.Now() via the Stats handler.
func computeBurndown(open, allCompleted []tw.Task, daily bool, bars int, now time.Time) []views.BurndownBar {
	result := make([]views.BurndownBar, bars)

	for i := range bars {
		// i=0 is the oldest period, i=bars-1 is today.
		periodsAgo := bars - 1 - i
		var periodEnd time.Time
		var label, date string

		if daily {
			d := now.AddDate(0, 0, -periodsAgo)
			periodEnd = time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, d.Location())
			label = d.Format("1/2")
			date = d.Format("2006-01-02")
		} else {
			d := now.AddDate(0, 0, -periodsAgo*7)
			periodEnd = time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, d.Location())
			label = d.Format("1/2")
			date = d.Format("2006-01-02")
		}

		pending, started, done := 0, 0, 0

		for _, t := range open {
			entry, err := tw.ParseTime(t.Entry)
			if err != nil || entry.IsZero() || entry.After(periodEnd) {
				continue
			}
			if t.Start != "" {
				if start, err := tw.ParseTime(t.Start); err == nil && !start.After(periodEnd) {
					started++
					continue
				}
			}
			pending++
		}
		for _, t := range allCompleted {
			entry, err := tw.ParseTime(t.Entry)
			if err != nil || entry.IsZero() || entry.After(periodEnd) {
				continue
			}
			end, err := tw.ParseTime(t.CompletedAt())
			if err != nil || end.IsZero() {
				pending++
				continue
			}
			if end.After(periodEnd) {
				// still open at this point - was it started?
				if t.Start != "" {
					if start, err := tw.ParseTime(t.Start); err == nil && !start.After(periodEnd) {
						started++
						continue
					}
				}
				pending++
			} else {
				done++ // cumulative: completed on or before this period
			}
		}

		result[i] = views.BurndownBar{Label: label, Date: date, Pending: pending, Started: started, Done: done}
	}
	return result
}

// computeBurndownRates matches Taskwarrior's CmdBurndown::scanForPeak algorithm
// exactly: for every task, count it as "pending" on each calendar day from its
// entry date until its end date (or today if still open). Deleted and recurring
// tasks are included — Taskwarrior's scanForPeak processes ALL tasks (filter.subset
// without a status filter returns everything). The peak is the earliest day with
// the maximum count, matching std::map's ascending-key iteration.
// Returns (0, "") when there is no convergence.
func computeBurndownRates(open, allCompleted, deleted, recurring []tw.Task, now time.Time) (float64, string) {
	dailyCounts := map[string]int{}
	currentCount := 0

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	addOpen := func(entry time.Time) {
		currentCount++
		startDay := time.Date(entry.Year(), entry.Month(), entry.Day(), 0, 0, 0, 0, entry.Location())
		for d := startDay; !d.After(today); d = d.AddDate(0, 0, 1) {
			dailyCounts[d.Format("2006-01-02")]++
		}
	}
	addClosed := func(entry, end time.Time) {
		startDay := time.Date(entry.Year(), entry.Month(), entry.Day(), 0, 0, 0, 0, entry.Location())
		// Taskwarrior's loop: while (entry_incremented < end_exact_time)
		// Using midnight of each day as entry_incremented approximates this —
		// a task completed at 15:00 on day D has midnight(D) < 15:00 so D is counted.
		for d := startDay; d.Before(end); d = d.AddDate(0, 0, 1) {
			dailyCounts[d.Format("2006-01-02")]++
		}
	}

	for _, slc := range [][]tw.Task{open, recurring} {
		for _, t := range slc {
			entry, err := tw.ParseTime(t.Entry)
			if err != nil || entry.IsZero() {
				continue
			}
			addOpen(entry)
		}
	}
	for _, slc := range [][]tw.Task{allCompleted, deleted} {
		for _, t := range slc {
			entry, err := tw.ParseTime(t.Entry)
			if err != nil || entry.IsZero() {
				continue
			}
			end, err := tw.ParseTime(t.CompletedAt())
			if err != nil || end.IsZero() {
				continue
			}
			addClosed(entry, end)
		}
	}

	if currentCount == 0 {
		return 0, ""
	}

	// Taskwarrior's std::map iterates in ascending key order; the first date
	// to reach the maximum count becomes the peak (earliest tied date wins).
	var peakCount int
	var peakDate string
	dates := make([]string, 0, len(dailyCounts))
	for d := range dailyCounts {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	for _, date := range dates {
		if dailyCounts[date] > peakCount {
			peakCount = dailyCounts[date]
			peakDate = date
		}
	}

	if peakCount <= currentCount {
		return 0, ""
	}
	peak, err := time.ParseInLocation("2006-01-02", peakDate, now.Location())
	if err != nil {
		return 0, ""
	}
	secondsSincePeak := now.Sub(peak).Seconds()
	if secondsSincePeak < 3*86400 {
		return 0, ""
	}

	// Match Taskwarrior exactly: work in tasks/second then convert for display.
	fixRatePerSec := float64(peakCount-currentCount) / secondsSincePeak
	fixRatePerDay := fixRatePerSec * 86400
	secondsRemaining := float64(currentCount) / fixRatePerSec
	daysRemaining := secondsRemaining / 86400
	completion := now.Add(time.Duration(secondsRemaining) * time.Second)

	var vague string
	switch {
	case daysRemaining >= 365:
		vague = fmt.Sprintf("%.0fy", daysRemaining/365)
	case daysRemaining >= 30:
		vague = fmt.Sprintf("%.0fmo", daysRemaining/30)
	case daysRemaining >= 7:
		vague = fmt.Sprintf("%.0fw", daysRemaining/7)
	default:
		vague = fmt.Sprintf("%.0fd", daysRemaining)
	}

	return fixRatePerDay, completion.Format("2006-01-02") + " (" + vague + ")"
}

// computeMonthlyHistory builds a newest-first slice of MonthCount covering up
// to 12 months. "Added" is derived from the entry date of every task in both
// slices (open tasks not yet finished; completed tasks within the 12-month
// window). "Completed" is derived from the end date of completed tasks.
func computeMonthlyHistory(open, completed []tw.Task) []views.MonthCount {
	const months = 12
	now := time.Now()
	cutoff := now.AddDate(-1, 0, 0)

	type bucket struct{ added, done int }
	buckets := map[string]*bucket{}
	ensure := func(ym string) *bucket {
		if buckets[ym] == nil {
			buckets[ym] = &bucket{}
		}
		return buckets[ym]
	}

	for _, t := range open {
		if entry, err := tw.ParseTime(t.Entry); err == nil && entry.After(cutoff) {
			ensure(entry.Local().Format("2006-01")).added++
		}
	}
	for _, t := range completed {
		if entry, err := tw.ParseTime(t.Entry); err == nil && entry.After(cutoff) {
			ensure(entry.Local().Format("2006-01")).added++
		}
		if end, err := tw.ParseTime(t.CompletedAt()); err == nil && end.After(cutoff) {
			ensure(end.Local().Format("2006-01")).done++
		}
	}

	result := make([]views.MonthCount, 0, months)
	for i := range months {
		m := now.AddDate(0, -i, 0)
		ym := m.Format("2006-01")
		b := buckets[ym]
		added, done := 0, 0
		if b != nil {
			added, done = b.added, b.done
		}
		// Skip trailing months with no activity at all.
		if added == 0 && done == 0 && i > 0 {
			continue
		}
		result = append(result, views.MonthCount{
			Label:     m.Format("January 2006"),
			YearMonth: ym,
			Added:     added,
			Completed: done,
		})
	}
	return result
}

// computeTimeHistory builds a newest-first slice of TimeDayCount for the last
// N days. Each entry holds the total tracked minutes for that calendar day
// across all provided tasks. Returns nil when no day has tracked time.
func computeTimeHistory(tasks []tw.Task, days int, now time.Time) []views.TimeDayCount {
	buckets := map[string]int{} // YYYY-MM-DD -> minutes
	for _, t := range tasks {
		for _, s := range tw.ParseSessions(t, now) {
			end := s.Stop
			if end.IsZero() {
				end = now
			}
			if end.Before(s.Start) {
				continue
			}
			key := s.Start.Local().Format("2006-01-02")
			buckets[key] += int(end.Sub(s.Start).Minutes())
		}
	}

	hasData := false
	result := make([]views.TimeDayCount, 0, days)
	for i := range days {
		day := now.AddDate(0, 0, -i)
		key := day.Format("2006-01-02")
		m := buckets[key]
		if m > 0 {
			hasData = true
		}
		result = append(result, views.TimeDayCount{
			Label:   day.Format("1/2"),
			Date:    key,
			Minutes: m,
		})
	}
	if !hasData {
		return nil
	}
	return result
}
