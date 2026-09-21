package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

const flowFile = "flow.json"

func FlowPath(root string) string { return filepath.Join(Dir(root), flowFile) }

var ErrNoSidecar = errors.New("no sidecar found")

func LoadFlow(root string) (*flowEntity.Flow, error) {
	b, err := os.ReadFile(FlowPath(root))
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
			f.SchemaVersion, flowEntity.SchemaVersion, FlowPath(root))
	}

	if f.Nodes == nil {
		f.Nodes = map[string]*flowEntity.Node{}
	}

	return &f, nil
}

func SaveFlow(root string, f *flowEntity.Flow) error {
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".flow-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), FlowPath(root))
}
