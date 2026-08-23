package scanningFlow

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Infra reads the environment the code expects, from the files that describe it.
//
// Deliberately regex based and deliberately shallow. A real SQL parser would be
// more correct and would take a week; these patterns cover the shape that
// migration tools actually emit, and anything they miss is visible as an absent
// row rather than a wrong one. The rule everywhere in testigo applies here too:
// a heuristic that reports nothing is recoverable, a heuristic that reports
// something false is not.
func Infra(root string) flowEntity.Infra {
	var out flowEntity.Infra
	seenDir := map[string]bool{}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "vendor", "node_modules", ".testigo", "_to_delete":
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

			cs := parseConstraints(string(body), rel)
			if len(cs) > 0 {
				out.Constraints = append(out.Constraints, cs...)
			}
			if dir := filepath.Dir(rel); !seenDir[dir] {
				seenDir[dir] = true
				out.MigrationDirs = append(out.MigrationDirs, dir)
			}

		case name == "docker-compose.yml" || name == "docker-compose.yaml" || name == "compose.yml":
			body, e := os.ReadFile(path)
			if e == nil {
				out.Services = append(out.Services, parseCompose(string(body), rel)...)
			}

		case strings.HasSuffix(name, ".yml"), strings.HasSuffix(name, ".yaml"),
			strings.HasSuffix(name, ".json"), strings.HasSuffix(name, ".toml"):
			if strings.Contains(rel, "testdata/") {
				return nil
			}
			body, e := os.ReadFile(path)
			if e == nil {
				out.Topics = append(out.Topics, parseTopics(string(body), rel)...)
			}
		}

		return nil
	})
	return out
}

var (
	// CREATE UNIQUE INDEX ... ON table (a, b)
	reUniqueIndex = regexp.MustCompile(`(?is)create\s+unique\s+index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?\S+\s+on\s+([\w."]+)\s*\(([^)]*)\)`)
	// ALTER TABLE t ADD CONSTRAINT c UNIQUE (a, b)
	reUniqueConstraint = regexp.MustCompile(`(?is)alter\s+table\s+([\w."]+)\s+add\s+constraint\s+\S+\s+unique\s*\(([^)]*)\)`)
	// inline: CREATE TABLE t ( ... UNIQUE (a) ... )  /  PRIMARY KEY (a)
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

var (
	reComposeService = regexp.MustCompile(`(?m)^  ([a-zA-Z0-9_-]+):\s*$`)
	reComposeImage   = regexp.MustCompile(`(?m)^\s+image:\s*["']?([^"'\s]+)`)
	reTopicName      = regexp.MustCompile(`(?i)(?:^|[\s"'])topic\w*\s*[:=]\s*["']?([a-zA-Z0-9._-]{3,})`)
	rePartitions     = regexp.MustCompile(`(?i)partitions?\s*[:=]\s*["']?(\d+)`)
	reReplicas       = regexp.MustCompile(`(?i)replica(?:tion)?[_-]?factor\s*[:=]\s*["']?(\d+)`)
)

func parseCompose(body, file string) []flowEntity.Service {
	var out []flowEntity.Service
	blocks := reComposeService.FindAllStringSubmatchIndex(body, -1)
	for i, b := range blocks {
		name := body[b[2]:b[3]]
		end := len(body)
		if i+1 < len(blocks) {
			end = blocks[i+1][0]
		}
		image := ""
		if m := reComposeImage.FindStringSubmatch(body[b[1]:end]); m != nil {
			image = m[1]
		}
		out = append(out, flowEntity.Service{Name: name, Image: image, Kind: kindOfImage(image + " " + name), File: file})
	}
	return out
}

func kindOfImage(s string) string {
	s = strings.ToLower(s)
	for _, k := range []struct{ needle, kind string }{
		{"postgres", "postgres"}, {"timescale", "postgres"}, {"mysql", "mysql"},
		{"maria", "mysql"}, {"redis", "redis"}, {"kafka", "kafka"},
		{"redpanda", "kafka"}, {"nats", "nats"}, {"rabbit", "rabbitmq"},
		{"mongo", "mongo"}, {"elastic", "elasticsearch"}, {"etcd", "etcd"},
		{"clickhouse", "clickhouse"}, {"cassandra", "cassandra"},
	} {
		if strings.Contains(s, k.needle) {
			return k.kind
		}
	}
	return "other"
}

func parseTopics(body, file string) []flowEntity.Topic {
	names := reTopicName.FindAllStringSubmatch(body, -1)
	if len(names) == 0 {
		return nil
	}
	partitions, replicas := 0, 0
	if m := rePartitions.FindStringSubmatch(body); m != nil {
		partitions, _ = strconv.Atoi(m[1])
	}
	if m := reReplicas.FindStringSubmatch(body); m != nil {
		replicas, _ = strconv.Atoi(m[1])
	}
	seen := map[string]bool{}
	var out []flowEntity.Topic
	for _, n := range names {
		name := n[1]
		if seen[name] || strings.Contains(name, "${") {
			continue
		}
		seen[name] = true
		out = append(out, flowEntity.Topic{Name: name, Partitions: partitions, Replicas: replicas, File: file})
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
		// index expressions like lower(email) are not plain columns
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
