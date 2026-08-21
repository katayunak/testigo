package storage

import (
	"encoding/json"
	"os"

	"github.com/katayunak/testigo/internal/models"
)

// Save writes the flow atomically. A half-written sidecar after a crash would
// look like a corrupt repo state on the next run, so we write to a temp file in
// the same directory and rename, which is atomic on every filesystem we care
// about.
func Save(root string, f *models.Flow) error {
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

	return os.Rename(tmp.Name(), Path(root))
}
