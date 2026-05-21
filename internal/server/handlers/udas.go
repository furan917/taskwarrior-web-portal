package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
	"github.com/furan917/taskwarrior-web-portal/internal/views"
)

type UDAs struct {
	TW     *tw.Client
	Logger *slog.Logger
}

func (u *UDAs) ManageUDAs(w http.ResponseWriter, r *http.Request) {
	udas := u.TW.UDAsCached(r.Context())
	page := buildPage(u.TW, r, "UDAs", "udas", false)
	renderHTML(w, r, "UDAs", views.ManageUDAsPage(page, udas), u.Logger)
}

func (u *UDAs) CreateUDAForm(w http.ResponseWriter, r *http.Request) {
	csrf := csrfToken(r)
	renderHTML(w, r, "UDAForm", views.UDAFormModal(csrf, "", "", "", "", true), u.Logger)
}

func (u *UDAs) EditUDAForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.UDANamePattern.MatchString(name) {
		http.Error(w, "invalid UDA name", http.StatusBadRequest)
		return
	}
	var found tw.UDA
	for _, uda := range u.TW.UDAsCached(r.Context()) {
		if uda.Name == name {
			found = uda
			break
		}
	}
	csrf := csrfToken(r)
	renderHTML(w, r, "UDAForm", views.UDAFormModal(csrf, found.Name, found.Type, found.Label, strings.Join(found.Values, ","), false), u.Logger)
}

func (u *UDAs) CreateUDA(w http.ResponseWriter, r *http.Request) {
	name, udaType, label, values, ok := parseUDAForm(w, r)
	if !ok {
		return
	}
	if err := u.TW.CreateUDA(r.Context(), name, udaType, label, values); err != nil {
		u.udaFormError(w, "create", err)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

func (u *UDAs) UpdateUDA(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.UDANamePattern.MatchString(name) {
		http.Error(w, "invalid UDA name", http.StatusBadRequest)
		return
	}
	_, udaType, label, values, ok := parseUDAForm(w, r)
	if !ok {
		return
	}
	if err := u.TW.UpdateUDA(r.Context(), name, udaType, label, values); err != nil {
		u.udaFormError(w, "update", err)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

func (u *UDAs) DeleteUDA(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !tw.UDANamePattern.MatchString(name) {
		http.Error(w, "invalid UDA name", http.StatusBadRequest)
		return
	}
	if err := u.TW.DeleteUDA(r.Context(), name); err != nil {
		u.Logger.Error("delete UDA failed", "name", name, "err", err)
		http.Error(w, "delete UDA failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

func parseUDAForm(w http.ResponseWriter, r *http.Request) (name, udaType, label, values string, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeContextFormError(w, http.StatusBadRequest, "bad form data")
		return "", "", "", "", false
	}
	name = strings.TrimSpace(r.FormValue("name"))
	udaType = strings.TrimSpace(r.FormValue("type"))
	label = strings.TrimSpace(r.FormValue("label"))
	values = strings.TrimSpace(r.FormValue("values"))

	if !tw.UDANamePattern.MatchString(name) {
		writeContextFormError(w, http.StatusBadRequest, "invalid UDA name: must start with a letter, then letters/digits/underscores only")
		return "", "", "", "", false
	}
	validTypes := map[string]struct{}{"string": {}, "numeric": {}, "date": {}, "duration": {}}
	if _, valid := validTypes[udaType]; !valid {
		writeContextFormError(w, http.StatusBadRequest, "type must be one of: string, numeric, date, duration")
		return "", "", "", "", false
	}
	if label == "" {
		writeContextFormError(w, http.StatusBadRequest, "label is required")
		return "", "", "", "", false
	}
	if values != "" {
		for _, p := range strings.Split(values, ",") {
			if strings.TrimSpace(p) == "" {
				writeContextFormError(w, http.StatusBadRequest, "values list contains an empty entry")
				return "", "", "", "", false
			}
		}
	}
	return name, udaType, label, values, true
}

func (u *UDAs) udaFormError(w http.ResponseWriter, op string, err error) {
	u.Logger.Error(op+" UDA failed", "err", err)
	if errors.Is(err, tw.ErrInvalid) {
		writeContextFormError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeContextFormError(w, http.StatusInternalServerError, op+" UDA failed")
}
