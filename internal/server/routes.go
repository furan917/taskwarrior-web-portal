package server

import (
	"net/http"

	"github.com/furan917/taskwarrior-web-portal/internal/server/handlers"
)

// staticHandler wraps the embedded FS server. Cache-Control is no-cache so the
// browser always revalidates - assets aren't content-hashed yet, and one extra
// localhost roundtrip per asset is cheaper than stale-JS-after-redeploy bugs.
func staticHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

// registerRoutes wires every URL contract once. The returned http.Handler
// has CSRF middleware applied to everything except /healthz and /static/.
func registerRoutes(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc("GET /healthz", handlers.Healthz)

	if cfg.Static != nil {
		fileServer := http.FileServerFS(cfg.Static)
		mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler(fileServer)))
	}

	v := &handlers.Views{TW: cfg.TW, Logger: cfg.Logger}
	t := &handlers.Tasks{TW: cfg.TW, Logger: cfg.Logger}
	f := &handlers.Forms{TW: cfg.TW, Logger: cfg.Logger}
	c := &handlers.Contexts{TW: cfg.TW, Logger: cfg.Logger}
	s := &handlers.Sync{TW: cfg.TW, Logger: cfg.Logger}
	bw := handlers.NewBugwarrior(cfg.Logger)
	u := &handlers.UDAs{TW: cfg.TW, Logger: cfg.Logger}
	lb := &handlers.Labels{TW: cfg.TW, Logger: cfg.Logger}
	to := &handlers.TagOps{TW: cfg.TW, Logger: cfg.Logger}

	// All app routes go through CSRF middleware.
	app := http.NewServeMux()
	app.HandleFunc("GET /", v.Home)
	app.HandleFunc("GET /next", v.Report("next"))
	app.HandleFunc("GET /ready", v.Report("ready"))
	app.HandleFunc("GET /agenda", v.Report("agenda"))
	app.HandleFunc("GET /forecast", v.Report("forecast"))
	// /r/{name} surfaces any user-defined report from the taskrc that isn't
	// one of the curated four. The handler validates the name against
	// tw.ReportNamePattern and 404s for anything not in ReportsCached.
	app.HandleFunc("GET /r/{name}", v.ReportByName)
	app.HandleFunc("GET /project/{name}", v.Project)
	app.HandleFunc("GET /tag/{name}", v.Tag)
	app.HandleFunc("GET /browse", v.Labels)
	app.HandleFunc("GET /kanban", v.Kanban)
	app.HandleFunc("GET /calendar", v.Calendar)
	app.HandleFunc("GET /partials/list", v.Partial)
	app.HandleFunc("GET /partials/active-indicator", v.ActiveIndicator)

	app.HandleFunc("GET /forms/add", f.Add)
	app.HandleFunc("GET /forms/edit/{id}", f.Edit)
	app.HandleFunc("GET /forms/sessions/{id}", f.Sessions)

	app.HandleFunc("POST /tasks", t.Create)
	app.HandleFunc("PUT /tasks/{id}", t.Modify)
	app.HandleFunc("POST /tasks/{id}/done", t.Done)
	app.HandleFunc("POST /tasks/{id}/start", t.Start)
	app.HandleFunc("POST /tasks/{id}/stop", t.Stop)
	app.HandleFunc("POST /tasks/{id}/move", t.Move)
	app.HandleFunc("POST /tasks/{id}/duplicate", t.Duplicate)
	app.HandleFunc("PUT /tasks/{id}/intervals", t.PutIntervals)
	app.HandleFunc("DELETE /tasks/{id}", t.Delete)
	app.HandleFunc("POST /tasks/bulk/done", t.BulkDone)
	app.HandleFunc("POST /tasks/bulk/delete", t.BulkDelete)
	app.HandleFunc("POST /tasks/{id}/annotate", t.Annotate)
	app.HandleFunc("POST /tasks/{id}/denotate", t.Denotate)
	app.HandleFunc("GET /done", v.Done)
	app.HandleFunc("GET /stats", v.Stats)
	app.HandleFunc("GET /config", v.ConfigInfo(s, bw))
	app.HandleFunc("POST /sync", s.Run)
	app.HandleFunc("POST /bugwarrior-pull", bw.Pull)
	app.HandleFunc("GET /timesheet", v.Timesheet)
	app.HandleFunc("POST /timesheet/enable-tracking", v.EnableTimeTracking)
	app.HandleFunc("GET /reports/time", v.TimeReport)
	app.HandleFunc("POST /undo", t.Undo)
	app.HandleFunc("POST /context", c.Set)
	app.HandleFunc("GET /contexts", c.ManageContexts)
	app.HandleFunc("GET /forms/context/new", c.CreateContextForm)
	app.HandleFunc("GET /forms/context/{name}", c.EditContextForm)
	app.HandleFunc("POST /contexts", c.CreateContext)
	app.HandleFunc("PUT /contexts/{name}", c.UpdateContext)
	app.HandleFunc("DELETE /contexts/{name}", c.DeleteContext)

	app.HandleFunc("GET /udas", u.ManageUDAs)
	app.HandleFunc("GET /forms/uda/new", u.CreateUDAForm)
	app.HandleFunc("GET /forms/uda/{name}", u.EditUDAForm)
	app.HandleFunc("POST /udas", u.CreateUDA)
	app.HandleFunc("PUT /udas/{name}", u.UpdateUDA)
	app.HandleFunc("DELETE /udas/{name}", u.DeleteUDA)

	app.HandleFunc("GET /forms/tag/{name}/rename", lb.RenameTagForm)
	app.HandleFunc("GET /forms/project/{name}/rename", lb.RenameProjectForm)
	app.HandleFunc("POST /tags/{name}/rename", lb.RenameTag)
	app.HandleFunc("POST /projects/{name}/rename", lb.RenameProject)

	app.HandleFunc("GET /forms/tag/{name}/merge", to.MergeTagForm)
	app.HandleFunc("POST /tags/{name}/merge", to.MergeTag)

	mux.Handle("/", withCSRF(cfg.Logger, app))
}
