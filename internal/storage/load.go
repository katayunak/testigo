package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Load reads the previous run's flow
func Load(root string) (*flowEntity.Flow, error) {
	b, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSidecar
	}
	if err != nil {
		return nil, err
	}

	var f flowEntity.Flow
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("sidecar is corrupt: %w", err)
	}

	if f.SchemaVersion != flowEntity.SchemaVersion {
		return nil, fmt.Errorf("sidecar schema v%d, this binary speaks v%d: delete %s and rescan",
			f.SchemaVersion, flowEntity.SchemaVersion, Path(root))
	}

	if f.Nodes == nil {
		f.Nodes = map[string]*flowEntity.Node{}
	}

	return &f, nil
}
