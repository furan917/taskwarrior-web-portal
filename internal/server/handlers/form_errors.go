package handlers

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
)

// writeFormError writes a validation error to the response as a small HTML
// fragment plus a `data-field-error` attribute naming the offending field.
// The modal forms carry `hx-target-error="#task-form-errors"`; HTMX picks up
// the 400 + HTML body and swaps it into that container. JS in app.js reads
// the data-field-error attribute and red-borders the matching input so the
// user has both an inline message AND a visual cue at the field itself.
//
// We hardcode Content-Type so HTMX renders the body as HTML rather than
// dropping it as plain text. Status 400 keeps the existing client-side
// "modal stays open on error" behaviour (success returns 204 + HX-Trigger
// refresh, which the afterRequest handler in app.js uses to close).
func writeFormError(w http.ResponseWriter, err error) {
	field, message := classifyValidationError(err)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<div class="rounded border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-200" data-field-error=%q role="alert"><strong class="font-semibold">Couldn&apos;t save:</strong> %s</div>`,
		field, html.EscapeString(message))
}

// writeContextFormError writes an HTML error fragment for context form modals.
// Produces plain text inside the already-styled #context-form-errors container.
// Kept separate from writeFormError so the task form error path is untouched.
func writeContextFormError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<span role="alert">%s</span>`, html.EscapeString(message))
}

// classifyValidationError takes an error from tw.AddArgs / tw.ModifyArgs /
// readUDAArgs and returns the form-field name plus a user-readable message
// the modal can display. Falls back to the raw error text when no known
// pattern matches (defence in depth - an unrecognised error still renders
// something rather than a silent 400).
//
// Field names mirror the form input names so the JS highlight loop can do
// `input[name="<field>"]` lookups.
//
// Uses errors.As against the typed errors (*tw.ValidationError /
// *udaInputError) so renaming an error message can't silently break field
// classification. The typed-error refactor replaced the previous
// strings.HasPrefix matching against fmt.Errorf-formatted text.
func classifyValidationError(err error) (field, message string) {
	if err == nil {
		return "", ""
	}
	var verr *tw.ValidationError
	if errors.As(err, &verr) {
		return classifyTwValidationError(verr)
	}
	var uerrPtr *udaInputError
	if errors.As(err, &uerrPtr) {
		return "uda_" + uerrPtr.name, formatUDAError(uerrPtr)
	}
	return "", err.Error()
}

func classifyTwValidationError(verr *tw.ValidationError) (field, message string) {
	switch verr.Field {
	case "description":
		return "description", "Description is required."
	case "project":
		return "project", "Project must contain only letters, digits, dots, or underscores. Got: " + verr.Value
	case "tags":
		return "tags", "Tags must contain only letters, digits, dashes, or underscores. Got: " + verr.Value
	case "due":
		return "due", explainDateError("Due", verr.Value)
	case "wait":
		return "wait", explainDateError("Wait", verr.Value)
	case "scheduled":
		return "scheduled", explainDateError("Scheduled for start", verr.Value)
	case "depends":
		return "depends", "Dependency must be a valid task UUID."
	}
	return verr.Field, verr.Error()
}

// explainDateError builds a friendly date-format hint that matches the
// help-tooltip copy: it lists the same three ways to write a valid date
// so the user sees the rule both inline (in the error) and on hover (in
// the tooltip).
func explainDateError(label, value string) string {
	if value == "" {
		return label + ": invalid date. Try +2d, tomorrow, eod, or YYYY-MM-DD."
	}
	return fmt.Sprintf("%s: %q isn't a valid date. Try +2d, tomorrow, eod, or YYYY-MM-DD.", label, value)
}

// uerr type-asserts an error chain for the per-UDA validation error kind
// (defined in tasks.go). Lifted into its own helper so classify... can
// inline the type switch without importing the type's name into the
// outer scope.
func uerr(err error) *udaInputError {
	var e *udaInputError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func formatUDAError(e *udaInputError) string {
	switch e.kind {
	case "numeric":
		return e.name + ": must be a number."
	case "date":
		return e.name + ": isn't a valid date. Try +2d, tomorrow, eod, or YYYY-MM-DD."
	case "enum":
		return e.name + ": value not allowed."
	}
	return e.name + ": invalid value."
}

// writeIfTaskParseError surfaces Taskwarrior runtime errors that look like
// date-parse failures as a 400 with a per-field error fragment, instead of
// a generic 500. Pre-validation in tw.AddArgs catches obvious cases (spaces,
// pure digits) but not every form Taskwarrior itself rejects (unknown
// keywords like "potato", malformed offsets like "+xyz"); when those slip
// through, the binary errors with stderr like "The date '...' is not a
// valid date." or "Could not interpret the date 'x'." - we read the
// captured stderr from tw.TaskExitError and fingerprint it.
//
// Stderr is held only on the error type (never logged) and discarded after
// classification, mirroring tw.runRaw's contract.
//
// Returns true when it wrote a 400 (caller should bail). False means the
// error didn't look like a parse issue, so the caller should fall through
// to its generic 500 path.
func writeIfTaskParseError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var exitErr *tw.TaskExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	stderr := strings.ToLower(exitErr.Stderr)
	if stderr == "" {
		return false
	}
	if strings.Contains(stderr, "recurring") && strings.Contains(stderr, "due") {
		writeTaskErrorFragment(w, "due", "A recurring task also needs a Due date. Set Due, or clear Recur.")
		return true
	}
	dateLike := strings.Contains(stderr, "date") &&
		(strings.Contains(stderr, "interpret") ||
			strings.Contains(stderr, "not a valid") ||
			strings.Contains(stderr, "not in a recognised") ||
			strings.Contains(stderr, "not in a recognized") ||
			strings.Contains(stderr, "format"))
	if !dateLike {
		return false
	}
	writeTaskErrorFragment(w, "", "Taskwarrior rejected one of the dates. Try +2d, tomorrow, eod, or YYYY-MM-DD.")
	return true
}

// field may be empty; the JS ignores an empty data-field-error.
func writeTaskErrorFragment(w http.ResponseWriter, field, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<div class="rounded border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-200" data-field-error=%q role="alert"><strong class="font-semibold">Couldn&apos;t save:</strong> %s</div>`,
		field, html.EscapeString(message))
}
