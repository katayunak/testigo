package scanningFlow

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func namedType(pkgPath, name string, under types.Type) *types.Named {
	pkg := types.NewPackage(pkgPath, pkgPath[strings.LastIndex(pkgPath, "/")+1:])
	return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), under, nil)
}

func signatureOf(params ...types.Type) *types.Signature {
	var vars []*types.Var
	for _, p := range params {
		vars = append(vars, types.NewVar(token.NoPos, nil, "", p))
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(vars...), nil, false)
}

func TestAGenericCallbackIsNotAPathToIO(t *testing.T) {
	if !genericSignature(signatureOf(types.Typ[types.Int32])) {
		t.Error("func(rune) rune links under CHA to every function of that shape; following it made strings.ToLower look like network I/O")
	}
	if !genericSignature(signatureOf(types.Typ[types.Int], types.Typ[types.Int])) {
		t.Error("a sort comparator is plumbing, not a route to a socket")
	}
}

func TestACallbackNamingALibraryTypeIsFollowed(t *testing.T) {
	conn := namedType("github.com/go-pg/pg/v10/internal/pool", "Conn", types.NewStruct(nil, nil))
	ctx := namedType("context", "Context", types.NewInterfaceType(nil, nil))
	if genericSignature(signatureOf(ctx, types.NewPointer(conn))) {
		t.Error("go-pg runs every query inside func(context.Context, *pool.Conn); skipping it dropped real database seams")
	}
}

func TestStreamsAndErrorsAreNotIOAbstractions(t *testing.T) {
	for _, name := range []string{"error", "io.Writer", "io.Reader", "context.Context", "fmt.Stringer"} {
		if !plumbingIfaces[name] {
			t.Errorf("%s resolves under CHA to every implementation in the program, including socket reads and writes", name)
		}
	}
	if plumbingIfaces["net.Conn"] {
		t.Error("net.Conn is how real clients reach the socket; it has to be followed")
	}
}

func TestAnAsynchronousSendIsKeptWithoutAStaticPath(t *testing.T) {
	for _, name := range []string{"Publish", "SendByContext", "RequestWithContext", "Enqueue"} {
		if !asyncEffect(name) {
			t.Errorf("%s hands work to a background writer, so no static path reaches the socket; it must be kept by name", name)
		}
	}
	for _, name := range []string{"New", "WithDetails", "FromIncomingContext", "Get", "Pending", "Where", "SpanFromContext"} {
		if asyncEffect(name) {
			t.Errorf("%s is not a send; treating it as one would bring the false seams back", name)
		}
	}
}

func TestTxCloseWithoutCommitIsARollback(t *testing.T) {
	rollingBack := []string{
		"(*github.com/go-pg/pg/v10.Tx).Close",
		"(*database/sql.Tx).Close",
		"(*github.com/jmoiron/sqlx.Tx).Close",
	}
	for _, target := range rollingBack {
		var f flowEntity.Facts
		applyTxFacts(&f, target)
		if !f.RollsBackTx {
			t.Errorf("%s: go-pg and database/sql both roll back an uncommitted transaction on Close; missing it made every defer tx.Close() a false TX-NO-ROLLBACK", target)
		}
	}
}

func TestOnlyATransactionsCloseCountsAsRollback(t *testing.T) {
	notRollbacks := []string{
		"(*database/sql.DB).Close",
		"(*github.com/redis/go-redis/v9.Client).Close",
		"(*os.File).Close",
	}
	for _, target := range notRollbacks {
		var f flowEntity.Facts
		applyTxFacts(&f, target)
		if f.RollsBackTx {
			t.Errorf("%s: closing a connection pool or a file is not a rollback; treating every Close as one would hide a real TX-NO-ROLLBACK", target)
		}
	}
}
