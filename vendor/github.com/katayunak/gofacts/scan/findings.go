package scan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/gofacts/fact"
)

// structural rule IDs apply to a function's own code. A Flow keeps them only
// for functions reachable from its entry points; the other rules describe
// types and schema, which every path shares.
var structural = map[string]bool{
	"SEAM-CONCRETE": true, "TX-NET-CALL": true, "TX-NO-ROLLBACK": true,
	"TX-UNUSED": true, "TX-BYPASS": true,
}

func (p *Program) allFindings() ([]fact.Finding, int) {
	var all []fact.Finding
	all = append(all, p.stateFindings()...)
	all = append(all, p.moneyFindings()...)
	all = append(all, p.seamFindings()...)
	all = append(all, p.txFindings()...)
	all = append(all, p.infraFindings()...)
	all = append(all, p.schemaFindings()...)

	kept := withoutGenerated(all, p.generated, func(f fact.Finding) string { return f.Ref.File })
	dropped := len(all) - len(kept)
	for _, is := range p.ld.Issues {
		kept = append(kept, fact.Finding{
			ID: "LOAD-ERROR", Severity: fact.SevInfo, Basis: fact.Proved,
			Title:  "package did not load or type-check",
			Detail: is.Pkg + ": " + is.Msg + " — facts behind this package are missing, not absent",
			Ref:    fact.CodeRef{Pkg: is.Pkg, File: is.Pos.File, Line: is.Pos.Line},
			Line:   is.Pos.Line,
		})
	}
	fact.SortFindings(kept)
	return kept, dropped
}

func (p *Program) seamFindings() []fact.Finding {
	byFn := map[string][]fact.Seam{}
	var ids []string
	for _, s := range p.seams {
		if s.Injectable {
			continue
		}
		if f := p.funcs[s.In.ID()]; f != nil && f.InTest {
			continue
		}
		if _, ok := byFn[s.In.ID()]; !ok {
			ids = append(ids, s.In.ID())
		}
		byFn[s.In.ID()] = append(byFn[s.In.ID()], s)
	}
	var out []fact.Finding
	for _, id := range ids {
		seams := byFn[id]
		kinds := map[string]bool{}
		for _, s := range seams {
			kinds[string(s.Kind)] = true
		}
		ks := make([]string, 0, len(kinds))
		for k := range kinds {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		f := fact.Finding{
			ID: "SEAM-CONCRETE", Severity: fact.SevHigh, Basis: fact.Proved,
			Title:  "I/O called on a concrete type, so faults cannot be injected",
			Detail: fmt.Sprintf("%d call(s) to %s go straight to a concrete type. A test cannot make them time out, fail, or return a duplicate, which is where payment bugs live.", len(seams), strings.Join(ks, ", ")),
			Ref:    seams[0].In, Line: seams[0].Line,
		}
		for _, s := range seams[1:] {
			f.Related = append(f.Related, s.At())
		}
		out = append(out, f)
	}
	return out
}

func (p *Program) txFindings() []fact.Finding {
	var out []fact.Finding
	for _, tx := range p.txs {
		if f := p.funcs[tx.In.ID()]; f != nil && f.InTest {
			continue
		}
		var net, bypass []fact.TxEffect
		for _, e := range tx.Inside {
			switch {
			case e.Kind.Network():
				net = append(net, e)
			case e.Bypasses:
				bypass = append(bypass, e)
			}
		}
		if len(net) > 0 {
			out = append(out, fact.Finding{
				ID: "TX-NET-CALL", Severity: fact.SevCritical, Basis: weakest(net),
				Title:  "network call inside a database transaction",
				Detail: "While the transaction opened at " + tx.Begin.String() + " is open, this calls " + describe(net) + ". Holding a transaction across a network round trip pins locks for the duration of someone else's timeout: a slow provider becomes a database outage, and a rollback cannot undo what the provider already did.",
				Ref:    tx.In, Line: net[0].At.Line, Related: append([]fact.Pos{tx.Begin}, sites(net[1:])...),
			})
		}
		if len(tx.Leaks) > 0 && !tx.Wrapper {
			out = append(out, fact.Finding{
				ID: "TX-NO-ROLLBACK", Severity: fact.SevHigh, Basis: fact.Proved,
				Title:  "a return leaves the transaction open",
				Detail: fmt.Sprintf("%d return path(s) after the begin at %s reach neither a commit nor a rollback, and no rollback is deferred. The connection and its locks stay held until the pool reclaims them. Pair the begin with `defer tx.Rollback()`, which is a no-op after a successful commit.", len(tx.Leaks), tx.Begin),
				Ref:    tx.In, Line: tx.Leaks[0].Line, Related: append([]fact.Pos{tx.Begin}, tx.Leaks[1:]...),
			})
		}
		if !tx.Used && !tx.Escapes && !tx.Wrapper {
			out = append(out, fact.Finding{
				ID: "TX-UNUSED", Severity: fact.SevHigh, Basis: fact.Proved,
				Title:  "transaction opened but no statement runs on it",
				Detail: "Nothing between the begin and the commit is executed on the transaction or handed to a function that could use it, so every write in between goes through the pool directly and is not atomic. The transaction protects nothing.",
				Ref:    tx.In, Line: tx.Begin.Line,
			})
		}
		if len(bypass) > 0 {
			sev, basis := fact.SevMedium, weakest(bypass)
			if basis == fact.Proved {
				sev = fact.SevHigh
			}
			out = append(out, fact.Finding{
				ID: "TX-BYPASS", Severity: sev, Basis: basis,
				Title:  "database work outside the open transaction",
				Detail: "While the transaction opened at " + tx.Begin.String() + " is open, " + describe(bypass) + " runs without it. That work commits on its own: if the transaction rolls back, it stays; if it commits, the two were never atomic.",
				Ref:    tx.In, Line: bypass[0].At.Line, Related: append([]fact.Pos{tx.Begin}, sites(bypass[1:])...),
			})
		}
	}
	return out
}

func weakest(es []fact.TxEffect) fact.Basis {
	for _, e := range es {
		if e.Basis != fact.Proved {
			return fact.Heuristic
		}
	}
	return fact.Proved
}

func describe(es []fact.TxEffect) string {
	var parts []string
	for _, e := range es {
		what := e.Target
		if e.Via != "" {
			what = e.Via + " (which reaches " + string(e.Kind) + ")"
		}
		parts = append(parts, what+" at "+e.At.String())
	}
	return strings.Join(parts, ", ")
}

func sites(es []fact.TxEffect) []fact.Pos {
	out := make([]fact.Pos, 0, len(es))
	for _, e := range es {
		out = append(out, e.At)
	}
	return out
}

func (p *Program) touchesDatabase() bool {
	for _, s := range p.seams {
		if s.Kind == fact.SeamDB {
			return true
		}
	}
	return false
}

func (p *Program) schemaFindings() []fact.Finding {
	var out []fact.Finding
	if !p.infra.SchemaKnown() && p.touchesDatabase() {
		out = append(out, fact.Finding{
			ID: "SCHEMA-NOT-FOUND", Severity: fact.SevCritical, Basis: fact.Proved,
			Title:  "this module talks to a database, but no schema was found",
			Detail: "No .sql files, no DDL inside Go files and no ORM struct tags were found. Without the schema nothing can tell whether a unique index backs an idempotency key, whether a CHECK guards a balance, or which columns exist, so every uniqueness claim is withheld rather than guessed. Commit the schema, or export one with pg_dump.",
		})
	}
	if missing := p.infra.MigrationsTotal - p.infra.MigrationsWithDown; missing > 0 {
		out = append(out, fact.Finding{
			ID: "MIGRATION-NO-DOWN", Severity: fact.SevMedium, Basis: fact.Proved,
			Title:  fmt.Sprintf("%d of %d migrations cannot be rolled back", missing, p.infra.MigrationsTotal),
			Detail: "These migrations (" + p.infra.MigrationTool + ") have no down step, so the schema only moves forward. A test database built from the migrations cannot be torn down between cases, and a bad migration has no rehearsed way back in production.",
		})
	}
	return out
}
