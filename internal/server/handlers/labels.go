package handlers

import (
	"log/slog"
	"net/http"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
	"github.com/furan917/taskwarrior-web-portal/internal/views"
)

// Labels handles tag and project rename operations.
type Labels struct {
	TW     *tw.Client
	Logger *slog.Logger
}

// RenameTagForm handles GET /forms/tag/{name}/rename - renders the rename modal for a tag.
func (l *Labels) RenameTagForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.TagPattern.MatchString(name) {
		http.Error(w, "invalid tag name", http.StatusBadRequest)
		return
	}
	csrf := csrfToken(r)
	renderHTML(w, r, "RenameForm", views.RenameFormModal(csrf, name, "tag"), l.Logger)
}

// RenameProjectForm handles GET /forms/project/{name}/rename - renders the rename modal for a project.
func (l *Labels) RenameProjectForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.ProjectPattern.MatchString(name) {
		http.Error(w, "invalid project name", http.StatusBadRequest)
		return
	}
	csrf := csrfToken(r)
	renderHTML(w, r, "RenameForm", views.RenameFormModal(csrf, name, "project"), l.Logger)
}

// RenameTag handles POST /tags/{name}/rename - executes a tag rename across all tasks.
func (l *Labels) RenameTag(w http.ResponseWriter, r *http.Request) {
	oldTag := r.PathValue("name")
	if !tw.TagPattern.MatchString(oldTag) {
		http.Error(w, "invalid tag name", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	newTag := r.FormValue("new_name")
	if !tw.TagPattern.MatchString(newTag) {
		writeContextFormError(w, http.StatusBadRequest, "New name must contain only letters, digits, dashes, or underscores.")
		return
	}
	if oldTag == newTag {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := l.TW.RunNoContext(r.Context(), "+"+oldTag, "modify", "-"+oldTag, "+"+newTag); err != nil && !tw.IsNoOpExit(err) {
		l.Logger.Error("tag rename failed", "old", oldTag, "new", newTag, "err", err)
		writeContextFormError(w, http.StatusInternalServerError, "Rename failed.")
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

// RenameProject handles POST /projects/{name}/rename - executes a project rename across all tasks.
func (l *Labels) RenameProject(w http.ResponseWriter, r *http.Request) {
	oldName := r.PathValue("name")
	if !tw.ProjectPattern.MatchString(oldName) {
		http.Error(w, "invalid project name", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	newName := r.FormValue("new_name")
	if !tw.ProjectPattern.MatchString(newName) {
		writeContextFormError(w, http.StatusBadRequest, "New name must contain only letters, digits, dots, or underscores.")
		return
	}
	if oldName == newName {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := l.TW.RunNoContext(r.Context(), "project:"+oldName, "modify", "project:"+newName); err != nil && !tw.IsNoOpExit(err) {
		l.Logger.Error("project rename failed", "old", oldName, "new", newName, "err", err)
		writeContextFormError(w, http.StatusInternalServerError, "Rename failed.")
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}
