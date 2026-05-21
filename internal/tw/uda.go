package tw

import (
	"regexp"
	"strings"
)

// UDA describes one user-defined attribute as configured in ~/.taskrc.
//
// Type is one of "string", "numeric", "date", or "duration"; if the user's
// taskrc declares some other (or empty) type we still surface the entry but
// fall back to "string" rendering on the UI side.
//
// Label is the human-friendly heading. Empty Label means the UI should fall
// back to the bare Name.
type UDA struct {
	Name  string
	Type  string
	Label string

	// Values is the closed enumeration declared by `uda.<name>.values=a,b,c`
	// in ~/.taskrc. Empty means no constraint - the UI renders a free-text
	// input. Non-empty means render a <select> and reject submitted values
	// that aren't a member.
	//
	// Note: Taskwarrior emits a trailing empty entry on `task _get
	// rc.uda.<name>.values` when an empty/cleared value is allowed (e.g.
	// the built-in priority returns "H,M,L,"). ListUDAs strips that empty.
	// The empty/clear case is handled by the form's separate "(none)" option.
	Values []string

	// BuiltIn is true for pseudo-UDAs that Taskwarrior configures internally
	// (e.g. "priority"). They appear in `task _udas` output but are not safe
	// to edit or delete via the portal.
	BuiltIn bool
}

// builtinUDANames is the set of names that Taskwarrior ships as pseudo-UDAs.
// These appear in `task _udas` but must not be editable through the portal.
var builtinUDANames = map[string]bool{
	"priority": true,
}

// UDANamePattern matches the conservative subset of identifiers we accept as
// UDA names: a leading letter, then up to 63 letters/digits/underscores.
// Anything outside this set is rejected during discovery so a hostile taskrc
// cannot smuggle parser tokens (rc.*, +tag, due:..., shell metachars) into a
// later `task add` argv via the cached UDA list.
var UDANamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// QuoteArg wraps a value in double quotes and escapes the bytes that
// would terminate the quoted region (backslash and double-quote). It serves
// as the single quoting helper across the package - description, project,
// date, and UDA value args all route through here.
func QuoteArg(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
