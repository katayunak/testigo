package storage

import (
	"errors"
	"path/filepath"

	"github.com/katayunak/testigo/internal/config"
)

const flowFile = "flow.json"

// Dir is testigo's directory in the repository. It is config.Dir, not a second
// opinion about where testigo keeps things — one owner for the path means the
// two can never drift.
func Dir(root string) string { return config.Dir(root) }

func Path(root string) string { return filepath.Join(Dir(root), flowFile) }

var ErrNoSidecar = errors.New("no sidecar found")
