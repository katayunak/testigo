package scanningFlow

import (
	"go/ast"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/testigo/internal/codeRef"
)

// Candidate is a possible entry point, with the evidence for it. Evidence is
// shown rather than hidden so the user can judge the guess instead of trusting
// it — the same principle applied everywhere else in testigo.
type Candidate struct {
	Entry    EntryPoint
	Score    int
	File     string
	Line     int
	Evidence []string
}

var paymentVerb = regexp.MustCompile(`(?i)(pay|payment|charge|capture|authori[sz]e|refund|void|settle|transfer|payout|withdraw|deposit|topup|top_up|invoice|order|checkout|reconcil|ledger|disburse)`)
var callbackWord = regexp.MustCompile(`(?i)(webhook|callback|notify|notification|ipn|consume|handle|process|worker|cron|job|scheduler)`)

// SuggestEntries finds plausible starting points for a payment flow.
//
// This exists because the first thing a new user has to do is name their entry
// points, and getting that wrong produces an empty graph and a bad first
// impression. It is a convenience, not an analysis: the output is a list to
// choose from, never something testigo acts on by itself.
func SuggestEntries(root string, patterns []string, env []string) ([]Candidate, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	cfg := &packages.Config{Mode: loadMode, Dir: root, Tests: false, Env: env}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil || !fd.Name.IsExported() {
					continue
				}
				sig, _ := p.TypesInfo.Defs[fd.Name].(*types.Func)
				if sig == nil {
					continue
				}
				score, ev := scoreEntry(fd, sig.Type().(*types.Signature))
				if score <= 0 {
					continue
				}
				pos := p.Fset.Position(fd.Pos())
				out = append(out, Candidate{
					Entry: EntryPoint{
						Pkg: p.PkgPath, Symbol: codeRef.Symbol(fd),
						Label: strings.Join(ev, "+"),
					},
					Score: score, File: relPath(p.Fset, fd.Pos(), root), Line: pos.Line, Evidence: ev,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Entry.Pkg+out[i].Entry.Symbol < out[j].Entry.Pkg+out[j].Entry.Symbol
	})
	return out, nil
}

func scoreEntry(fd *ast.FuncDecl, sig *types.Signature) (int, []string) {
	var score int
	var ev []string
	params := sig.Params()

	if params.Len() == 2 &&
		typeIs(params.At(0).Type(), "net/http.ResponseWriter") &&
		typeIs(params.At(1).Type(), "*net/http.Request") {
		score += 5
		ev = append(ev, "http-handler")
	}
	if params.Len() > 0 && typeIs(params.At(0).Type(), "context.Context") {
		score += 1
		ev = append(ev, "ctx-first")
		// A ctx-first method returning (*T, error) is the shape every generated
		// gRPC service method has.
		if sig.Results().Len() == 2 && params.Len() == 2 {
			score += 2
			ev = append(ev, "rpc-shape")
		}
	}
	name := fd.Name.Name
	recv := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recv = codeRef.Symbol(fd)
	}
	if paymentVerb.MatchString(name) || paymentVerb.MatchString(recv) {
		score += 4
		ev = append(ev, "payment-verb")
	}
	if callbackWord.MatchString(name) || callbackWord.MatchString(recv) {
		score += 3
		ev = append(ev, "callback-shape")
	}
	return score, ev
}

func typeIs(t types.Type, want string) bool {
	return types.TypeString(t, relativeTo) == want
}
