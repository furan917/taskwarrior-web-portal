package handlers

import (
	"log/slog"
	"net/http"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
	"github.com/furan917/taskwarrior-web-portal/internal/views"
)

// TagOps handles tag merge operations.
type TagOps struct {
	TW     *tw.Client
	Logger *slog.Logger
}

// MergeTagForm handles GET /forms/tag/{name}/merge - renders the merge modal pre-filled with the from tag.
func (t *TagOps) MergeTagForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.TagPattern.MatchString(name) {
		http.Error(w, "invalid tag name", http.StatusBadRequest)
		return
	}
	csrf := csrfToken(r)
	allTags := t.TW.TagsCached(r.Context())
	renderHTML(w, r, "MergeTagForm", views.MergeTagFormModal(csrf, name, allTags), t.Logger)
}

// MergeTag handles POST /tags/{name}/merge - merges the from tag into the to tag across all tasks.
func (t *TagOps) MergeTag(w http.ResponseWriter, r *http.Request) {
	fromTag := r.PathValue("name")
	if !tw.TagPattern.MatchString(fromTag) {
		http.Error(w, "invalid tag name", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	toTag := r.FormValue("to")
	if !tw.TagPattern.MatchString(toTag) {
		writeContextFormError(w, http.StatusBadRequest, "Target tag must contain only letters, digits, dashes, or underscores.")
		return
	}
	if fromTag == toTag {
		writeContextFormError(w, http.StatusBadRequest, "cannot merge a tag into itself")
		return
	}
	if err := t.TW.RunNoContext(r.Context(), "+"+fromTag, "modify", "-"+fromTag, "+"+toTag); err != nil && !tw.IsNoOpExit(err) {
		t.Logger.Error("tag merge failed", "from", fromTag, "to", toTag, "err", err)
		writeContextFormError(w, http.StatusInternalServerError, "Merge failed.")
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}
