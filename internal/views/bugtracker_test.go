package views

import (
	"testing"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
)

// BugTrackerInfoFor

func TestBugTrackerInfoFor_Nil_NoUDAs(t *testing.T) {
	if got := BugTrackerInfoFor(nil); got != nil {
		t.Errorf("nil map: got %v want nil", got)
	}
}

func TestBugTrackerInfoFor_Nil_EmptyUDAs(t *testing.T) {
	if got := BugTrackerInfoFor(map[string]string{}); got != nil {
		t.Errorf("empty map: got %v want nil", got)
	}
}

func TestBugTrackerInfoFor_Nil_NoTrackerUDAs(t *testing.T) {
	udas := map[string]string{"mycustomfield": "value"}
	if got := BugTrackerInfoFor(udas); got != nil {
		t.Errorf("non-tracker UDAs: got %v want nil", got)
	}
}

func TestBugTrackerInfoFor_GitHub(t *testing.T) {
	udas := map[string]string{
		"githubnumber": "123",
		"githuburl":    "https://github.com/owner/repo/issues/123",
		"githubrepo":   "owner/repo",
	}
	info := BugTrackerInfoFor(udas)
	if info == nil {
		t.Fatal("expected non-nil info for GitHub UDAs")
	}
	if info.Name != "GitHub" {
		t.Errorf("Name: got %q want GitHub", info.Name)
	}
	if info.Label != "GH #123" {
		t.Errorf("Label: got %q want \"GH #123\"", info.Label)
	}
	if info.URL != "https://github.com/owner/repo/issues/123" {
		t.Errorf("URL: got %q", info.URL)
	}
	if info.Repo != "owner/repo" {
		t.Errorf("Repo: got %q want owner/repo", info.Repo)
	}
}

func TestBugTrackerInfoFor_GitLab(t *testing.T) {
	udas := map[string]string{
		"gitlabnumber": "456",
		"gitlaburl":    "https://gitlab.com/ns/proj/-/issues/456",
	}
	info := BugTrackerInfoFor(udas)
	if info == nil {
		t.Fatal("expected non-nil info for GitLab UDAs")
	}
	if info.Name != "GitLab" {
		t.Errorf("Name: got %q want GitLab", info.Name)
	}
	if info.Label != "GL #456" {
		t.Errorf("Label: got %q want \"GL #456\"", info.Label)
	}
}

func TestBugTrackerInfoFor_Jira(t *testing.T) {
	udas := map[string]string{
		"jiraid":   "PROJ-789",
		"jiraurl":  "https://jira.example.com/browse/PROJ-789",
		"jirastatus": "In Progress",
	}
	info := BugTrackerInfoFor(udas)
	if info == nil {
		t.Fatal("expected non-nil info for Jira UDAs")
	}
	if info.Name != "Jira" {
		t.Errorf("Name: got %q want Jira", info.Name)
	}
	// Jira has empty ChipPrefix so label is just the id
	if info.Label != "PROJ-789" {
		t.Errorf("Label: got %q want \"PROJ-789\"", info.Label)
	}
}

func TestBugTrackerInfoFor_Linear(t *testing.T) {
	udas := map[string]string{
		"linearidentifier": "ENG-42",
		"linearurl":        "https://linear.app/team/issue/ENG-42",
		"linearteam":       "Engineering",
	}
	info := BugTrackerInfoFor(udas)
	if info == nil {
		t.Fatal("expected non-nil info for Linear UDAs")
	}
	if info.Label != "ENG-42" {
		t.Errorf("Label: got %q want \"ENG-42\"", info.Label)
	}
	if info.Repo != "Engineering" {
		t.Errorf("Repo: got %q want Engineering", info.Repo)
	}
}

func TestBugTrackerInfoFor_Bugzilla(t *testing.T) {
	udas := map[string]string{
		"bugzillabugid": "98765",
		"bugzillaurl":   "https://bugzilla.mozilla.org/show_bug.cgi?id=98765",
	}
	info := BugTrackerInfoFor(udas)
	if info == nil {
		t.Fatal("expected non-nil info for Bugzilla UDAs")
	}
	if info.Label != "BZ #98765" {
		t.Errorf("Label: got %q want \"BZ #98765\"", info.Label)
	}
}

func TestBugTrackerInfoFor_NoURL(t *testing.T) {
	udas := map[string]string{"githubnumber": "7"}
	info := BugTrackerInfoFor(udas)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.URL != "" {
		t.Errorf("URL: got %q want empty", info.URL)
	}
}

// IsBugTrackerUDA

func TestIsBugTrackerUDA_KnownKeys(t *testing.T) {
	known := []string{
		"githubnumber", "githuburl", "githubrepo", "githubtitle",
		"gitlabnumber", "gitlaburl",
		"jiraid", "jiraurl", "jirastatus",
		"bugzillabugid", "bugzillaurl",
		"bitbucketid",
		"tracnumber", "tracurl",
		"redmineid",
		"adoid", "adourl",
		"clickupid",
		"linearidentifier", "linearurl",
		"pagureid",
		"youtrackissue",
		"gerritid",
		"trellocardid",
		"phabricatorid",
	}
	for _, name := range known {
		if !IsBugTrackerUDA(name) {
			t.Errorf("IsBugTrackerUDA(%q) = false, want true", name)
		}
	}
}

func TestIsBugTrackerUDA_UnknownKeys(t *testing.T) {
	unknown := []string{
		"priority", "project", "tags", "due",
		"mycustom", "estimate", "sprint",
	}
	for _, name := range unknown {
		if IsBugTrackerUDA(name) {
			t.Errorf("IsBugTrackerUDA(%q) = true, want false", name)
		}
	}
}

// nonTrackerUDAs

func TestNonTrackerUDAs_FiltersTrackerUDAs(t *testing.T) {
	udas := []tw.UDA{
		{Name: "githubnumber"},
		{Name: "githuburl"},
		{Name: "mycustom"},
		{Name: "sprint"},
	}
	got := nonTrackerUDAs(udas)
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2", len(got))
	}
	if got[0].Name != "mycustom" || got[1].Name != "sprint" {
		t.Errorf("unexpected names: %v", got)
	}
}

func TestNonTrackerUDAs_AllNonTracker(t *testing.T) {
	udas := []tw.UDA{{Name: "foo"}, {Name: "bar"}}
	got := nonTrackerUDAs(udas)
	if len(got) != 2 {
		t.Errorf("len: got %d want 2", len(got))
	}
}

func TestNonTrackerUDAs_AllTracker(t *testing.T) {
	udas := []tw.UDA{{Name: "githubnumber"}, {Name: "githuburl"}}
	got := nonTrackerUDAs(udas)
	if len(got) != 0 {
		t.Errorf("len: got %d want 0", len(got))
	}
}

// sortedNonTrackerUDANames

func TestSortedNonTrackerUDANames_SortsAndFilters(t *testing.T) {
	m := map[string]string{
		"githubnumber": "42",
		"githuburl":    "https://...",
		"zebra":        "z",
		"alpha":        "a",
		"gitlabnumber": "7",
	}
	got := sortedNonTrackerUDANames(m)
	want := []string{"alpha", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestSortedNonTrackerUDANames_SkipsEmptyValues(t *testing.T) {
	m := map[string]string{
		"mycustom": "",
		"sprint":   "Q1",
	}
	got := sortedNonTrackerUDANames(m)
	if len(got) != 1 || got[0] != "sprint" {
		t.Errorf("got %v want [sprint]", got)
	}
}
