// Package cite checks the file:line references a model puts in its answers.
//
// A citation that has the right shape proves nothing: models invent plausible
// paths, cite the line after the one they mean, and quote code that is not
// there. A Verifier answers the questions that matter — is the file inside the
// repository, does the line exist, is the quoted code on it — and treats the
// path itself as untrusted input.
package cite

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Citation struct {
	File    string
	Line    int
	EndLine int
}

func (c Citation) String() string {
	if c.EndLine > c.Line {
		return fmt.Sprintf("%s:%d-%d", c.File, c.Line, c.EndLine)
	}
	return fmt.Sprintf("%s:%d", c.File, c.Line)
}

var re = regexp.MustCompile(`([\w./\-]+\.(?:go|sql|proto|ya?ml|json|toml|md)):(\d+)(?:-(\d+))?`)

// Parse finds every citation in s.
func Parse(s string) []Citation {
	var out []Citation
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		c := Citation{File: m[1]}
		c.Line, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			c.EndLine, _ = strconv.Atoi(m[3])
		}
		out = append(out, c)
	}
	return out
}

// Has reports whether s contains at least one citation of a Go file, the rule
// testigo applies to every proof field.
func Has(s string) bool {
	for _, c := range Parse(s) {
		if strings.HasSuffix(c.File, ".go") {
			return true
		}
	}
	return false
}

var (
	ErrOutsideRoot = errors.New("path is outside the repository")
	ErrNoSuchFile  = errors.New("no such file")
	ErrNoSuchLine  = errors.New("no such line")
	ErrNotQuoted   = errors.New("quoted code is not at the cited lines")
)

const maxFileBytes = 8 << 20

// QuoteSlack is how many lines around a citation a quote may sit on. Models
// are routinely off by one or two; more than that is a different claim.
const QuoteSlack = 2

type Verifier struct {
	root  string
	lines map[string][]string
}

func NewVerifier(root string) (*Verifier, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	return &Verifier{root: resolved, lines: map[string][]string{}}, nil
}

// Check verifies the file exists inside the root and the lines exist in it.
func (v *Verifier) Check(c Citation) error {
	_, err := v.span(c)
	return err
}

// Lines returns the cited lines.
func (v *Verifier) Lines(c Citation) ([]string, error) { return v.span(c) }

// Quote verifies that snippet appears, ignoring whitespace differences, within
// QuoteSlack lines of the citation.
func (v *Verifier) Quote(c Citation, snippet string) error {
	lines, err := v.file(c.File)
	if err != nil {
		return err
	}
	if _, err := v.span(c); err != nil {
		return err
	}
	from := max(c.Line-QuoteSlack, 1)
	to := min(max(c.EndLine, c.Line)+QuoteSlack, len(lines))
	window := squeeze(strings.Join(lines[from-1:to], "\n"))
	if want := squeeze(snippet); want == "" || !strings.Contains(window, want) {
		return fmt.Errorf("%s: %w", c, ErrNotQuoted)
	}
	return nil
}

func (v *Verifier) span(c Citation) ([]string, error) {
	lines, err := v.file(c.File)
	if err != nil {
		return nil, err
	}
	end := max(c.EndLine, c.Line)
	if c.Line < 1 || end > len(lines) {
		return nil, fmt.Errorf("%s: %w (file has %d)", c, ErrNoSuchLine, len(lines))
	}
	return lines[c.Line-1 : end], nil
}

func (v *Verifier) file(rel string) ([]string, error) {
	if lines, ok := v.lines[rel]; ok {
		return lines, nil
	}
	full, err := v.resolve(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", rel, ErrNoSuchFile)
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("%s: file too large to verify (%d bytes)", rel, info.Size())
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, ErrNoSuchFile)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxFileBytes)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	v.lines[rel] = lines
	return lines, nil
}

// resolve refuses absolute paths, parent traversal and symlinks that lead out
// of the root. The path came from a model; it is input, not a location.
func (v *Verifier) resolve(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%q: %w", rel, ErrOutsideRoot)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q: %w", rel, ErrOutsideRoot)
	}
	full := filepath.Join(v.root, clean)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, ErrNoSuchFile)
	}
	if resolved != v.root && !strings.HasPrefix(resolved, v.root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q: %w", rel, ErrOutsideRoot)
	}
	return resolved, nil
}

func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }
