package tw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
	maxOutputBytes = 64 << 20 // 64 MiB cap on stdout per invocation
	cacheTTL       = 60 * time.Second

	// stderrTailBytes caps how much stderr we buffer per invocation. Used
	// by writeIfTaskParseError to classify Taskwarrior parser errors as
	// 400s instead of 500s. Bounded so a flood of stderr can't blow memory.
	stderrTailBytes = 4 << 10 // 4 KiB
)

// safetyArgs are prepended to every invocation. confirmation=no neutralises
// interactive prompts; json.array=on guarantees `task export` produces a
// well-formed JSON array even for empty results.
var safetyArgs = []string{
	"rc.confirmation=no",
	"rc.recurrence.confirmation=no",
	"rc.json.array=on",
	"rc.bulk=0",
}

type Client struct {
	binary  string        // empty -> "task" via $PATH
	timeout time.Duration // zero -> defaultTimeout

	// Discovery caches: each is a TTL-invalidated ttlCache so a transient
	// `task` failure during the first call doesn't poison the cache to
	// empty for the process lifetime, and so newly-defined projects /
	// tags / UDAs / contexts surface within cacheTTL of being added via
	// the CLI without a server restart. Replaces the v0-v5 sync.Once
	// pattern, which had both problems.
	udas     ttlCache[[]UDA]
	projects ttlCache[[]string]
	tags     ttlCache[[]string]
	contexts ttlCache[[]Context]
	reports  ttlCache[[]string]

	// filterCache memoises `task _get rc.report.<name>.filter` per name for
	// the Client's lifetime. Each entry is a *filterEntry whose sync.Once
	// gates the underlying shell call; concurrent callers for the same
	// report name share a single fetch. Per-report filters are stored in
	// ~/.taskrc and don't change on a normal day, so a once-cache is
	// adequate here (the discovery caches above use TTL because they're
	// derived from task data which DOES change).
	filterCache sync.Map // string -> *filterEntry

	// importMu serialises the export-mutate-import critical section in
	// ReplaceIntervals so two concurrent edits on the same task can't
	// race-overwrite each other (the second's Export wouldn't see the
	// first's not-yet-imported writes). Single-user threat model means
	// the practical contention is near-zero, but the cost of holding the
	// mutex is trivial against the subprocess latency it gates.
	importMu sync.Mutex
}

type filterEntry struct {
	once  sync.Once
	value string
}

// ttlCache is a lazy single-value TTL cache. On miss it calls fetch under
// lock; on fetch error it keeps the prior value if any (don't poison: a
// transient binary failure shouldn't blank the dropdown for the rest of
// the process lifetime). Generic over T so the four discovery lists share
// one implementation.
type ttlCache[T any] struct {
	mu      sync.Mutex
	value   T
	expiry  time.Time
	fetched bool
}

func (c *ttlCache[T]) invalidate() {
	c.mu.Lock()
	c.expiry = time.Time{} // zero is always Before any real time, forcing a re-fetch
	c.mu.Unlock()
}

func (c *ttlCache[T]) load(ctx context.Context, fetch func(context.Context) (T, error)) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fetched && time.Now().Before(c.expiry) {
		return c.value
	}
	v, err := fetch(ctx)
	if err != nil {
		// Don't poison: keep the prior value if we have one. On true
		// first-call failure (fetched == false), c.value is the zero
		// value of T, which matches what a fresh sync.Once + nil-error
		// path would have produced.
		return c.value
	}
	c.value = v
	c.expiry = time.Now().Add(cacheTTL)
	c.fetched = true
	return c.value
}

// ClientOption configures a Client at construction time. Functional-options
// pattern keeps NewClient() backwards compatible while letting tests inject
// a fake binary path or a tighter timeout without setting unexported
// fields directly.
type ClientOption func(*Client)

// WithTimeout overrides the per-invocation timeout. Zero or negative falls
// back to defaultTimeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithBinary overrides the `task` binary path. Empty string falls back to
// $PATH lookup. Used in tests to point at a fake script instead.
func WithBinary(path string) ClientOption {
	return func(c *Client) { c.binary = path }
}

func NewClient(opts ...ClientOption) *Client {
	c := &Client{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// argv builds the argv slice for a Taskwarrior invocation by concatenating
// safetyArgs with the caller's args. Single helper instead of nine
// `append(append([]string{}, safetyArgs...), ...)` sites scattered through
// this file.
func (c *Client) argv(args ...string) []string {
	full := make([]string, 0, len(safetyArgs)+len(args))
	full = append(full, safetyArgs...)
	full = append(full, args...)
	return full
}

// ErrUnsafeArg is a defence-in-depth signal: handlers should already have
// validated input, but this catches accidental rc.* propagation.
var ErrUnsafeArg = errors.New("unsafe argument")

func guardArgs(args []string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "rc.") {
			return fmt.Errorf("%w: rc.* override not permitted from caller", ErrUnsafeArg)
		}
	}
	return nil
}

// Export runs `task <args> export` and decodes the JSON array into []Task.
// args are filter/report expressions (e.g. "limit:3", "+READY", "agenda").
func (c *Client) Export(ctx context.Context, args ...string) ([]Task, error) {
	if err := guardArgs(args); err != nil {
		return nil, err
	}
	full := c.argv(args...)
	full = append(full, "export")

	out, err := c.runRaw(ctx, full)
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	var tasks []Task
	if err := json.Unmarshal(out, &tasks); err != nil {
		return nil, fmt.Errorf("decode task export: %w", err)
	}
	return tasks, nil
}

// Run executes `task <args>` for non-export operations (add, modify, done,
// delete). Use AddArgs to construct args safely from user input.
func (c *Client) Run(ctx context.Context, args ...string) error {
	if err := guardArgs(args); err != nil {
		return err
	}
	full := c.argv(args...)
	_, err := c.runRaw(ctx, full)
	if err == nil || IsNoOpExit(err) {
		c.tags.invalidate()
		c.projects.invalidate()
	}
	return err
}

// RunNoContext executes `task rc.context=none <args>`, bypassing any active
// context filter. Use this for bulk admin operations (rename, merge) that must
// affect ALL matching tasks regardless of what context the user currently has
// active - a project-based context would otherwise AND with a project: filter
// and silently match nothing.
func (c *Client) RunNoContext(ctx context.Context, args ...string) error {
	if err := guardArgs(args); err != nil {
		return err
	}
	full := make([]string, 0, len(safetyArgs)+1+len(args))
	full = append(full, safetyArgs...)
	full = append(full, "rc.context=none")
	full = append(full, args...)
	_, err := c.runRaw(ctx, full)
	if err == nil || IsNoOpExit(err) {
		c.tags.invalidate()
		c.projects.invalidate()
	}
	return err
}

// Add executes `task add <args>`. When bypassContextWrite is true it injects
// rc.context=none ahead of user args so Taskwarrior skips native write-filter
// application; this is used when the active context's write filter contains
// logical operators that Taskwarrior would silently prepend to the description.
// The rc.context=none token is generated internally and is not subject to
// guardArgs (which only checks caller-supplied args).
func (c *Client) Add(ctx context.Context, bypassContextWrite bool, args ...string) error {
	if err := guardArgs(args); err != nil {
		return err
	}
	var full []string
	if bypassContextWrite {
		full = make([]string, 0, len(safetyArgs)+1+len(args))
		full = append(full, safetyArgs...)
		full = append(full, "rc.context=none")
		full = append(full, args...)
	} else {
		full = c.argv(args...)
	}
	_, err := c.runRaw(ctx, full)
	if err == nil {
		c.tags.invalidate()
		c.projects.invalidate()
	}
	return err
}

// Annotate appends a note to the task. Description is treated as literal
// positional text via the `--` separator so embedded DOM tokens (+tag,
// due:tomorrow, rc.foo=bar) are stored verbatim and never interpreted as
// attribute modifiers.
//
// Empty (whitespace-only) text returns ErrInvalid; text longer than
// MaxDescriptionBytes is rejected to keep argv well under any platform's
// ARG_MAX.
func (c *Client) Annotate(ctx context.Context, id, text string) error {
	if !IDPattern.MatchString(id) {
		return fmt.Errorf("%w: id %q", ErrInvalid, id)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: annotation text is required", ErrInvalid)
	}
	if len(text) > MaxDescriptionBytes {
		return fmt.Errorf("%w: annotation text too long (max %d bytes)", ErrInvalid, MaxDescriptionBytes)
	}
	return c.Run(ctx, id, "annotate", "--", text)
}

// Denotate removes the annotation whose description matches text. Taskwarrior
// uses substring match; we pass the full description verbatim via `--` so
// punctuation in the note (+tag, due:..., shell metachars) is treated as
// positional text.
func (c *Client) Denotate(ctx context.Context, id, text string) error {
	if !IDPattern.MatchString(id) {
		return fmt.Errorf("%w: id %q", ErrInvalid, id)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: annotation text is required", ErrInvalid)
	}
	return c.Run(ctx, id, "denotate", "--", text)
}

// Start marks a task as actively being worked on (Taskwarrior records the
// timestamp on the task's `start` field, which then drives the +ACTIVE
// virtual tag). Idempotent in Taskwarrior - re-starting an already-active
// task is a no-op.
func (c *Client) Start(ctx context.Context, id string) error {
	if !IDPattern.MatchString(id) {
		return fmt.Errorf("%w: id %q", ErrInvalid, id)
	}
	return c.Run(ctx, id, "start")
}

// Stop clears the `start` timestamp set by Start. Idempotent: stopping an
// already-stopped task is a no-op in Taskwarrior.
func (c *Client) Stop(ctx context.Context, id string) error {
	if !IDPattern.MatchString(id) {
		return fmt.Errorf("%w: id %q", ErrInvalid, id)
	}
	return c.Run(ctx, id, "stop")
}

// Duplicate is `task <id> duplicate` - clones the task's editable fields
// (description, project, tags, due/wait/scheduled, UDAs) into a new pending
// task. Recurrence is NOT carried across; that's a Taskwarrior policy
// decision and exactly what we want for "create similar task".
func (c *Client) Duplicate(ctx context.Context, id string) error {
	if !IDPattern.MatchString(id) {
		return fmt.Errorf("%w: id %q", ErrInvalid, id)
	}
	return c.Run(ctx, id, "duplicate")
}

// IntervalEdit describes a change to an existing on-disk interval.
// Identity is the on-disk timestamp pair (OriginalStart, OriginalEnd) -
// the same pair ParseSessions would emit for that journal-annotation
// pair. OriginalEnd is the zero time for an active (open) interval -
// only a Started annotation exists, no Stopped yet.
type IntervalEdit struct {
	OriginalStart time.Time
	OriginalEnd   time.Time
	Start         time.Time
	Stop          time.Time // zero -> the edited interval is left open
}

// IntervalCreate is a brand new interval to record on the task.
type IntervalCreate struct {
	Start time.Time
	Stop  time.Time // zero -> active / open
}

// IntervalDelete removes an existing interval. Identity matches
// IntervalEdit's Original* fields.
type IntervalDelete struct {
	OriginalStart time.Time
	OriginalEnd   time.Time
}

// Sentinel validation errors returned by UpdateIntervals. The handler
// inspects them via errors.Is to map onto user-facing HTTP errors. We
// keep them typed (not just string-wrapping) so the handler isn't
// forced into substring matching against error messages, which rots
// silently when phrasing changes.
var (
	// ErrIntervalOverlap is returned when the resulting state would
	// contain two intervals whose [Start, Stop) ranges intersect.
	ErrIntervalOverlap = errors.New("intervals overlap")
	// ErrMultipleOpenIntervals: at most one open (no-end) interval
	// is permitted per task. ParseSessions can only meaningfully
	// surface one as "currently active"; the rest would be orphans.
	ErrMultipleOpenIntervals = errors.New("at most one open interval permitted")
	// ErrOpenIntervalRequiresActive: an open interval is only valid
	// when the task is currently being tracked (task.Start set).
	// Otherwise the dangling Started annotation has no logical
	// matching Stopped and ParseSessions would drop it.
	ErrOpenIntervalRequiresActive = errors.New("open interval requires task to be active")
)

// UpdateIntervals applies a DIFF to the task's time-tracking history,
// rather than replacing the whole history. Each existing journal
// pair is identified by its on-disk (StartedAt, StoppedAt) timestamps;
// edits and deletes target specific pairs by that key, creates add
// new pairs, and any existing pair NOT referenced by an edit/delete
// is left exactly as it was.
//
// This is the safe primitive for a paginated editor: the FE can
// submit changes to a subset of rows without that submission
// implicitly meaning "delete every other interval on this task".
// The previous full-replace `ReplaceIntervals` was a data-loss
// footgun when paired with FE pagination - it took out everything
// the FE didn't happen to have loaded.
//
// Behaviour:
//   - Non-journal annotations (user notes) pass through untouched.
//   - Journal pairs are re-paired greedily from sorted Started/
//     Stopped lists, mirroring ParseSessions' read-side pairing so
//     the keying scheme matches what the FE rendered. Orphan stops
//     are dropped (same as ParseSessions).
//   - Edits/deletes whose Original* key matches no existing pair are
//     silently no-ops (the pair was already gone, e.g. an out-of-
//     band CLI invocation removed it between FE render and submit).
//   - Final annotation list is sorted by entry timestamp so the next
//     ParseSessions sees a canonical order.
//
// Validation: cross-state invariants (overlap, single-open-only,
// open-only-when-active) are checked AFTER applying the diff so the
// result reflects the user's intended end state regardless of which
// of the existing/created/edited intervals contribute to the
// conflict. Per-item shape checks (zero start, etc.) are also done
// here; the HTTP handler still validates user-input shape (future
// dates, end-before-start) at the request boundary, but this method
// must remain safe to call from any future internal path.
//
// Concurrency: the existing importMu serialises export → mutate →
// import so two concurrent saves on the same task can't race.
// Single-record `task import` is atomic per the TW docs, so a parse
// failure aborts before any commit.
//
// `task undo` rollback: the entire mutation is a single `task
// import` operation, so undo reverses it cleanly - same as the
// prior ReplaceIntervals approach.
func (c *Client) UpdateIntervals(ctx context.Context, id string, edits []IntervalEdit, creates []IntervalCreate, deletes []IntervalDelete, taskActive bool) error {
	if !IDPattern.MatchString(id) {
		return fmt.Errorf("%w: id %q", ErrInvalid, id)
	}
	for _, cr := range creates {
		if cr.Start.IsZero() {
			return fmt.Errorf("%w: create has zero start", ErrInvalid)
		}
	}
	for _, e := range edits {
		if e.Start.IsZero() {
			return fmt.Errorf("%w: edit has zero new start", ErrInvalid)
		}
	}

	c.importMu.Lock()
	defer c.importMu.Unlock()

	// Round-trip the EXPORT BYTES through a json.RawMessage map rather
	// than the typed Task struct. Typed round-trip leaks two bugs:
	//   1. Task.UDAs is map[string]string so numeric / boolean UDA
	//      values get stringified on re-marshal.
	//   2. Task.ID has no `omitempty`, so the (post-import meaningless)
	//      short id gets re-emitted.
	// Mutating only the `annotations` key of the raw map preserves
	// every other field exactly as TW emitted it.
	rawBytes, err := c.runRaw(ctx, append(c.argv(id), "export"))
	if err != nil {
		return fmt.Errorf("export for interval update: %w", err)
	}
	rawBytes = bytes.TrimSpace(rawBytes)
	var records []json.RawMessage
	if err := json.Unmarshal(rawBytes, &records); err != nil {
		return fmt.Errorf("decode export for interval update: %w", err)
	}
	if len(records) != 1 {
		return fmt.Errorf("%w: expected 1 task for %q, got %d", ErrInvalid, id, len(records))
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(records[0], &record); err != nil {
		return fmt.Errorf("decode task record: %w", err)
	}

	var anns []Annotation
	if rawAnns, ok := record["annotations"]; ok && len(rawAnns) > 0 {
		if err := json.Unmarshal(rawAnns, &anns); err != nil {
			return fmt.Errorf("decode annotations: %w", err)
		}
	}

	final, err := applyIntervalDiff(anns, edits, creates, deletes)
	if err != nil {
		return err
	}
	if err := validateFinalIntervals(final, taskActive); err != nil {
		return err
	}

	newAnns, err := json.Marshal(final)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}
	record["annotations"] = newAnns

	out, err := json.Marshal([]map[string]json.RawMessage{record})
	if err != nil {
		return fmt.Errorf("marshal task for import: %w", err)
	}
	if err := c.Import(ctx, out); err != nil {
		return fmt.Errorf("import after interval update: %w", err)
	}
	return nil
}

// IntervalOriginKind tags where a planned pair came from. Callers
// use this to produce error messages that name the originating
// operation (e.g. "create 0 overlaps with edit 1") rather than just
// "overlap" - critical for the FE to highlight the exact rows.
type IntervalOriginKind int

const (
	// OriginExisting: the pair was already on the task and not
	// referenced by any edit/delete in the submitted diff.
	OriginExisting IntervalOriginKind = iota
	// OriginEdit: the pair came from the new values of an
	// IntervalEdit; Index is its position in the edits slice.
	OriginEdit
	// OriginCreate: the pair came from an IntervalCreate; Index is
	// its position in the creates slice.
	OriginCreate
)

// IntervalOrigin describes a planned pair's provenance.
type IntervalOrigin struct {
	Kind  IntervalOriginKind
	Index int // -1 for Existing; >= 0 for Edit/Create.
}

// IntervalPlanItem is one pair in the post-diff final state. Used by
// callers that need to validate or describe the post-diff result
// before the actual import - in particular the HTTP handler's
// index-aware overlap detection.
type IntervalPlanItem struct {
	Start  time.Time
	Stop   time.Time // zero -> open
	Origin IntervalOrigin
}

// PlanIntervalUpdate computes what the task's interval set will look
// like after applying the supplied diff to the supplied existing
// annotations - WITHOUT writing anything. Each returned pair carries
// an Origin so callers can emit error messages that point back at
// specific rows in the user's submission.
//
// Pairing mirrors ParseSessions (so identity by (StartedAt, StoppedAt)
// matches what the FE rendered) and so does the diff application -
// this is the same algorithm applyIntervalDiff uses internally, just
// stopped one step short of annotation re-emission.
func PlanIntervalUpdate(existing []Annotation, edits []IntervalEdit, creates []IntervalCreate, deletes []IntervalDelete) []IntervalPlanItem {
	var startAnns, stopAnns []Annotation
	for _, a := range existing {
		if !IsJournalAnnotation(a.Description) {
			continue
		}
		desc := strings.TrimSpace(a.Description)
		switch {
		case desc == JournalStartDescription || strings.HasPrefix(desc, JournalStartDescription+" ") || strings.HasPrefix(desc, "Started "):
			startAnns = append(startAnns, a)
		case desc == JournalStopDescription || strings.HasPrefix(desc, JournalStopDescription+" ") || strings.HasPrefix(desc, "Stopped "):
			stopAnns = append(stopAnns, a)
		}
	}
	sort.SliceStable(startAnns, func(i, j int) bool { return startAnns[i].Entry < startAnns[j].Entry })
	sort.SliceStable(stopAnns, func(i, j int) bool { return stopAnns[i].Entry < stopAnns[j].Entry })

	type pairState struct {
		startEntry string
		stopEntry  string
		deleted    bool
		edited     bool
		editIdx    int
		newStart   time.Time
		newStop    time.Time
	}
	pairs := make([]pairState, 0, len(startAnns))
	si := 0
	for _, sa := range startAnns {
		for si < len(stopAnns) && stopAnns[si].Entry < sa.Entry {
			si++
		}
		if si < len(stopAnns) {
			pairs = append(pairs, pairState{startEntry: sa.Entry, stopEntry: stopAnns[si].Entry})
			si++
		} else {
			pairs = append(pairs, pairState{startEntry: sa.Entry})
		}
	}

	pairIdx := make(map[string][]int)
	for i, p := range pairs {
		key := p.startEntry + "|" + p.stopEntry
		pairIdx[key] = append(pairIdx[key], i)
	}
	take := func(key string) (int, bool) {
		list, ok := pairIdx[key]
		if !ok || len(list) == 0 {
			return 0, false
		}
		idx := list[0]
		pairIdx[key] = list[1:]
		return idx, true
	}
	keyFor := func(start, stop time.Time) string {
		k := FormatTime(start.UTC()) + "|"
		if !stop.IsZero() {
			k += FormatTime(stop.UTC())
		}
		return k
	}

	for _, d := range deletes {
		if idx, ok := take(keyFor(d.OriginalStart, d.OriginalEnd)); ok {
			pairs[idx].deleted = true
		}
	}
	for i, e := range edits {
		if idx, ok := take(keyFor(e.OriginalStart, e.OriginalEnd)); ok {
			pairs[idx].edited = true
			pairs[idx].editIdx = i
			pairs[idx].newStart = e.Start.UTC()
			pairs[idx].newStop = e.Stop.UTC()
		}
	}

	out := make([]IntervalPlanItem, 0, len(pairs)+len(creates))
	for _, p := range pairs {
		if p.deleted {
			continue
		}
		if p.edited {
			out = append(out, IntervalPlanItem{
				Start:  p.newStart,
				Stop:   p.newStop,
				Origin: IntervalOrigin{Kind: OriginEdit, Index: p.editIdx},
			})
			continue
		}
		// Existing untouched pair - parse its on-disk timestamps.
		// We can't easily fail here; if a malformed entry slipped
		// through (shouldn't happen since the read side would have
		// already rejected it), skip it rather than crash.
		start, err := ParseTime(p.startEntry)
		if err != nil {
			continue
		}
		var stop time.Time
		if p.stopEntry != "" {
			if s, err := ParseTime(p.stopEntry); err == nil {
				stop = s
			}
		}
		out = append(out, IntervalPlanItem{
			Start:  start,
			Stop:   stop,
			Origin: IntervalOrigin{Kind: OriginExisting, Index: -1},
		})
	}
	for i, cr := range creates {
		out = append(out, IntervalPlanItem{
			Start:  cr.Start.UTC(),
			Stop:   cr.Stop.UTC(),
			Origin: IntervalOrigin{Kind: OriginCreate, Index: i},
		})
	}
	return out
}

// applyIntervalDiff produces the new annotation list given the
// existing annotations and the diff. Non-journal annotations carry
// through untouched. Journal pairs are reconstructed via the same
// greedy pairing ParseSessions uses (so identity by (StartedAt,
// StoppedAt) is well-defined), then deletes/edits are applied by key
// and creates are appended.
//
// Edits and deletes that don't match an existing pair are silently
// ignored - the pair was already removed by another caller between
// the FE rendering it and submitting the diff. Erroring would create
// a footgun where stale tabs can never save.
//
// Exposed for testing (the read-side pairing logic is subtle enough
// that direct unit tests beat round-tripping through UpdateIntervals).
func applyIntervalDiff(existing []Annotation, edits []IntervalEdit, creates []IntervalCreate, deletes []IntervalDelete) ([]Annotation, error) {
	var nonJournal []Annotation
	var startAnns, stopAnns []Annotation
	for _, a := range existing {
		if !IsJournalAnnotation(a.Description) {
			nonJournal = append(nonJournal, a)
			continue
		}
		desc := strings.TrimSpace(a.Description)
		switch {
		case desc == JournalStartDescription || strings.HasPrefix(desc, JournalStartDescription+" "):
			startAnns = append(startAnns, a)
		case desc == JournalStopDescription || strings.HasPrefix(desc, JournalStopDescription+" "):
			stopAnns = append(stopAnns, a)
		case strings.HasPrefix(desc, "Started "):
			// TW 2.x legacy: timestamp embedded in description.
			// We never WRITE this form, but we tolerate reading it so
			// the diff round-trips on historical data. Same heuristic
			// as ParseSessions / IsJournalAnnotation.
			startAnns = append(startAnns, a)
		case strings.HasPrefix(desc, "Stopped "):
			stopAnns = append(stopAnns, a)
		}
	}

	// FormatTime emits YYYYMMDDTHHMMSSZ which is lexically time-
	// ordered, so a string sort is equivalent to a time sort.
	sort.SliceStable(startAnns, func(i, j int) bool { return startAnns[i].Entry < startAnns[j].Entry })
	sort.SliceStable(stopAnns, func(i, j int) bool { return stopAnns[i].Entry < stopAnns[j].Entry })

	type pairState struct {
		startEntry string // YYYYMMDDTHHMMSSZ
		stopEntry  string // "" if open
		deleted    bool
		edited     bool
		newStart   time.Time
		newStop    time.Time // zero -> open
	}
	pairs := make([]pairState, 0, len(startAnns))
	si := 0
	for _, sa := range startAnns {
		// Advance past stops at or before this start (orphans). Mirrors
		// the !stops[si].After(start) check in ParseSessions.
		for si < len(stopAnns) && stopAnns[si].Entry < sa.Entry {
			si++
		}
		if si < len(stopAnns) {
			pairs = append(pairs, pairState{startEntry: sa.Entry, stopEntry: stopAnns[si].Entry})
			si++
		} else {
			pairs = append(pairs, pairState{startEntry: sa.Entry})
		}
	}

	// Multi-map: when two pairs happen to share an identical (Started,
	// Stopped) key (rare but possible after a manual data manipulation),
	// deletes/edits consume them in original order so a second delete
	// with the same key hits the second pair, not the same one again.
	pairIdx := make(map[string][]int)
	for i, p := range pairs {
		key := p.startEntry + "|" + p.stopEntry
		pairIdx[key] = append(pairIdx[key], i)
	}
	takeIdx := func(key string) (int, bool) {
		list, ok := pairIdx[key]
		if !ok || len(list) == 0 {
			return 0, false
		}
		idx := list[0]
		pairIdx[key] = list[1:]
		return idx, true
	}
	keyFor := func(start, stop time.Time) string {
		key := FormatTime(start.UTC()) + "|"
		if !stop.IsZero() {
			key += FormatTime(stop.UTC())
		}
		return key
	}

	for _, d := range deletes {
		if idx, ok := takeIdx(keyFor(d.OriginalStart, d.OriginalEnd)); ok {
			pairs[idx].deleted = true
		}
	}
	for _, e := range edits {
		if idx, ok := takeIdx(keyFor(e.OriginalStart, e.OriginalEnd)); ok {
			pairs[idx].edited = true
			pairs[idx].newStart = e.Start.UTC()
			pairs[idx].newStop = e.Stop.UTC()
		}
	}

	final := make([]Annotation, 0, len(existing)+2*len(creates))
	final = append(final, nonJournal...)
	for _, p := range pairs {
		if p.deleted {
			continue
		}
		startEntry, stopEntry := p.startEntry, p.stopEntry
		if p.edited {
			startEntry = FormatTime(p.newStart)
			stopEntry = ""
			if !p.newStop.IsZero() {
				stopEntry = FormatTime(p.newStop)
			}
		}
		final = append(final, Annotation{Entry: startEntry, Description: JournalStartDescription})
		if stopEntry != "" {
			final = append(final, Annotation{Entry: stopEntry, Description: JournalStopDescription})
		}
	}
	for _, cr := range creates {
		final = append(final, Annotation{Entry: FormatTime(cr.Start.UTC()), Description: JournalStartDescription})
		if !cr.Stop.IsZero() {
			final = append(final, Annotation{Entry: FormatTime(cr.Stop.UTC()), Description: JournalStopDescription})
		}
	}

	sort.SliceStable(final, func(i, j int) bool { return final[i].Entry < final[j].Entry })
	return final, nil
}

// validateFinalIntervals enforces cross-state invariants on the
// resulting annotation list. It pairs the journal annotations exactly
// like the read side (ParseSessions) does, then checks:
//   - at most one open interval
//   - if any interval is open, the task must be active
//   - no two intervals overlap (chronologically sorted, walk pairs)
//
// Overlap detection runs across the FULL final state, which means a
// new create on page 1 that overlaps with an existing pair on page 3
// is caught - something the previous full-replace handler couldn't
// see because it never knew about the page 3 data.
func validateFinalIntervals(anns []Annotation, taskActive bool) error {
	var startAnns, stopAnns []Annotation
	for _, a := range anns {
		desc := strings.TrimSpace(a.Description)
		if !IsJournalAnnotation(desc) {
			continue
		}
		switch {
		case desc == JournalStartDescription || strings.HasPrefix(desc, JournalStartDescription+" ") || strings.HasPrefix(desc, "Started "):
			startAnns = append(startAnns, a)
		case desc == JournalStopDescription || strings.HasPrefix(desc, JournalStopDescription+" ") || strings.HasPrefix(desc, "Stopped "):
			stopAnns = append(stopAnns, a)
		}
	}
	sort.SliceStable(startAnns, func(i, j int) bool { return startAnns[i].Entry < startAnns[j].Entry })
	sort.SliceStable(stopAnns, func(i, j int) bool { return stopAnns[i].Entry < stopAnns[j].Entry })

	type pair struct {
		start, stop time.Time
		open        bool
	}
	pairs := make([]pair, 0, len(startAnns))
	si := 0
	for _, sa := range startAnns {
		for si < len(stopAnns) && stopAnns[si].Entry < sa.Entry {
			si++
		}
		start, err := ParseTime(sa.Entry)
		if err != nil {
			return fmt.Errorf("validate: bad start entry %q: %w", sa.Entry, err)
		}
		if si < len(stopAnns) {
			stop, err := ParseTime(stopAnns[si].Entry)
			if err != nil {
				return fmt.Errorf("validate: bad stop entry %q: %w", stopAnns[si].Entry, err)
			}
			pairs = append(pairs, pair{start: start, stop: stop})
			si++
		} else {
			pairs = append(pairs, pair{start: start, open: true})
		}
	}

	openCount := 0
	for _, p := range pairs {
		if p.open {
			openCount++
		}
	}
	if openCount > 1 {
		return ErrMultipleOpenIntervals
	}
	if openCount == 1 && !taskActive {
		return ErrOpenIntervalRequiresActive
	}

	// Overlap check. Sort pairs by start time so adjacent pairs are
	// the only ones we need to compare. Treat the (single) open pair
	// as extending to a sentinel +∞ for the comparison - if a later
	// closed pair begins after the open one's start, it overlaps with
	// the still-running interval.
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].start.Before(pairs[j].start) })
	farFuture := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	for i := 1; i < len(pairs); i++ {
		prev := pairs[i-1]
		curr := pairs[i]
		prevEnd := prev.stop
		if prev.open {
			prevEnd = farFuture
		}
		if curr.start.Before(prevEnd) {
			return ErrIntervalOverlap
		}
	}
	return nil
}

// Import pipes the given JSON byte slice to `task import -` via stdin. This
// is the ONLY mechanism Taskwarrior exposes for mutating an existing task's
// annotation entry-timestamps - the argv-based `task annotate` always stamps
// `now`, so retroactive interval editing has to round-trip via export +
// re-import.
//
// Argv contains a single constant flag (`-` reads stdin); no user input
// touches argv. Safety:
//   - The JSON payload is server-constructed from a typed Task struct;
//     callers should never pass FE-controlled bytes directly.
//   - guardArgs still inspects our `import` and `-` args (they pass).
//   - stderr is captured into TaskExitError for parse-error classification.
//   - Stdin write is bounded by the buffer the caller hands us; nothing
//     here memory-blows on a giant payload.
//
// Returns TaskExitError when `task` exits non-zero (e.g. malformed JSON or
// schema mismatch). Per `task import` docs the operation is atomic per
// record on the matching UUID: a parse failure aborts before any record
// commits.
func (c *Client) Import(ctx context.Context, raw []byte) error {
	// Caller args are the constants "import" and "-"; guardArgs runs on
	// these (pre-argv) so it sees only the un-prefixed slice and never
	// trips on our own rc.* safety prepends. Same shape Run uses.
	callerArgs := []string{"import", "-"}
	if err := guardArgs(callerArgs); err != nil {
		return err
	}
	args := c.argv(callerArgs...)

	timeout := c.timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin := c.binary
	if bin == "" {
		bin = "task"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = bytes.NewReader(raw)
	// Bound stdout/stderr via pipes + LimitReader so a hostile or wedged
	// `task import` writing gigabytes can't OOM us. Mirrors runRaw's
	// discipline; the previous `bytes.Buffer` shape would buffer the
	// entire stream before applying the stderrTailBytes truncation.
	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("import stdout pipe: %w", err)
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("import stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start task import: %w", err)
	}
	stdoutBytes, _ := io.ReadAll(io.LimitReader(stdoutR, maxOutputBytes))
	stderrBytes, _ := io.ReadAll(io.LimitReader(stderrR, maxOutputBytes))

	if err := cmd.Wait(); err != nil {
		stderr := stderrBytes
		if len(stderr) > stderrTailBytes {
			stderr = stderr[len(stderr)-stderrTailBytes:]
		}
		stdout := stdoutBytes
		if len(stdout) > stderrTailBytes {
			stdout = stdout[len(stdout)-stderrTailBytes:]
		}
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return &TaskExitError{
			ExitCode: exitCode,
			Stderr:   string(stderr),
			Stdout:   string(stdout),
			Wrapped:  err,
		}
	}
	return nil
}

// ResolveReportFilter returns the filter expression configured for the named
// report in the user's .taskrc (e.g. "report.agenda.filter"). Empty string
// (no error) if the report has no filter defined.
//
// This calls `task _get rc.report.<name>.filter`. The rc.* arg here is
// trusted (constructed from a hardcoded report name, not user input), so it
// bypasses guardArgs by going through runRaw directly.
func (c *Client) ResolveReportFilter(ctx context.Context, name string) (string, error) {
	// Belt-and-braces validation: only allow simple report names.
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return "", fmt.Errorf("%w: invalid report name %q", ErrUnsafeArg, name)
		}
	}
	if name == "" {
		return "", fmt.Errorf("%w: empty report name", ErrUnsafeArg)
	}

	args := c.argv("_get", "rc.report."+name+".filter")
	out, err := c.runRaw(ctx, args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ReportFilterCached is the memoised counterpart to ResolveReportFilter:
// the per-name lookup is shelled at most once per Client lifetime, after
// which the cached string is returned to every caller. On error (any kind:
// invalid name, exec failure, timeout) the empty string is cached, so the
// handler-side fallback path runs once and stays stable. Logging the
// underlying error is the caller's responsibility - this method intentionally
// surfaces nothing about how the value was obtained.
func (c *Client) ReportFilterCached(ctx context.Context, name string) string {
	v, _ := c.filterCache.LoadOrStore(name, &filterEntry{})
	entry := v.(*filterEntry)
	entry.once.Do(func() {
		got, _ := c.ResolveReportFilter(ctx, name)
		entry.value = got
	})
	return entry.value
}

// ListUDAs returns the User-Defined Attributes currently declared in the
// user's ~/.taskrc, with their declared type and label. Empty result with nil
// error means no UDAs are defined.
//
// Implementation: shells `task _udas` to enumerate names, then per-name calls
// `task _get rc.uda.<name>.type` and `task _get rc.uda.<name>.label`. Names
// failing UDANamePattern are dropped silently as a defence-in-depth filter
// against a hostile taskrc smuggling parser tokens through the cache.
//
// Both _udas and _get rc.uda.* arguments are constructed entirely from
// constants and validated names (no user input), so they go via runRaw and
// bypass guardArgs (which would otherwise reject the rc.uda.* prefix).
func (c *Client) ListUDAs(ctx context.Context) ([]UDA, error) {
	listArgs := c.argv("_udas")
	out, err := c.runRaw(ctx, listArgs)
	if err != nil {
		return nil, fmt.Errorf("list udas: %w", err)
	}

	var udas []UDA
	for line := range strings.SplitSeq(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if !UDANamePattern.MatchString(name) {
			continue
		}

		typ := c.getRcKey(ctx, "rc.uda."+name+".type")
		label := c.getRcKey(ctx, "rc.uda."+name+".label")
		values := parseUDAValues(c.getRcKey(ctx, "rc.uda."+name+".values"))
		udas = append(udas, UDA{Name: name, Type: typ, Label: label, Values: values, BuiltIn: builtinUDANames[name]})
	}
	return udas, nil
}

// parseUDAValues splits the comma-separated value list returned by
// `task _get rc.uda.<name>.values`, dropping the trailing empty entry that
// Taskwarrior emits to signal "empty allowed" (the form layer always offers
// a separate clear option). Empty input returns nil so the caller can
// distinguish "no enum constraint" from "enum with no values" cleanly.
func parseUDAValues(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// JournalTimeEnabled reports whether `rc.journal.time` is set to "yes" in the
// active taskrc. With it enabled, taskwarrior appends "Started …" / "Stopped …"
// annotations on every start/stop call, giving the web UI enough data to build
// a timesheet.
func (c *Client) JournalTimeEnabled(ctx context.Context) bool {
	return strings.EqualFold(c.getRcKey(ctx, "rc.journal.time"), "yes")
}

// EnableJournalTime writes `journal.time=yes` to the active taskrc via
// `task config journal.time yes`. Idempotent - safe to call if already on.
func (c *Client) EnableJournalTime(ctx context.Context) error {
	return c.Run(ctx, "config", "journal.time", "yes")
}

// getRcKey looks up a single rc.* key with a short timeout, returning empty
// string on any error so the caller can fall back to its default. The key is
// trusted (constructed from a validated UDA name and a constant suffix) so
// this bypasses guardArgs.
func (c *Client) getRcKey(ctx context.Context, key string) string {
	args := c.argv("_get", key)
	out, err := c.runRaw(ctx, args)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// UDAsCached returns the UDA list, computing it once per Client lifetime. The
// underlying ListUDAs call is gated by sync.Once; on first-call failure the
// cached value is the empty list and subsequent calls return that empty list
// without retrying.
func (c *Client) UDAsCached(ctx context.Context) []UDA {
	return c.udas.load(ctx, c.ListUDAs)
}

// runRaw is the trusted-caller entry point that skips guardArgs. Callers
// (Export, Run, ResolveReportFilter) are responsible for either validating
// user-supplied args via guardArgs or passing only constants/internally
// constructed args.
//
// stderr is captured into a bounded scratch buffer (stderrTailBytes) and
// folded into the returned error as a *TaskExitError when the command
// exits non-zero. This lets handlers classify Taskwarrior parser errors
// (e.g. "Could not interpret the date 'x'.") as 400s instead of generic
// 500s. The captured stderr is NEVER logged - the error type carries it
// for in-process classification only, then it's discarded.
func (c *Client) runRaw(ctx context.Context, args []string) ([]byte, error) {
	timeout := c.timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin := c.binary
	if bin == "" {
		bin = "task"
	}
	cmd := exec.CommandContext(ctx, bin, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// stderr goes to a bounded buffer for classification only; it's never
	// logged or surfaced in plaintext - Taskwarrior may echo descriptions
	// or UUIDs on parse errors, so the buffer is held in the returned
	// error type and consumed by handlers that need to fingerprint the
	// failure shape.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start task: %w", err)
	}

	out, readErr := io.ReadAll(io.LimitReader(stdout, maxOutputBytes))

	if waitErr := cmd.Wait(); waitErr != nil {
		stderr := stderrBuf.Bytes()
		if len(stderr) > stderrTailBytes {
			stderr = stderr[len(stderr)-stderrTailBytes:]
		}
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		stdoutTail := out
		if len(stdoutTail) > stderrTailBytes {
			stdoutTail = stdoutTail[len(stdoutTail)-stderrTailBytes:]
		}
		return out, &TaskExitError{
			ExitCode: exitCode,
			Stderr:   string(stderr),
			Stdout:   string(stdoutTail),
			Wrapped:  waitErr,
		}
	}
	if readErr != nil {
		return out, fmt.Errorf("read task stdout: %w", readErr)
	}
	if int64(len(out)) >= maxOutputBytes {
		// Discard the partial buffer: callers always treat a non-nil err as
		// "no usable output", and returning it muddies that contract.
		return nil, fmt.Errorf("task output exceeded %d bytes", maxOutputBytes)
	}
	return out, nil
}

// TaskExitError is returned by runRaw when the `task` binary exits non-zero.
// It carries the captured stderr (bounded to stderrTailBytes) so handlers
// can classify the failure - e.g. surface "Could not interpret the date 'x'."
// as a per-field 400 rather than a generic 500. Callers that don't care
// can rely on the Error() string which redacts stderr to the exit code only.
type TaskExitError struct {
	ExitCode int
	Stderr   string // bounded to stderrTailBytes; never log this verbatim
	Stdout   string // bounded to stderrTailBytes; carries TW's user-facing
	// summary line ("Deleted 0 tasks.", "Modified 1 task.") so callers can
	// classify a non-zero exit as a real error vs an idempotent no-op.
	Wrapped error
}

// noOpExitPattern matches Taskwarrior's "X 0 tasks." summary lines emitted
// when an operation found no tasks to act on (e.g. trying to delete an
// already-deleted task, or modify with no changes). For idempotent
// commands (delete, done, modify) this is functionally success - the
// desired end state already holds.
var noOpExitPattern = regexp.MustCompile(`\b(Deleted|Completed|Modified|Updated|Started|Stopped) 0 tasks?\.`)

// IsNoOpExit reports whether err is a TaskExitError whose stdout indicates
// the requested operation was a no-op because the task was already in the
// target state. Use this in handlers for idempotent actions to convert a
// "task is not deletable / Deleted 0 tasks" exit-1 into a quiet success
// rather than a 500 - the user's intent (gone / done) is already met.
func IsNoOpExit(err error) bool {
	var te *TaskExitError
	if !errors.As(err, &te) {
		return false
	}
	return noOpExitPattern.MatchString(te.Stdout)
}

// IsNotInitialised reports whether err is a TaskExitError caused by a missing
// ~/.taskrc - Taskwarrior exits 2 with this message when it has never been run.
func IsNotInitialised(err error) bool {
	var te *TaskExitError
	if !errors.As(err, &te) {
		return false
	}
	return strings.Contains(te.Stderr, "Cannot proceed without rc file")
}

func (e *TaskExitError) Error() string {
	if e.ExitCode >= 0 {
		return fmt.Sprintf("task command failed: exit status %d", e.ExitCode)
	}
	return fmt.Sprintf("task command failed: %v", e.Wrapped)
}

func (e *TaskExitError) Unwrap() error { return e.Wrapped }

// IsInvalidFilter reports whether err is a TaskExitError caused by a filter
// expression that Taskwarrior's export evaluator cannot parse. This is a
// safety net for filter shapes (e.g. -+tag) that SafeReadFilter did not
// catch at composition time.
func IsInvalidFilter(err error) bool {
	var te *TaskExitError
	if !errors.As(err, &te) {
		return false
	}
	return strings.Contains(te.Stderr, "The expression could not be evaluated")
}

// Project / tag discovery (auto-suggest sources). Cached for the process
// lifetime; restart to pick up newly-defined names.
//
// projectListPattern / tagListPattern mirror ProjectPattern / TagPattern in
// task.go but live here so the discovery path doesn't accidentally accept
// names the AddArgs validator would later reject. Anything failing the
// pattern is dropped silently as defence-in-depth against a hostile sqlite
// state smuggling parser tokens through the cache.

var projectListPattern = regexp.MustCompile(`^[a-zA-Z0-9._]+$`)
var tagListPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ReportNamePattern is the allowlist for Taskwarrior report names. Used
// both internally as the discovery filter and by the views layer to
// validate the {name} URL path param of /r/{name}. Same shape as
// ContextNamePattern and tagListPattern - letters, digits, dash,
// underscore.
var ReportNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// virtualTags is the set of Taskwarrior virtual tags - computed at query time
// from task state, never user-creatable. `task _tags` emits these alongside
// real user tags so we filter them out of suggest lists where they'd offer
// the user something they can't actually set.
//
// Source: https://taskwarrior.org/docs/virtual-tags/ (Taskwarrior 3.x).
var virtualTags = map[string]struct{}{
	"ACTIVE": {}, "ANNOTATED": {}, "BLOCKED": {}, "BLOCKING": {},
	"CHILD": {}, "COMPLETED": {}, "DELETED": {}, "DUE": {},
	"DUETODAY": {}, "INSTANCE": {}, "LATEST": {}, "MONTH": {},
	"ORPHAN": {}, "OVERDUE": {}, "PARENT": {}, "PENDING": {},
	"PRIORITY": {}, "PROJECT": {}, "QUARTER": {}, "READY": {},
	"SCHEDULED": {}, "TAGGED": {}, "TEMPLATE": {}, "TODAY": {},
	"TOMORROW": {}, "UDA": {}, "UNBLOCKED": {}, "UNTIL": {},
	"WAITING": {}, "WEEK": {}, "YEAR": {}, "YESTERDAY": {},
}

// ListReports shells `task _reports` and returns the sorted, deduplicated
// list of report names matching the same shape as Taskwarrior's report
// names. Used by the views layer to dynamically surface user-defined
// reports in the nav, replacing the previous hardcoded curatedReportSpecs
// map (curated specs still drive the four pinned tabs).
//
// Names are filtered through ReportNamePattern as defence in depth so a
// hostile taskrc cannot smuggle filter-fragment characters through report
// discovery.
func (c *Client) ListReports(ctx context.Context) ([]string, error) {
	args := c.argv("_reports")
	out, err := c.runRaw(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	return filterStringList(string(out), ReportNamePattern), nil
}

// ListProjects shells `task _projects` and returns the deduplicated, sorted
// list of project names matching projectListPattern. Empty result with nil
// error means the user has no projects yet.
//
// _projects is built into Taskwarrior and emits one project name per line on
// stdout. The argument is a constant so this goes via runRaw.
func (c *Client) ListProjects(ctx context.Context) ([]string, error) {
	args := c.argv("_projects")
	out, err := c.runRaw(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return filterStringList(string(out), projectListPattern), nil
}

// ListTags is the same shape as ListProjects but for `task _tags`. Virtual
// tags (ACTIVE, BLOCKED, OVERDUE, …) are stripped because they're computed
// from task state, not user-set, so suggesting them would mislead the user.
func (c *Client) ListTags(ctx context.Context) ([]string, error) {
	args := c.argv("_tags")
	out, err := c.runRaw(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	all := filterStringList(string(out), tagListPattern)
	out2 := all[:0]
	for _, t := range all {
		if _, isVirtual := virtualTags[t]; isVirtual {
			continue
		}
		out2 = append(out2, t)
	}
	return out2, nil
}

// filterStringList parses newline-separated output, drops blanks/duplicates
// and any entry failing pat, and returns a sorted slice. Returned slice is
// non-nil empty when nothing survives the filter so callers can range over it
// without nil-checking.
func filterStringList(raw string, pat *regexp.Regexp) []string {
	seen := map[string]bool{}
	out := []string{}
	for line := range strings.SplitSeq(raw, "\n") {
		v := strings.TrimSpace(line)
		if v == "" || seen[v] {
			continue
		}
		if !pat.MatchString(v) {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ProjectsCached returns the project list, lazily fetched and TTL-invalidated
// via the shared ttlCache. Newly-defined projects surface within cacheTTL of
// being added via the CLI without a server restart, and a transient `task`
// binary failure during the first call doesn't poison the cache to empty.
func (c *Client) ProjectsCached(ctx context.Context) []string {
	return c.projects.load(ctx, c.ListProjects)
}

// TagsCached is the tag-side counterpart to ProjectsCached.
func (c *Client) TagsCached(ctx context.Context) []string {
	return c.tags.load(ctx, c.ListTags)
}

// ReportsCached is the reports-side counterpart. Used by the views layer
// to surface user-defined reports in the nav alongside the curated
// defaults (Ready / Next / Agenda / Forecast).
func (c *Client) ReportsCached(ctx context.Context) []string {
	return c.reports.load(ctx, c.ListReports)
}

// Undo wraps `task undo`. The safetyArgs prefix already contains
// rc.confirmation=no so Taskwarrior won't interactively prompt; the call
// reverses the most recent change atomically. Repeated calls walk further
// back through the undo log.
func (c *Client) Undo(ctx context.Context) error {
	return c.Run(ctx, "undo")
}

// ListContexts shells `task context list` and parses the human-readable
// table. Output shape (Taskwarrior 3.x):
//
//	Name  Type   Definition   Active
//	---- ------ ------------ --------
//	work read   +work         yes
//	work write  +work         no
//	home read   +home
//	...
//	(empty when no contexts defined; the bare `task contexts` form returns
//	"No matches." with exit 1 in 3.x and is no longer used)
//
// `rc.defaultwidth:1000` defeats Taskwarrior's column-wrap heuristic so long
// OR-chained filters land on one line per row - the parser doesn't merge
// continuation lines and would otherwise truncate them.
//
// Multiple rows for the same name (one per type) are merged into a single
// Context entry; ReadFilter / WriteFilter capture the per-type filter and
// Active reflects whichever row was marked active (Taskwarrior tracks one
// active read context and one active write context independently, but the
// UI only surfaces read).
//
// Names failing ContextNamePattern are dropped silently as defence-in-depth.
func (c *Client) ListContexts(ctx context.Context) ([]Context, error) {
	args := c.argv("rc.defaultwidth:1000", "context", "list")
	out, err := c.runRaw(ctx, args) // rc.defaultwidth is a trusted constant; runRaw bypass of guardArgs is intentional
	if err != nil {
		return nil, fmt.Errorf("list contexts: %w", err)
	}
	return parseContextsTable(string(out)), nil
}

// parseContextsTable extracts Context entries from the raw stdout of
// `task contexts`. Lifted into a free function so it can be unit-tested
// without shelling.
func parseContextsTable(raw string) []Context {
	byName := map[string]*Context{}
	order := []string{}
	var lastName string // last seen valid context name, for blank-name continuation rows

	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip header / separator / footer rows. The header has the literal
		// "Name" column title; the separator is a sequence of dashes; the
		// footer matches "N contexts" or "No contexts defined.".
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "name") && strings.Contains(lower, "type") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			continue
		}
		if strings.HasSuffix(lower, "contexts defined.") {
			continue
		}
		if strings.Contains(lower, "context") && (strings.HasSuffix(lower, ")") || strings.HasSuffix(lower, "active") || strings.HasSuffix(lower, "active.")) {
			// "3 contexts (2 of which are active)" footer.
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Taskwarrior 3.x prints one row per type for each context. When a
		// context has both a read and write filter, the second row has a blank
		// name column, so after TrimSpace fields[0] is the type ("read"/"write")
		// and fields[1] is the first token of the filter. Detect this by
		// checking whether fields[1] is a type keyword; if not, treat fields[0]
		// as the type and carry lastName forward as the name.
		var name, typ string
		var filterStart int
		f0 := strings.ToLower(fields[0])
		f1 := strings.ToLower(fields[1])
		if (f1 == "read" || f1 == "write") && ContextNamePattern.MatchString(fields[0]) {
			// Normal row: name type filter...
			name = fields[0]
			lastName = name
			typ = f1
			filterStart = 2
		} else if (f0 == "read" || f0 == "write") && lastName != "" {
			// Continuation row: name column was blank; type is fields[0].
			name = lastName
			typ = f0
			filterStart = 1
		} else {
			continue
		}

		// Active flag is the trailing field if it looks like yes/no; the
		// filter text is everything between the type and the active flag.
		active := false
		filterEnd := len(fields)
		if last := strings.ToLower(fields[len(fields)-1]); last == "yes" || last == "no" {
			active = last == "yes"
			filterEnd = len(fields) - 1
		}
		filter := strings.Join(fields[filterStart:filterEnd], " ")

		entry, ok := byName[name]
		if !ok {
			entry = &Context{Name: name}
			byName[name] = entry
			order = append(order, name)
		}
		switch typ {
		case "read":
			entry.ReadFilter = filter
			if active {
				entry.Active = true
			}
		case "write":
			entry.WriteFilter = filter
		}
	}

	out := make([]Context, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ContextsCached returns the contexts list, lazily fetched and
// TTL-invalidated via the shared ttlCache.
//
// The Active flag captured here is a snapshot of the moment of cache fill
// and MUST NOT be relied on - use ActiveContext for the live value.
func (c *Client) ContextsCached(ctx context.Context) []Context {
	return c.contexts.load(ctx, c.ListContexts)
}

// ActiveContext shells `task _get rc.context` and returns the active
// context name, or empty string when no context is active. Errors are
// folded into "no active context" to keep the UI rendering even if the
// binary is briefly flaky - the dropdown will just show "(none)" as
// active in that case.
//
// `_context` (without `_get`) is the discovery-list helper in TW 3.x and
// emits ALL defined context names regardless of which is active, so it's
// the wrong tool here. `_get rc.context` reads the single rc setting that
// tracks the live active state.
//
// NOT cached: the user can change the active context out-of-band via the
// CLI or via the POST /context route, and a stale value here would mislead
// the pill / title / empty-state. The call is cheap (~10-30 ms) and runs
// once per page render.
func (c *Client) ActiveContext(ctx context.Context) string {
	args := c.argv("_get", "rc.context")
	out, err := c.runRaw(ctx, args)
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return ""
	}
	if !ContextNamePattern.MatchString(name) {
		return ""
	}
	return name
}

// Dependents returns every pending task whose `depends` list includes uuid -
// i.e. the set of tasks blocked on this one. Implemented as
// `task export depends.has:<uuid> status:pending`. The argument is validated
// against IDPattern as defence-in-depth so callers cannot smuggle filter
// fragments through the path-value plumbing.
//
// Not cached: the result is per-uuid and the dataset is small (one Export per
// expand panel), so a sync.Map of *filterEntry-style memoisation would buy
// nothing meaningful and complicate invalidation when a task is modified.
func (c *Client) Dependents(ctx context.Context, uuid string) ([]Task, error) {
	if !IDPattern.MatchString(uuid) {
		return nil, fmt.Errorf("%w: uuid %q", ErrInvalid, uuid)
	}
	return c.Export(ctx, "depends.has:"+uuid, "status:pending")
}

// SetContext activates the named context, or clears it when name is empty.
// Validation of the name is the caller's responsibility; we re-check here as
// defence-in-depth so a future refactor can't bypass the handler-side check.
//
// Implementation:
//   - empty name -> `task context none`
//   - non-empty -> `task context <name>`
//
// Both forms ride safetyArgs so Taskwarrior doesn't prompt interactively.
func (c *Client) SetContext(ctx context.Context, name string) error {
	if name == "" {
		return c.Run(ctx, "context", "none")
	}
	if !ContextNamePattern.MatchString(name) {
		return fmt.Errorf("%w: context name %q", ErrInvalid, name)
	}
	return c.Run(ctx, "context", name)
}
