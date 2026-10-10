package scan

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/sqlschema"
)

// schema assembles what the database enforces from three sources, in order of
// trust: SQL migration files applied in version order, DDL embedded in Go
// migrations, and ORM struct tags for tables neither of those mentions.
func (p *Program) schema(skip []string) fact.Infra {
	var infra fact.Infra
	var s sqlschema.Schema
	dirs := map[string]bool{}

	migrations := p.sqlFiles(skip)
	sqlschema.Order(migrations)
	for _, m := range migrations {
		dirs[filepath.ToSlash(filepath.Dir(m.Path))] = true
		if m.HasUp {
			s.ApplySQL(m.Up, m.Path)
		}
	}
	if len(migrations) > 0 {
		infra.SchemaSources = append(infra.SchemaSources, "sql files")
		infra.MigrationFiles = len(migrations)
	}
	sqlTool, sqlTotal, sqlDown := sqlschema.DownCoverage(migrations)

	goMig := p.goMigrations()
	if len(goMig.chunks) > 0 {
		for _, c := range goMig.chunks {
			s.ApplySQL(c.sql, c.file)
			dirs[filepath.ToSlash(filepath.Dir(c.file))] = true
		}
		infra.SchemaSources = append(infra.SchemaSources, "sql inside go files")
		infra.MigrationFiles += len(goMig.chunks)
	}
	s.Fill(&infra)

	infra.MigrationTool = joinTools(string(sqlTool), goMig.tool)
	infra.MigrationsTotal = sqlTotal + goMig.total
	infra.MigrationsWithDown = sqlDown + goMig.withDown

	tagTables, tagCons := p.schemaFromTags()
	added := false
	for _, t := range tagTables {
		if infra.HasTable(t.Name) {
			continue
		}
		infra.Tables = append(infra.Tables, t)
		added = true
	}
	for _, c := range tagCons {
		if _, dup := infra.UniqueOn(c.Table, c.Columns[0]); dup && len(c.Columns) == 1 {
			continue
		}
		infra.Constraints = append(infra.Constraints, c)
		added = true
	}
	if added {
		infra.SchemaSources = append(infra.SchemaSources, "orm struct tags")
	}

	for d := range dirs {
		infra.MigrationDirs = append(infra.MigrationDirs, d)
	}
	sort.Strings(infra.MigrationDirs)
	return infra
}

func joinTools(tools ...string) string {
	var out []string
	for _, t := range tools {
		if t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, ", ")
}

func (p *Program) sqlFiles(skip []string) []sqlschema.Migration {
	var out []sqlschema.Migration
	_ = filepath.WalkDir(p.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != p.root && p.skipDir(d.Name(), skip) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".sql") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(p.root, path)
		out = append(out, sqlschema.SplitMigration(filepath.ToSlash(rel), string(body)))
		return nil
	})
	return out
}

var reDDL = regexp.MustCompile(`(?is)\b(create\s+table|alter\s+table|create\s+(unique\s+)?index|create\s+type|drop\s+table|drop\s+index)\b`)

type goMigrations struct {
	chunks          []sqlChunk
	tool            string
	total, withDown int
}

type sqlChunk struct {
	sql, file string
	line      int
}

// registration lists the calls that register a Go migration and the argument
// that holds its down function.
var registration = map[string]struct {
	tool string
	down int
}{
	"MustRegisterTx": {"go-pg/migrations", 1}, "MustRegister": {"go-pg/migrations", 1},
	"RegisterTx": {"go-pg/migrations", 1}, "Register": {"go-pg/migrations", 1},
	"AddMigration": {"goose", 1}, "AddMigrationContext": {"goose", 1},
	"AddMigrationNoTx": {"goose", 1}, "AddMigrationNoTxContext": {"goose", 1},
	"AddNamedMigration": {"goose", 2}, "AddNamedMigrationContext": {"goose", 2},
}

// goMigrations reads DDL from string constants in Go files, skipping any that
// sit inside a registered down function: a down migration undoes the schema,
// it does not describe it.
func (p *Program) goMigrations() goMigrations {
	var out goMigrations
	tools := map[string]bool{}
	for _, pkg := range p.prod {
		for _, f := range pkg.Syntax {
			var downs [][2]token.Pos
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				reg, ok := registration[calleeName(call.Fun)]
				if !ok || !importsMigrationTool(pkg, call, reg.tool) {
					return true
				}
				tools[reg.tool] = true
				out.total++
				if reg.down < len(call.Args) {
					down := call.Args[reg.down]
					if !isNil(pkg.TypesInfo, down) {
						out.withDown++
					}
					downs = append(downs, [2]token.Pos{down.Pos(), down.End()})
				}
				return true
			})
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				tv, ok := pkg.TypesInfo.Types[lit]
				if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
					return true
				}
				text := constant.StringVal(tv.Value)
				if !reDDL.MatchString(text) {
					return true
				}
				for _, s := range downs {
					if lit.Pos() >= s[0] && lit.End() <= s[1] {
						return true
					}
				}
				out.chunks = append(out.chunks, sqlChunk{sql: text, file: p.rel(lit.Pos()), line: p.line(lit.Pos())})
				return true
			})
		}
	}
	sort.SliceStable(out.chunks, func(i, j int) bool {
		if out.chunks[i].file != out.chunks[j].file {
			return out.chunks[i].file < out.chunks[j].file
		}
		return out.chunks[i].line < out.chunks[j].line
	})
	var names []string
	for t := range tools {
		names = append(names, t)
	}
	sort.Strings(names)
	out.tool = strings.Join(names, ", ")
	return out
}

func importsMigrationTool(pkg *packages.Package, call *ast.CallExpr, tool string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	path := fn.Pkg().Path()
	switch tool {
	case "go-pg/migrations":
		return strings.Contains(path, "go-pg/migrations")
	case "goose":
		return strings.Contains(path, "pressly/goose")
	}
	return false
}

func isNil(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	return ok && tv.IsNil()
}

func calleeName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr:
		return calleeName(f.X)
	}
	return ""
}

// ormKeys are the tag keys that carry a column name, in priority order.
var ormKeys = []string{"db", "sql", "pg", "bun", "json"}

var ormOptionWord = map[string]bool{
	"pk": true, "nopk": true, "unique": true, "notnull": true, "use_zero": true,
	"array": true, "hstore": true, "composite": true, "msgpack": true, "json": true,
	"discard_unknown_columns": true, "primarykey": true, "primary_key": true,
	"autoincrement": true, "index": true, "uniqueindex": true, "embedded": true,
	"not null": true, "-": true, "nullzero": true, "scanonly": true, "rel": true,
}

// columnOf is the column a struct field is stored in according to its tags,
// falling back to the snake_case of its name.
func columnOf(fld *ast.Field, fieldName string) string {
	tag := tagOf(fld)
	for _, part := range strings.Split(tag.Get("gorm"), ";") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "column:"); ok && v != "" {
			return v
		}
	}
	for _, key := range ormKeys {
		v := tag.Get(key)
		if v == "" {
			continue
		}
		name := strings.TrimSpace(strings.Split(v, ",")[0])
		if name == "" || name == "-" || strings.Contains(name, ":") || ormOptionWord[strings.ToLower(name)] {
			continue
		}
		return name
	}
	return snakeCase(fieldName)
}

// tableOf is the table a struct is stored in: a go-pg tableName or bun
// BaseModel tag, a gorm TableName method returning a constant, or the plural
// snake_case of the type name.
func (p *Program) tableOf(named *types.Named) string {
	if named == nil {
		return ""
	}
	p.collectTableNameMethods()
	if name := p.tableNameMethod(named); name != "" {
		return name
	}
	if st, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			fld := st.Field(i)
			tag := reflect.StructTag(st.Tag(i))
			if fld.Name() == "tableName" {
				if v := strings.TrimPrefix(strings.Split(firstNonEmpty(tag.Get("pg"), tag.Get("gorm")), ",")[0], "alias:"); v != "" && v != "-" {
					return cleanSQLIdent(v)
				}
			}
			if fld.Embedded() && fld.Name() == "BaseModel" {
				for _, part := range strings.Split(tag.Get("bun"), ",") {
					if v, ok := strings.CutPrefix(strings.TrimSpace(part), "table:"); ok && v != "" {
						return cleanSQLIdent(v)
					}
				}
			}
		}
	}
	return pluralize(snakeCase(named.Obj().Name()))
}

// tableNameMethod reads `func (T) TableName() string { return "x" }`.
func (p *Program) tableNameMethod(named *types.Named) string {
	for i := 0; i < named.NumMethods(); i++ {
		if m := named.Method(i); m.Name() == "TableName" {
			return p.tableNames[m]
		}
	}
	return ""
}

func (p *Program) collectTableNameMethods() {
	if p.tableNames != nil {
		return
	}
	p.tableNames = map[*types.Func]string{}
	for _, pkg := range p.prod {
		for _, f := range pkg.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || fd.Name.Name != "TableName" || fd.Body == nil || len(fd.Body.List) != 1 {
					continue
				}
				ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					continue
				}
				tv, ok := pkg.TypesInfo.Types[ret.Results[0]]
				if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
					continue
				}
				if obj, ok := pkg.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					p.tableNames[obj] = cleanSQLIdent(constant.StringVal(tv.Value))
				}
			}
		}
	}
}

func cleanSQLIdent(s string) string {
	s = strings.NewReplacer(`"`, "", "`", "").Replace(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// schemaFromTags reads tables and uniqueness from go-pg, gorm and bun tags.
// Composite indexes are grouped by name: gorm's uniqueIndex:idx_x on two
// fields is one constraint over both, not two single-column ones.
func (p *Program) schemaFromTags() ([]fact.Table, []fact.Constraint) {
	p.collectTableNameMethods()
	var tables []fact.Table
	var cons []fact.Constraint
	for _, pkg := range p.prod {
		for _, f := range pkg.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					return true
				}
				var named *types.Named
				if obj, ok := pkg.TypesInfo.Defs[spec.Name].(*types.TypeName); ok {
					named, _ = obj.Type().(*types.Named)
				}
				table := fact.Table{Name: p.tableOf(named), File: p.rel(spec.Pos()), Line: p.line(spec.Pos())}
				groups := map[string]*fact.Constraint{}
				var order []string
				for _, fld := range st.Fields.List {
					tag := tagOf(fld)
					pgTag, gormTag, bunTag := tag.Get("pg"), tag.Get("gorm"), tag.Get("bun")
					if pgTag == "" && gormTag == "" && bunTag == "" || pgTag == "-" || gormTag == "-" || bunTag == "-" {
						continue
					}
					for _, nm := range fld.Names {
						if nm.Name == "tableName" {
							continue
						}
						col := columnOf(fld, nm.Name)
						opts := ormOptions(pgTag, gormTag, bunTag)
						c := fact.Column{Name: col, NotNull: opts.notNull, PrimaryKey: opts.pk}
						table.Columns = append(table.Columns, c)
						if opts.pk {
							cons = append(cons, fact.Constraint{Table: table.Name, Columns: []string{col}, Kind: fact.KindPrimaryKey, File: table.File, Line: p.line(fld.Pos())})
						}
						for _, g := range opts.uniqueGroups {
							if g == "" {
								cons = append(cons, fact.Constraint{Table: table.Name, Columns: []string{col}, Kind: fact.KindUniqueConstraint, File: table.File, Line: p.line(fld.Pos())})
								continue
							}
							if groups[g] == nil {
								groups[g] = &fact.Constraint{Table: table.Name, Kind: fact.KindUniqueIndex, Name: g, File: table.File, Line: p.line(fld.Pos())}
								order = append(order, g)
							}
							groups[g].Columns = append(groups[g].Columns, col)
						}
					}
				}
				for _, g := range order {
					cons = append(cons, *groups[g])
				}
				if len(table.Columns) > 0 {
					tables = append(tables, table)
				}
				return true
			})
		}
	}
	return tables, cons
}

type ormOpts struct {
	pk, notNull  bool
	uniqueGroups []string
}

func ormOptions(pgTag, gormTag, bunTag string) ormOpts {
	var o ormOpts
	for _, part := range splitOpts(pgTag + "," + bunTag) {
		lower := strings.ToLower(part)
		switch {
		case lower == "pk":
			o.pk = true
		case lower == "notnull" || lower == "not null":
			o.notNull = true
		case lower == "unique":
			o.uniqueGroups = append(o.uniqueGroups, "")
		case strings.HasPrefix(lower, "unique:"):
			o.uniqueGroups = append(o.uniqueGroups, part[len("unique:"):])
		}
	}
	for _, part := range strings.Split(gormTag, ";") {
		key, val, _ := strings.Cut(strings.TrimSpace(part), ":")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "primarykey", "primary_key":
			o.pk = true
		case "not null":
			o.notNull = true
		case "unique":
			o.uniqueGroups = append(o.uniqueGroups, "")
		case "uniqueindex":
			name := strings.Split(strings.TrimSpace(val), ",")[0]
			o.uniqueGroups = append(o.uniqueGroups, name)
		}
	}
	if o.pk {
		o.notNull = true
	}
	return o
}

func splitOpts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' })
}
