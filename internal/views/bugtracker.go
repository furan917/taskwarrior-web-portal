package views

import (
	"fmt"
	"sort"

	"github.com/furan917/taskwarrior-web-portal/internal/tw"
)

// bugTrackerSpec describes how to detect, display, and suppress one bug tracker service.
type bugTrackerSpec struct {
	Name       string   // display name: "GitHub", "Jira", etc.
	DetectUDA  string   // UDA whose non-empty presence signals this tracker
	IDUDA      string   // UDA holding the issue number / id
	URLUDA     string   // UDA holding the issue URL (may be empty string)
	RepoUDA    string   // UDA holding the repo/project slug (may be empty string)
	ChipPrefix string   // short prefix for the chip label, e.g. "GH"
	NumericID  bool     // if true the chip label is "{prefix} #{id}", else just "{id}"
	AllUDAs    []string // every UDA key for this tracker, used for suppression
}

// bugTrackerSpecs lists every known bugwarrior service. Order matters: first match wins.
// Derived from https://github.com/GothenburgBitFactory/bugwarrior service sources.
var bugTrackerSpecs = []bugTrackerSpec{
	{
		Name: "GitHub", DetectUDA: "githubnumber", IDUDA: "githubnumber",
		URLUDA: "githuburl", RepoUDA: "githubrepo",
		ChipPrefix: "GH", NumericID: true,
		AllUDAs: []string{
			"githubtitle", "githubbody", "githubcreatedon", "githubupdatedat",
			"githubclosedon", "githubmilestone", "githuburl", "githubrepo",
			"githubtype", "githubnumber", "githubuser", "githubnamespace",
			"githubstate", "githubdraft",
		},
	},
	{
		Name: "GitLab", DetectUDA: "gitlabnumber", IDUDA: "gitlabnumber",
		URLUDA: "gitlaburl", RepoUDA: "gitlabrepo",
		ChipPrefix: "GL", NumericID: true,
		AllUDAs: []string{
			"gitlabtitle", "gitlabdescription", "gitlabcreatedon", "gitlabupdatedat",
			"gitlabduedate", "gitlabmilestone", "gitlaburl", "gitlabrepo",
			"gitlabtype", "gitlabnumber", "gitlabstate", "gitlabupvotes",
			"gitlabdownvotes", "gitlabwip", "gitlabauthor", "gitlabassignee",
			"gitlabnamespace", "gitlabweight",
		},
	},
	{
		Name: "Jira", DetectUDA: "jiraid", IDUDA: "jiraid",
		URLUDA: "jiraurl", RepoUDA: "",
		ChipPrefix: "", NumericID: false,
		AllUDAs: []string{
			"jiraissuetype", "jirasummary", "jiraurl", "jiraid",
			"jiradescription", "jiraestimate", "jirafixversion",
			"jiracreatedts", "jirastatus", "jirasubtasks", "jiraparent",
		},
	},
	{
		Name: "Bugzilla", DetectUDA: "bugzillabugid", IDUDA: "bugzillabugid",
		URLUDA: "bugzillaurl", RepoUDA: "bugzillaproduct",
		ChipPrefix: "BZ", NumericID: true,
		AllUDAs: []string{
			"bugzillaurl", "bugzillasummary", "bugzillabugid", "bugzillastatus",
			"bugzillaneedinfo", "bugzillaproduct", "bugzillacomponent", "bugzillaassignedon",
		},
	},
	{
		Name: "Bitbucket", DetectUDA: "bitbucketid", IDUDA: "bitbucketid",
		URLUDA: "bitbucketurl", RepoUDA: "",
		ChipPrefix: "BB", NumericID: true,
		AllUDAs: []string{"bitbuckettitle", "bitbucketurl", "bitbucketid"},
	},
	{
		Name: "Trac", DetectUDA: "tracnumber", IDUDA: "tracnumber",
		URLUDA: "tracurl", RepoUDA: "",
		ChipPrefix: "TR", NumericID: true,
		AllUDAs: []string{"tracsummary", "tracurl", "tracnumber", "traccomponent"},
	},
	{
		Name: "Redmine", DetectUDA: "redmineid", IDUDA: "redmineid",
		URLUDA: "redmineurl", RepoUDA: "redmineprojectname",
		ChipPrefix: "RM", NumericID: true,
		AllUDAs: []string{
			"redmineurl", "redminesubject", "redmineid", "redminedescription",
			"redminetracker", "redminestatus", "redmineauthor", "redminecategory",
			"redminestartdate", "redminespenthours", "redmineestimatedhours",
			"redminecreatedon", "redmineupdatedon", "redmineduedate",
			"redmineassignedto", "redmineprojectname",
		},
	},
	{
		Name: "Azure DevOps", DetectUDA: "adoid", IDUDA: "adoid",
		URLUDA: "adourl", RepoUDA: "adonamespace",
		ChipPrefix: "ADO", NumericID: true,
		AllUDAs: []string{
			"adotitle", "adodescription", "adoid", "adourl",
			"adotype", "adostate", "adoactivity", "adopriority",
			"adoremainingwork", "adoparent", "adonamespace",
		},
	},
	{
		Name: "ClickUp", DetectUDA: "clickupid", IDUDA: "clickupid",
		URLUDA: "clickupurl", RepoUDA: "clickupproject",
		ChipPrefix: "CU", NumericID: false,
		AllUDAs: []string{
			"clickupid", "clickupdescription", "clickupstatus", "clickupupdated",
			"clickupcreator", "clickupurl", "clickuplistname", "clickupproject",
			"clickupfolder", "clickupspace", "clickupname",
		},
	},
	{
		Name: "Linear", DetectUDA: "linearidentifier", IDUDA: "linearidentifier",
		URLUDA: "linearurl", RepoUDA: "linearteam",
		ChipPrefix: "", NumericID: false,
		AllUDAs: []string{
			"linearurl", "lineartitle", "lineardescription", "linearstatus",
			"linearidentifier", "linearteam", "linearcreator", "linearassignee",
			"linearcreated", "linearupdated", "linearclosed",
		},
	},
	{
		Name: "Pagure", DetectUDA: "pagureid", IDUDA: "pagureid",
		URLUDA: "pagureurl", RepoUDA: "pagurerepo",
		ChipPrefix: "PR", NumericID: true,
		AllUDAs: []string{
			"paguretitle", "paguredatecreated", "pagureurl",
			"pagurerepo", "paguretype", "pagureid",
		},
	},
	{
		Name: "YouTrack", DetectUDA: "youtrackissue", IDUDA: "youtrackissue",
		URLUDA: "youtrackurl", RepoUDA: "youtrackproject",
		ChipPrefix: "", NumericID: false,
		AllUDAs: []string{
			"youtrackissue", "youtracksummary", "youtrackurl",
			"youtrackproject", "youtracknumber",
		},
	},
	{
		Name: "Gerrit", DetectUDA: "gerritid", IDUDA: "gerritid",
		URLUDA: "gerriturl", RepoUDA: "",
		ChipPrefix: "CR", NumericID: true,
		AllUDAs: []string{
			"gerritsummary", "gerriturl", "gerritid",
			"gerritbranch", "gerrittopic", "gerritstatus", "gerritwip",
		},
	},
	{
		Name: "Trello", DetectUDA: "trellocardid", IDUDA: "trellocardidshort",
		URLUDA: "trellourl", RepoUDA: "trelloboard",
		ChipPrefix: "Trello", NumericID: true,
		AllUDAs: []string{
			"trellocard", "trellocardid", "trellocardidshort", "trellodescription",
			"trelloboard", "trellolist", "trelloshortlink", "trelloshorturl", "trellourl",
		},
	},
	{
		Name: "Phabricator", DetectUDA: "phabricatorid", IDUDA: "phabricatorid",
		URLUDA: "phabricatorurl", RepoUDA: "",
		ChipPrefix: "", NumericID: false,
		AllUDAs: []string{"phabricatortitle", "phabricatorurl", "phabricatortype", "phabricatorid"},
	},
	{
		Name: "Taiga", DetectUDA: "taigaid", IDUDA: "taigaid",
		URLUDA: "taigaurl", RepoUDA: "",
		ChipPrefix: "Taiga", NumericID: true,
		AllUDAs: []string{"taigasummary", "taigaurl", "taigaid"},
	},
	{
		Name: "Kanboard", DetectUDA: "kanboardtaskid", IDUDA: "kanboardtaskid",
		URLUDA: "kanboardurl", RepoUDA: "kanboardprojectname",
		ChipPrefix: "KB", NumericID: true,
		AllUDAs: []string{
			"kanboardtaskid", "kanboardtasktitle", "kanboardtaskdescription",
			"kanboardprojectid", "kanboardprojectname", "kanboardurl",
		},
	},
	{
		Name: "Pivotal Tracker", DetectUDA: "pivotalid", IDUDA: "pivotalid",
		URLUDA: "pivotalurl", RepoUDA: "pivotalprojectname",
		ChipPrefix: "PT", NumericID: true,
		AllUDAs: []string{
			"pivotalurl", "pivotaldescription", "pivotalstorytype", "pivotalprojectid",
			"pivotalprojectname", "pivotalowners", "pivotalrequesters", "pivotalid",
			"pivotalestimate", "pivotalblockers", "pivotalcreated", "pivotalupdated", "pivotalclosed",
		},
	},
	{
		Name: "Todoist", DetectUDA: "todoistid", IDUDA: "todoistid",
		URLUDA: "todoisturl", RepoUDA: "",
		ChipPrefix: "TD", NumericID: false,
		AllUDAs: []string{
			"todoistassignee", "todoistassigner", "todoistcontent", "todoistdescription",
			"todoistdue", "todoistdeadline", "todoistduration", "todoistid",
			"todoistparentid", "todoistsection", "todoisturl",
		},
	},
	{
		Name: "Nextcloud Deck", DetectUDA: "nextclouddeckcardid", IDUDA: "nextclouddeckcardid",
		URLUDA: "", RepoUDA: "nextclouddeckboardtitle",
		ChipPrefix: "Deck", NumericID: true,
		AllUDAs: []string{
			"nextclouddeckauthor", "nextclouddeckboardid", "nextclouddeckboardtitle",
			"nextclouddeckstackid", "nextclouddeckstacktitle", "nextclouddeckcardid",
			"nextclouddeckcardtitle", "nextclouddeckdescription",
			"nextclouddeckorder", "nextclouddeckassignee",
		},
	},
	{
		Name: "Gmail", DetectUDA: "gmailthreadid", IDUDA: "gmailthreadid",
		URLUDA: "gmailurl", RepoUDA: "",
		ChipPrefix: "Mail", NumericID: false,
		AllUDAs: []string{
			"gmailthreadid", "gmailsubject", "gmailurl", "gmaillastsender",
			"gmaillastsenderaddr", "gmaillastmessageid", "gmailsnippet", "gmaillabels",
		},
	},
	{
		Name: "Debian BTS", DetectUDA: "btsnumber", IDUDA: "btsnumber",
		URLUDA: "btsurl", RepoUDA: "btspackage",
		ChipPrefix: "BTS", NumericID: true,
		AllUDAs: []string{
			"btssubject", "btsurl", "btsnumber", "btspackage",
			"btssource", "btsforwarded", "btsstatus",
		},
	},
	{
		Name: "Gitbug", DetectUDA: "gitbugid", IDUDA: "gitbugid",
		URLUDA: "", RepoUDA: "",
		ChipPrefix: "Gitbug", NumericID: false,
		AllUDAs: []string{"gitbugauthor", "gitbugid", "gitbugstate", "gitbugtitle"},
	},
	{
		Name: "Teamwork", DetectUDA: "teamwork_id", IDUDA: "teamwork_id",
		URLUDA: "teamwork_url", RepoUDA: "",
		ChipPrefix: "TW", NumericID: true,
		AllUDAs: []string{
			"teamwork_url", "teamwork_title", "teamwork_description_long",
			"teamwork_project_id", "teamwork_status", "teamwork_id",
		},
	},
	{
		Name: "Logseq", DetectUDA: "logseqid", IDUDA: "logseqid",
		URLUDA: "logsequri", RepoUDA: "logseqpage",
		ChipPrefix: "Logseq", NumericID: false,
		AllUDAs: []string{
			"logseqid", "logsequuid", "logseqstate", "logseqtitle",
			"logseqdone", "logsequri", "logseqscheduled", "logseqdeadline", "logseqpage",
		},
	},
}

// bugTrackerUDASet is a set of all known bugwarrior UDA keys across every service.
// Built once at init time for O(1) IsBugTrackerUDA lookups.
var bugTrackerUDASet map[string]struct{}

func init() {
	bugTrackerUDASet = make(map[string]struct{})
	for _, spec := range bugTrackerSpecs {
		for _, name := range spec.AllUDAs {
			bugTrackerUDASet[name] = struct{}{}
		}
	}
}

// BugTrackerInfo holds the extracted display values for one task's tracker link.
type BugTrackerInfo struct {
	Name  string // service display name
	Label string // pre-computed chip label, e.g. "GH #123"
	URL   string // full issue URL (may be empty)
	Repo  string // repo/project slug (may be empty)
}

// BugTrackerInfoFor returns tracker display info for the given UDA map,
// or nil if no known tracker UDA is present.
func BugTrackerInfoFor(udas map[string]string) *BugTrackerInfo {
	if len(udas) == 0 {
		return nil
	}
	for i := range bugTrackerSpecs {
		spec := &bugTrackerSpecs[i]
		id := udas[spec.DetectUDA]
		if id == "" {
			continue
		}
		return &BugTrackerInfo{
			Name:  spec.Name,
			Label: chipLabel(spec, id),
			URL:   udas[spec.URLUDA],
			Repo:  udas[spec.RepoUDA],
		}
	}
	return nil
}

func chipLabel(spec *bugTrackerSpec, id string) string {
	if spec.ChipPrefix == "" {
		return id
	}
	if spec.NumericID {
		return fmt.Sprintf("%s #%s", spec.ChipPrefix, id)
	}
	return fmt.Sprintf("%s %s", spec.ChipPrefix, id)
}

// IsBugTrackerUDA reports whether name is a UDA written by any bugwarrior service.
func IsBugTrackerUDA(name string) bool {
	_, ok := bugTrackerUDASet[name]
	return ok
}

// nonTrackerUDAs returns the subset of udas that are NOT bugwarrior-managed.
func nonTrackerUDAs(udas []tw.UDA) []tw.UDA {
	out := udas[:0:0]
	for _, u := range udas {
		if !IsBugTrackerUDA(u.Name) {
			out = append(out, u)
		}
	}
	return out
}

// sortedNonTrackerUDANames returns the non-bugtracker UDA keys that have
// a non-empty value, sorted alphabetically.
func sortedNonTrackerUDANames(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == "" || IsBugTrackerUDA(k) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
