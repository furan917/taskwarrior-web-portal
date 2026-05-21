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

func newTagOps() *TagOps {
	return &TagOps{TW: tw.NewClient(), Logger: discardLogger()}
}

func TestMergeTag_ValidMergeTriggersCorrectArgs(t *testing.T) {
	logDir := installFakeTaskWith(t, fakeTaskOpts{RecordArgv: true})

	h := newTagOps()
	form := url.Values{"to": {"newtag"}}
	req := httptest.NewRequest(http.MethodPost, "/tags/oldtag/merge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "oldtag")
	rr := httptest.NewRecorder()
	h.MergeTag(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Errorf("HX-Refresh: got %q want true", got)
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	found := false
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(logDir, e.Name()))
		args := strings.Split(strings.TrimSpace(string(data)), "\n")
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "+oldtag") &&
			strings.Contains(joined, "modify") &&
			strings.Contains(joined, "-oldtag") &&
			strings.Contains(joined, "+newtag") {
			found = true
		}
	}
	if !found {
		t.Error("no task invocation with expected merge args recorded")
	}
}

func TestMergeTag_SameFromToReturns400(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})

	h := newTagOps()
	form := url.Values{"to": {"mytag"}}
	req := httptest.NewRequest(http.MethodPost, "/tags/mytag/merge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "mytag")
	rr := httptest.NewRecorder()
	h.MergeTag(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("same from/to: got %d want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "itself") {
		t.Errorf("body missing 'itself' message: %s", rr.Body.String())
	}
}

func TestMergeTag_InvalidTagInPathReturns400(t *testing.T) {
	h := newTagOps()
	for _, bad := range []string{"bad-tag!", "+evil", "a;b", "rc.foo=bar"} {
		form := url.Values{"to": {"newtag"}}
		req := httptest.NewRequest(http.MethodPost, "/tags/placeholder/merge", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("name", bad)
		rr := httptest.NewRecorder()
		h.MergeTag(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("bad path tag %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestMergeTag_InvalidToTagReturns400(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{})

	h := newTagOps()
	form := url.Values{"to": {"bad tag!"}}
	req := httptest.NewRequest(http.MethodPost, "/tags/oldtag/merge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "oldtag")
	rr := httptest.NewRecorder()
	h.MergeTag(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid to tag: got %d want 400", rr.Code)
	}
}

func TestMergeTagForm_RendersModal(t *testing.T) {
	installFakeTaskWith(t, fakeTaskOpts{Tags: []string{"oldtag", "newtag", "other"}})

	h := newTagOps()
	req := httptest.NewRequest(http.MethodGet, "/forms/tag/oldtag/merge", nil)
	req.SetPathValue("name", "oldtag")
	rr := httptest.NewRecorder()
	h.MergeTagForm(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "oldtag") {
		t.Error("modal missing from tag name")
	}
	if !strings.Contains(body, "/tags/oldtag/merge") {
		t.Error("modal missing post action URL")
	}
}

func TestMergeTagForm_RejectsBadName(t *testing.T) {
	h := newTagOps()
	req := httptest.NewRequest(http.MethodGet, "/forms/tag/bad+name/merge", nil)
	req.SetPathValue("name", "bad+name")
	rr := httptest.NewRecorder()
	h.MergeTagForm(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad name: got %d want 400", rr.Code)
	}
}
