package scanningFlow

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/packages"
)

func Infra(pkgs []*packages.Package, root string) flowEntity.Infra {
	var out flowEntity.Infra
	seenDir := map[string]bool{}
	sawSQLFile := false

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "vendor", "node_modules", ".testigo", "testigo", "_to_delete":
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		switch {
		case strings.HasSuffix(name, ".sql"):
			body, e := os.ReadFile(path)
			if e != nil {
				return nil
			}

			out.Constraints = append(out.Constraints, parseConstraints(string(body), rel)...)

			tables, fks, checks := parseSchema(string(body), rel)
			out.Tables = append(out.Tables, tables...)
			out.ForeignKeys = append(out.ForeignKeys, fks...)
			out.Checks = append(out.Checks, checks...)
			out.MigrationFiles++
			sawSQLFile = true

			if dir := filepath.Dir(rel); !seenDir[dir] {
				seenDir[dir] = true
				out.MigrationDirs = append(out.MigrationDirs, dir)
			}

		}

		return nil
	})

	if sawSQLFile {
		out.SchemaSources = append(out.SchemaSources, "sql files")
	}

	chunks, tool, withDown, total := sqlFromGo(pkgs, root)
	out.MigrationTool = tool
	out.MigrationsTotal = total
	out.MigrationsWithDown = withDown

	sawGoSQL := false
	for _, c := range chunks {
		if c.Down || isReversalOnly(c.SQL) {
			continue
		}
		sawGoSQL = true
		out.Constraints = append(out.Constraints, parseConstraints(c.SQL, c.File)...)
		tables, fks, checks := parseSchema(c.SQL, c.File)
		out.Tables = append(out.Tables, tables...)
		out.ForeignKeys = append(out.ForeignKeys, fks...)
		out.Checks = append(out.Checks, checks...)

		if dir := filepath.Dir(c.File); !seenDir[dir] {
			seenDir[dir] = true
			out.MigrationDirs = append(out.MigrationDirs, dir)
		}
	}
	if sawGoSQL {
		out.MigrationFiles += len(chunks)
		out.SchemaSources = append(out.SchemaSources, "sql inside go files")
	}

	tagTables, tagCons := schemaFromTags(pkgs, root)
	have := map[string]bool{}
	for _, t := range out.Tables {
		have[t.Name] = true
	}
	added := false
	for _, t := range tagTables {
		if have[t.Name] {
			continue
		}
		have[t.Name] = true
		out.Tables = append(out.Tables, t)
		added = true
	}
	for _, c := range tagCons {
		out.Constraints = append(out.Constraints, c)
		added = true
	}
	if added {
		out.SchemaSources = append(out.SchemaSources, "orm struct tags")
	}

	sort.Strings(out.MigrationDirs)
	return out
}

var (
	reUniqueIndex = regexp.MustCompile(`(?is)create\s+unique\s+index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?\S+\s+on\s+([\w."]+)\s*\(([^)]*)\)`)

	reUniqueConstraint = regexp.MustCompile(`(?is)alter\s+table\s+([\w."]+)\s+add\s+constraint\s+\S+\s+unique\s*\(([^)]*)\)`)

	reCreateTable  = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?([\w."]+)\s*\((.*?)\n\s*\)\s*;`)
	reInlineUnique = regexp.MustCompile(`(?i)\bunique\s*\(([^)]*)\)`)
	rePrimaryKey   = regexp.MustCompile(`(?i)\bprimary\s+key\s*\(([^)]*)\)`)
)

func parseConstraints(sql, file string) []flowEntity.Constraint {
	var out []flowEntity.Constraint
	add := func(table, cols, kind string, at int) {
		c := flowEntity.Constraint{
			Table: cleanIdent(table), Columns: splitColumns(cols), Kind: kind,
			File: file, Line: lineAt(sql, at),
		}
		if len(c.Columns) > 0 {
			out = append(out, c)
		}
	}
	for _, m := range reUniqueIndex.FindAllStringSubmatchIndex(sql, -1) {
		add(sql[m[2]:m[3]], sql[m[4]:m[5]], "unique_index", m[0])
	}
	for _, m := range reUniqueConstraint.FindAllStringSubmatchIndex(sql, -1) {
		add(sql[m[2]:m[3]], sql[m[4]:m[5]], "unique_constraint", m[0])
	}
	for _, m := range reCreateTable.FindAllStringSubmatchIndex(sql, -1) {
		table, body := sql[m[2]:m[3]], sql[m[4]:m[5]]
		for _, u := range reInlineUnique.FindAllStringSubmatch(body, -1) {
			add(table, u[1], "unique_constraint", m[0])
		}
		for _, pk := range rePrimaryKey.FindAllStringSubmatch(body, -1) {
			add(table, pk[1], "primary_key", m[0])
		}
	}
	return out
}

func cleanIdent(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, `"`, ""))
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func splitColumns(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		c := cleanIdent(part)

		if c == "" || strings.Contains(c, "(") {
			continue
		}
		if i := strings.IndexAny(c, " \t\n"); i >= 0 {
			c = c[:i]
		}
		out = append(out, c)
	}
	return out
}

func lineAt(s string, offset int) int {
	if offset > len(s) {
		return 0
	}
	return strings.Count(s[:offset], "\n") + 1
}
