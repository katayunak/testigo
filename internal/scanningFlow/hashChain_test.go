package scanningFlow

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

const hashChainFixtureSrc = `package chain

import "crypto/sha256"

type Log struct {
	Hash []byte
	Data string
}

func (l Log) ChainLog(previous *Log) Log {
	h := sha256.New()
	if previous != nil {
		h.Write(previous.Hash)
	}
	h.Write([]byte(l.Data))
	l.Hash = h.Sum(nil)
	return l
}

type Order struct {
	Total int
}

func (o Order) Merge(other *Order) Order {
	return Order{Total: o.Total + other.Total}
}

type Password struct {
	Digest []byte
}

func (p *Password) SetFrom(plain string) {
	sum := sha256.Sum256([]byte(plain))
	p.Digest = sum[:]
}
`

func loadHashChainFixture(t *testing.T) []*packages.Package {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/chain\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chain.go"), []byte(hashChainFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &packages.Config{
		Mode: loadMode,
		Dir:  dir,
		Env:  append(os.Environ(), "GOWORK=off"),
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Fatalf("package error: %v", e)
		}
	}
	return pkgs
}

func TestExtractHashChainsFindsASelfTypedHashingMethod(t *testing.T) {
	pkgs := loadHashChainFixture(t)
	chains := extractHashChains(pkgs, t.TempDir())

	if len(chains) != 1 {
		t.Fatalf("got %d chains, want 1: %+v", len(chains), chains)
	}
	if chains[0].Type != "example.com/chain.Log" {
		t.Fatalf("got type %q, want %q", chains[0].Type, "example.com/chain.Log")
	}
	if chains[0].Method.Symbol != "Log.ChainLog" {
		t.Fatalf("got method %q, want %q", chains[0].Method.Symbol, "Log.ChainLog")
	}
}

func TestExtractHashChainsIgnoresASelfTypedMethodThatDoesNotHash(t *testing.T) {
	pkgs := loadHashChainFixture(t)
	chains := extractHashChains(pkgs, t.TempDir())

	for _, c := range chains {
		if c.Type == "example.com/chain.Order" {
			t.Fatalf("Order.Merge takes another Order but never hashes anything, and should not be reported as a hash chain")
		}
	}
}

func TestExtractHashChainsIgnoresHashingThatIsNotSelfTyped(t *testing.T) {
	pkgs := loadHashChainFixture(t)
	chains := extractHashChains(pkgs, t.TempDir())

	for _, c := range chains {
		if c.Type == "example.com/chain.Password" {
			t.Fatalf("Password.SetFrom hashes a plain string, not a previous Password, and should not be reported as a hash chain")
		}
	}
}
