package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
)

func TestWriteIfTaskParseError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       bool
		wantField  string
		wantSubstr string
	}{
		{name: "nil error", err: nil, want: false},
		{name: "not a task exit error", err: errors.New("boom"), want: false},
		{
			name: "no stderr captured",
			err:  &tw.TaskExitError{ExitCode: 2, Wrapped: errors.New("exit status 2")},
			want: false,
		},
		{
			name:       "recurring task without due",
			err:        &tw.TaskExitError{ExitCode: 2, Stderr: "A recurring task must also have a 'due' date.", Wrapped: errors.New("exit status 2")},
			want:       true,
			wantField:  "due",
			wantSubstr: "needs a Due date",
		},
		{
			name:       "unparseable date",
			err:        &tw.TaskExitError{ExitCode: 2, Stderr: "Could not interpret the date 'potato'.", Wrapped: errors.New("exit status 2")},
			want:       true,
			wantField:  "",
			wantSubstr: "rejected one of the dates",
		},
		{
			name: "unrelated failure falls through to the caller",
			err:  &tw.TaskExitError{ExitCode: 1, Stderr: "Unknown command 'frobnicate'.", Wrapped: errors.New("exit status 1")},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			got := writeIfTaskParseError(rec, tc.err)

			if got != tc.want {
				t.Fatalf("writeIfTaskParseError = %v, want %v", got, tc.want)
			}
			if !tc.want {
				if rec.Body.Len() != 0 {
					t.Errorf("wrote a body for a non-match: %q", rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			body := rec.Body.String()
			if want := `data-field-error="` + tc.wantField + `"`; !strings.Contains(body, want) {
				t.Errorf("body missing %s\ngot: %s", want, body)
			}
			if !strings.Contains(body, tc.wantSubstr) {
				t.Errorf("body missing %q\ngot: %s", tc.wantSubstr, body)
			}
		})
	}
}
