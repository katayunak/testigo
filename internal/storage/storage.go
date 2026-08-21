package storage

import (
	"errors"
	"path/filepath"
)

const (
	dirName  = ".testigo"
	flowFile = "flow.json"
)

func Dir(root string) string { return filepath.Join(root, dirName) }

func Path(root string) string { return filepath.Join(Dir(root), flowFile) }

var ErrNoSidecar = errors.New("no sidecar found")
