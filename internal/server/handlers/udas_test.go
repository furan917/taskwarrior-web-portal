package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
)

// anyInvocationContains returns true if at least one recorded argv file in
// logDir contains all of the given substrings. Used for operations that make
// multiple task invocations (e.g. CreateUDA writes type, label, values
// separately), where readArgs only returns the last call.
func anyInvocationContains(t *testing.T, logDir string, parts ...string) bool {
	t.Helper()
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(logDir, e.Name()))
		joined := strings.Join(strings.Fields(string(data)), " ")
		found := true
		for _, p := range parts {
			if !strings.Contains(joined, p) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

func newUDAs() *UDAs {
	return &UDAs{TW: tw.NewClient(), Logger: discardLogger()}
}

// --- CreateUDA ---

func TestUDAs_CreateUDA_HappyPath(t *testing.T) {
	logDir := installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})
	u := newUDAs()

	form := url.Values{"name": {"effort"}, "type": {"numeric"}, "label": {"Effort"}, "values": {""}}
	req := httptest.NewRequest(http.MethodPost, "/udas", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	u.CreateUDA(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Errorf("HX-Refresh: got %q want true", got)
	}
	if !anyInvocationContains(t, logDir, "uda.effort.type", "numeric") {
		t.Error("no invocation recorded with uda.effort.type numeric")
	}
}

func TestUDAs_CreateUDA_WithValues(t *testing.T) {
	logDir := installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})
	u := newUDAs()

	form := url.Values{"name": {"priority2"}, "type": {"string"}, "label": {"Priority"}, "values": {"high,medium,low"}}
	req := httptest.NewRequest(http.MethodPost, "/udas", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	u.CreateUDA(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	args := readArgs(t, logDir)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "uda.priority2.values") {
		t.Errorf("argv missing uda.priority2.values: %v", args)
	}
}

func TestUDAs_CreateUDA_RejectsBadName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	for _, bad := range []string{"1starts", "bad name", "rc.override", "+tag", "a;b", ""} {
		form := url.Values{"name": {bad}, "type": {"string"}, "label": {"Label"}}
		req := httptest.NewRequest(http.MethodPost, "/udas", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		u.CreateUDA(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("name %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestUDAs_CreateUDA_RejectsBadType(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	for _, bad := range []string{"text", "integer", "", "String"} {
		form := url.Values{"name": {"myuda"}, "type": {bad}, "label": {"Label"}}
		req := httptest.NewRequest(http.MethodPost, "/udas", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		u.CreateUDA(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("type %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestUDAs_CreateUDA_RejectsEmptyLabel(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	form := url.Values{"name": {"myuda"}, "type": {"string"}, "label": {""}}
	req := httptest.NewRequest(http.MethodPost, "/udas", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	u.CreateUDA(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty label: got %d want 400", rr.Code)
	}
}

func TestUDAs_CreateUDA_RejectsEmptyValuesEntry(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	form := url.Values{"name": {"myuda"}, "type": {"string"}, "label": {"Label"}, "values": {"high,,low"}}
	req := httptest.NewRequest(http.MethodPost, "/udas", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	u.CreateUDA(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty value entry: got %d want 400", rr.Code)
	}
}

// --- UpdateUDA ---

func TestUDAs_UpdateUDA_HappyPath(t *testing.T) {
	logDir := installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})
	u := newUDAs()

	form := url.Values{"name": {"effort"}, "type": {"numeric"}, "label": {"Effort Points"}, "values": {""}}
	req := httptest.NewRequest(http.MethodPut, "/udas/effort", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "effort")
	rr := httptest.NewRecorder()
	u.UpdateUDA(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Errorf("HX-Refresh: got %q want true", got)
	}
	if !anyInvocationContains(t, logDir, "uda.effort.label", "Effort Points") {
		t.Error("no invocation recorded with uda.effort.label Effort Points")
	}
}

func TestUDAs_UpdateUDA_RejectsBadPathName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	form := url.Values{"name": {"effort"}, "type": {"numeric"}, "label": {"Effort"}}
	req := httptest.NewRequest(http.MethodPut, "/udas/bad+name", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "bad+name")
	rr := httptest.NewRecorder()
	u.UpdateUDA(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad path name: got %d want 400", rr.Code)
	}
}

func TestUDAs_UpdateUDA_RejectsBadType(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	form := url.Values{"name": {"effort"}, "type": {"invalid"}, "label": {"Effort"}}
	req := httptest.NewRequest(http.MethodPut, "/udas/effort", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "effort")
	rr := httptest.NewRecorder()
	u.UpdateUDA(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad type: got %d want 400", rr.Code)
	}
}

// --- DeleteUDA ---

func TestUDAs_DeleteUDA_HappyPath(t *testing.T) {
	logDir := installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})
	u := newUDAs()

	req := httptest.NewRequest(http.MethodDelete, "/udas/effort", nil)
	req.SetPathValue("name", "effort")
	rr := httptest.NewRecorder()
	u.DeleteUDA(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Errorf("HX-Refresh: got %q want true", got)
	}
	// DeleteUDA clears each key by setting it to empty via `config uda.<name>.<key> ""`.
	if !anyInvocationContains(t, logDir, "config", "uda.effort.type") {
		t.Error("no config invocation recorded for uda.effort.type")
	}
	if !anyInvocationContains(t, logDir, "config", "uda.effort.label") {
		t.Error("no config invocation recorded for uda.effort.label")
	}
}

func TestUDAs_DeleteUDA_RejectsBadName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	req := httptest.NewRequest(http.MethodDelete, "/udas/bad+name", nil)
	req.SetPathValue("name", "bad+name")
	rr := httptest.NewRecorder()
	u.DeleteUDA(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad name: got %d want 400", rr.Code)
	}
}

func TestUDAs_DeleteUDA_500WhenTaskFails(t *testing.T) {
	installFailingTask(t)
	u := newUDAs()

	req := httptest.NewRequest(http.MethodDelete, "/udas/effort", nil)
	req.SetPathValue("name", "effort")
	rr := httptest.NewRecorder()
	u.DeleteUDA(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d want 500", rr.Code)
	}
}

// --- Form renders ---

func TestUDAs_CreateUDAForm_RendersModal(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	req := httptest.NewRequest(http.MethodGet, "/forms/uda/new", nil)
	rr := httptest.NewRecorder()
	u.CreateUDAForm(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "/udas") {
		t.Error("create form missing POST /udas action")
	}
}

func TestUDAs_EditUDAForm_PreFills(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{
		UDAs: []fakeUDA{{Name: "effort", Type: "numeric", Label: "Effort"}},
	})
	u := newUDAs()

	req := httptest.NewRequest(http.MethodGet, "/forms/uda/effort", nil)
	req.SetPathValue("name", "effort")
	rr := httptest.NewRecorder()
	u.EditUDAForm(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "effort") {
		t.Error("edit form not pre-filled with UDA name")
	}
	if !strings.Contains(body, "Effort") {
		t.Error("edit form not pre-filled with UDA label")
	}
}

func TestUDAs_EditUDAForm_RejectsBadName(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})
	u := newUDAs()

	req := httptest.NewRequest(http.MethodGet, "/forms/uda/bad+name", nil)
	req.SetPathValue("name", "bad+name")
	rr := httptest.NewRecorder()
	u.EditUDAForm(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad name: got %d want 400", rr.Code)
	}
}
