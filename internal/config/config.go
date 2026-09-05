package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

const (
	// DirName is the one directory testigo owns inside a repository.
	//
	// Everything it writes lives here: the config, the rules, the sidecar, the
	// prompt pack and the answers. One visible directory beats four scattered
	// files, two of which used to be hidden — a dot-directory is easy to forget
	// you have, and the pack inside it is something you are meant to READ.
	DirName = "testigo"

	// FileName is the config, inside DirName.
	FileName = "config.json"
)

// Dir is testigo's directory inside a repository.
func Dir(root string) string { return filepath.Join(root, DirName) }

// Path is the config file.
func Path(root string) string { return filepath.Join(Dir(root), FileName) }

// legacy paths, from before everything moved under testigo/. Kept only so the
// error message can tell someone exactly what to move, rather than reporting a
// missing file they can see with their own eyes.
const (
	legacyConfig = "testigo.json"
	legacyRules  = "testigo.rules.json"
	legacyDir    = ".testigo"
)

// Config is the entry points, and nothing else.
//
// There used to be a `patterns` field here, holding Go package patterns so a
// large repository could load less. It is gone, and the reason is worth keeping:
// narrowing the load is a knob whose misuse is SILENT. Exclude a package the
// flow actually calls into and that call becomes invisible — the graph simply
// stops there, and the report looks complete. Trading a correct answer for
// twenty seconds is a bad trade for a tool whose only product is trust.
//
// If scan time ever becomes a real problem, the fix is caching what did not
// change, not looking at less code.
type Config struct {
	// Entries are the starting points of the payment flow. Plural on purpose.
	Entries []Entry `json:"entries"`
}

type Entry struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	Label  string `json:"label,omitempty"`
}

func Load(root string) (*Config, error) {
	b, err := os.ReadFile(Path(root))
	if os.IsNotExist(err) {
		// Everything testigo writes moved under testigo/. If the old layout is
		// still on disk, say exactly what to move rather than reporting a
		// missing file the person can plainly see is present.
		if _, old := os.Stat(filepath.Join(root, legacyConfig)); old == nil {
			return nil, fmt.Errorf(
				"testigo now keeps everything in one directory.\n\n"+
					"  mkdir -p %s\n"+
					"  mv %s %s\n"+
					"  mv %s %s 2>/dev/null || true\n"+
					"  mv %s/* %s/ 2>/dev/null || true\n"+
					"  rmdir %s 2>/dev/null || true\n\n"+
					"then add %s/ to .gitignore, except %s/agentResponse.json which is worth committing",
				DirName,
				legacyConfig, filepath.Join(DirName, FileName),
				legacyRules, filepath.Join(DirName, RulesFileName),
				legacyDir, DirName,
				legacyDir,
				DirName, DirName)
		}
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(stripComments(b), &c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	// An empty list is valid on disk: `testigo init` writes one, and
	// `testigo entry add` exists to fill it. Only the commands that need to WALK
	// the flow require entries, and they say so themselves.

	for i, e := range c.Entries {
		if e.Pkg == "" || e.Symbol == "" {
			return nil, fmt.Errorf("%s: entry %d needs both pkg and symbol", FileName, i)
		}
	}
	return &c, nil
}

func stripComments(b []byte) []byte {
	lines := strings.Split(string(b), "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "//") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func Save(root string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		return err
	}
	return os.WriteFile(Path(root), append(b, '\n'), 0o644)
}

func (c *Config) RequireEntries() error {
	if len(c.Entries) == 0 {
		return fmt.Errorf("%s has no entry points\n\n"+
			"Declare at least one:\n\n"+
			"  testigo entry . add <import-path>#<Symbol> \"label\"\n\n"+
			"or edit %s by hand. The template is in the file.", filepath.Join(DirName, FileName), filepath.Join(DirName, FileName))
	}
	return nil
}

func (c *Config) EntryPoints() []flowEntity.EntryPoint {
	out := make([]flowEntity.EntryPoint, 0, len(c.Entries))
	for _, e := range c.Entries {
		out = append(out, flowEntity.EntryPoint{Pkg: e.Pkg, Symbol: e.Symbol, Label: e.Label})
	}
	return out
}

// Example is written by `testigo init` so a first-time user has something to
// edit rather than a blank file and a manual to read.
const Example = `{
  // Every function where a payment flow can START.
  //
  // testigo does not guess these. Which functions begin a flow is something you
  // know and the code does not say: a handler called ProcessRequest may be the
  // whole flow, and one called CreatePayment may be a wrapper nobody calls any
  // more. A guessed list invites someone to accept it without reading, and an
  // entry point accepted without reading is a whole path through the system that
  // silently never gets analysed.
  //
  // List ALL of them. A payment flow usually has several, and they rejoin the
  // same state machine:
  //
  //   { "pkg": "example.com/pay/internal/api",
  //     "symbol": "(*Server).CreatePayment", "label": "API create" },
  //
  //   { "pkg": "example.com/pay/internal/webhook",
  //     "symbol": "(*Handler).ProviderCallback", "label": "provider webhook" },
  //
  //   { "pkg": "example.com/pay/internal/recon",
  //     "symbol": "(*Job).Reconcile", "label": "reconciliation job" },
  //
  //   { "pkg": "example.com/pay/internal/consumer",
  //     "symbol": "(*Consumer).HandleSettlement", "label": "settlement queue" }
  //
  // Symbol syntax:  Func  |  Type.Method  |  (*Type).Method
  //
  // Add them here, or from the command line:
  //   testigo entry . add example.com/pay/internal/api#'(*Server).CreatePayment' "API create"

  "entries": []
}
`
