package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
)

func newLabels() *Labels {
	return &Labels{TW: tw.NewClient(), Logger: discardLogger()}
}

// --- RenameTag ---

func TestLabels_RenameTag_Success(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})
	l := newLabels()

	form := url.Values{"new_name": {"newtag"}}
	req := httptest.NewRequest(http.MethodPost, "/tags/oldtag/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "oldtag")
	rr := httptest.NewRecorder()
	l.RenameTag(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Errorf("HX-Refresh: got %q want true", got)
	}
}

func TestLabels_RenameTag_SameNameIsNoOp(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	l := newLabels()

	form := url.Values{"new_name": {"mytag"}}
	req := httptest.NewRequest(http.MethodPost, "/tags/mytag/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "mytag")
	rr := httptest.NewRecorder()
	l.RenameTag(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("same-name rename: got %d want 204", rr.Code)
	}
	if got := rr.Header().Get("HX-Refresh"); got != "" {
		t.Errorf("same-name rename should not set HX-Refresh, got %q", got)
	}
}

func TestLabels_RenameTag_InvalidOldName(t *testing.T) {
	l := newLabels()

	for _, bad := range []string{"bad name", "a;b", "../", "rc.foo"} {
		form := url.Values{"new_name": {"good"}}
		req := httptest.NewRequest(http.MethodPost, "/tags/x/rename", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("name", bad)
		rr := httptest.NewRecorder()
		l.RenameTag(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("old name %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestLabels_RenameTag_InvalidNewName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	l := newLabels()

	for _, bad := range []string{"bad name", "a;b", "../", ""} {
		form := url.Values{"new_name": {bad}}
		req := httptest.NewRequest(http.MethodPost, "/tags/oldtag/rename", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("name", "oldtag")
		rr := httptest.NewRecorder()
		l.RenameTag(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("new name %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestLabels_RenameTag_TaskBinaryFailure(t *testing.T) {
	installFailingTask(t)
	l := newLabels()

	form := url.Values{"new_name": {"newtag"}}
	req := httptest.NewRequest(http.MethodPost, "/tags/oldtag/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "oldtag")
	rr := httptest.NewRecorder()
	l.RenameTag(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("binary failure: got %d want 500", rr.Code)
	}
}

// --- RenameProject ---

func TestLabels_RenameProject_Success(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})
	l := newLabels()

	form := url.Values{"new_name": {"newproject"}}
	req := httptest.NewRequest(http.MethodPost, "/projects/oldproject/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "oldproject")
	rr := httptest.NewRecorder()
	l.RenameProject(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Errorf("HX-Refresh: got %q want true", got)
	}
}

func TestLabels_RenameProject_SameNameIsNoOp(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	l := newLabels()

	form := url.Values{"new_name": {"myproject"}}
	req := httptest.NewRequest(http.MethodPost, "/projects/myproject/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "myproject")
	rr := httptest.NewRecorder()
	l.RenameProject(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("same-name rename: got %d want 204", rr.Code)
	}
}

func TestLabels_RenameProject_InvalidOldName(t *testing.T) {
	l := newLabels()

	for _, bad := range []string{"bad name", "a;b", "../"} {
		form := url.Values{"new_name": {"good"}}
		req := httptest.NewRequest(http.MethodPost, "/projects/x/rename", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("name", bad)
		rr := httptest.NewRecorder()
		l.RenameProject(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("old name %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestLabels_RenameProject_InvalidNewName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	l := newLabels()

	for _, bad := range []string{"bad name", "a;b", "../", ""} {
		form := url.Values{"new_name": {bad}}
		req := httptest.NewRequest(http.MethodPost, "/projects/myproject/rename", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("name", "myproject")
		rr := httptest.NewRecorder()
		l.RenameProject(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("new name %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestLabels_RenameProject_TaskBinaryFailure(t *testing.T) {
	installFailingTask(t)
	l := newLabels()

	form := url.Values{"new_name": {"newproject"}}
	req := httptest.NewRequest(http.MethodPost, "/projects/oldproject/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "oldproject")
	rr := httptest.NewRecorder()
	l.RenameProject(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("binary failure: got %d want 500", rr.Code)
	}
}

// --- Form rendering ---

func TestLabels_RenameTagForm_ValidName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	l := newLabels()

	req := httptest.NewRequest(http.MethodGet, "/forms/tag/mytag/rename", nil)
	req.SetPathValue("name", "mytag")
	rr := httptest.NewRecorder()
	l.RenameTagForm(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "mytag") {
		t.Error("response body should contain current tag name")
	}
}

func TestLabels_RenameTagForm_InvalidName(t *testing.T) {
	l := newLabels()

	req := httptest.NewRequest(http.MethodGet, "/forms/tag/x/rename", nil)
	req.SetPathValue("name", "bad name")
	rr := httptest.NewRecorder()
	l.RenameTagForm(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

func TestLabels_RenameProjectForm_ValidName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	l := newLabels()

	req := httptest.NewRequest(http.MethodGet, "/forms/project/myproject/rename", nil)
	req.SetPathValue("name", "myproject")
	rr := httptest.NewRecorder()
	l.RenameProjectForm(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "myproject") {
		t.Error("response body should contain current project name")
	}
}

func TestLabels_RenameProjectForm_InvalidName(t *testing.T) {
	l := newLabels()

	req := httptest.NewRequest(http.MethodGet, "/forms/project/x/rename", nil)
	req.SetPathValue("name", "bad name")
	rr := httptest.NewRecorder()
	l.RenameProjectForm(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}
