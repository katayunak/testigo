package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/katayunak/testigo/internal/scanningFlow"
)

// Load reads the previous run's flow
func Load(root string) (*scanningFlow.Flow, error) {
	b, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSidecar
	}
	if err != nil {
		return nil, err
	}

	var f scanningFlow.Flow
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("sidecar is corrupt: %w", err)
	}

	if f.SchemaVersion != scanningFlow.SchemaVersion {
		return nil, fmt.Errorf("sidecar schema v%d, this binary speaks v%d: delete %s and rescan",
			f.SchemaVersion, scanningFlow.SchemaVersion, Path(root))
	}

	if f.Nodes == nil {
		f.Nodes = map[string]*scanningFlow.Node{}
	}

	return &f, nil
}
