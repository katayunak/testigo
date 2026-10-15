package sqlschema

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/katayunak/gofacts/internal/sqltext"
)

type Tool string

const (
	ToolNone          Tool = ""
	ToolGolangMigrate Tool = "golang-migrate"
	ToolGoose         Tool = "goose"
	ToolSQLMigrate    Tool = "sql-migrate"
	ToolDbmate        Tool = "dbmate"
)

// Migration is one SQL migration file split into the part that builds the
// schema and the part that undoes it. Up keeps the file's line numbers: the
// down section is blanked, not removed.
type Migration struct {
	Path    string
	Version string
	Tool    Tool
	Up      string
	HasUp   bool
	HasDown bool
}

var (
	reMigrateFile = regexp.MustCompile(`^(\d+)_.*\.(up|down)\.sql$`)
	reLeadingNum  = regexp.MustCompile(`^(\d+)`)

	markers = []struct {
		tool     Tool
		up, down *regexp.Regexp
	}{
		{ToolGoose, regexp.MustCompile(`(?im)^\s*--\s*\+goose\s+up\b.*$`), regexp.MustCompile(`(?im)^\s*--\s*\+goose\s+down\b.*$`)},
		{ToolSQLMigrate, regexp.MustCompile(`(?im)^\s*--\s*\+migrate\s+up\b.*$`), regexp.MustCompile(`(?im)^\s*--\s*\+migrate\s+down\b.*$`)},
		{ToolDbmate, regexp.MustCompile(`(?im)^\s*--\s*migrate:up\b.*$`), regexp.MustCompile(`(?im)^\s*--\s*migrate:down\b.*$`)},
	}
)

func SplitMigration(path, content string) Migration {
	base := filepath.Base(path)
	m := Migration{Path: path}
	if n := reLeadingNum.FindString(base); n != "" {
		m.Version = n
	}

	if f := reMigrateFile.FindStringSubmatch(base); f != nil {
		m.Tool, m.Version = ToolGolangMigrate, f[1]
		if f[2] == "down" {
			m.HasDown = true
			return m
		}
		m.Up, m.HasUp = content, true
		return m
	}

	for _, mk := range markers {
		upLoc := mk.up.FindStringIndex(content)
		if upLoc == nil {
			continue
		}
		m.Tool, m.HasUp = mk.tool, true
		upStart, upEnd := upLoc[1], len(content)
		if downLoc := mk.down.FindStringIndex(content); downLoc != nil {
			downStart, downEnd := downLoc[1], len(content)
			if downLoc[0] > upLoc[0] {
				upEnd = downLoc[0]
			} else {
				downEnd = upLoc[0]
			}
			m.HasDown = strings.TrimSpace(sqltext.StripComments(content[downStart:downEnd])) != ""
		}
		m.Up = blank(blank(content, 0, upStart), upEnd, len(content))
		return m
	}

	m.Up, m.HasUp = content, true
	return m
}

// blank replaces s[from:to] with spaces except newlines.
func blank(s string, from, to int) string {
	b := []byte(s)
	for i := from; i < to && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

// Order sorts migrations the way migration tools apply them: by numeric
// version, so 10_x runs after 9_x even without zero padding.
func Order(ms []Migration) {
	sort.SliceStable(ms, func(i, j int) bool {
		vi, iok := version(ms[i])
		vj, jok := version(ms[j])
		if iok && jok && vi != vj {
			return vi < vj
		}
		if iok != jok {
			return iok
		}
		return ms[i].Path < ms[j].Path
	})
}

func version(m Migration) (uint64, bool) {
	if m.Version == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(m.Version, 10, 64)
	return n, err == nil
}

// DownCoverage counts migrations a tool expects to be reversible and how many
// of them are. Plain .sql files with no tool convention are not counted: a
// missing down there is not knowable.
func DownCoverage(ms []Migration) (tool Tool, total, withDown int) {
	type unit struct{ up, down bool }
	units := map[string]*unit{}
	var keys []string
	for _, m := range ms {
		if m.Tool == ToolNone {
			continue
		}
		tool = m.Tool
		key := m.Path
		if m.Tool == ToolGolangMigrate {
			key = filepath.Join(filepath.Dir(m.Path), m.Version)
		}
		u, ok := units[key]
		if !ok {
			u = &unit{}
			units[key] = u
			keys = append(keys, key)
		}
		u.up = u.up || m.HasUp
		u.down = u.down || m.HasDown
	}
	for _, k := range keys {
		if !units[k].up {
			continue
		}
		total++
		if units[k].down {
			withDown++
		}
	}
	return tool, total, withDown
}
