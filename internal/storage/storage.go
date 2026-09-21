package storage

import (
	"errors"
	"path/filepath"

	"github.com/katayunak/testigo/internal/config"
)

const flowFile = "flow.json"

func Dir(root string) string { return config.Dir(root) }

func Path(root string) string { return filepath.Join(Dir(root), flowFile) }

var ErrNoSidecar = errors.New("no sidecar found")
