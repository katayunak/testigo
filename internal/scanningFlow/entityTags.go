package scanningFlow

import (
	"go/ast"
	"reflect"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/packages"
)

func schemaFromTags(pkgs []*packages.Package, root string) ([]flowEntity.Table, []flowEntity.Constraint) {
	var tables []flowEntity.Table
	var cons []flowEntity.Constraint

	for _, p := range pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					return true
				}

				pos := p.Fset.Position(spec.Pos())
				file := fileRelTo(pos.Filename, root)

				table := flowEntity.Table{
					Name: tableNameOf(spec.Name.Name, st),
					File: file, Line: pos.Line,
				}
				mapped := false

				for _, fld := range st.Fields.List {
					tag := tagOf(fld)
					if tag == "" {
						continue
					}
					pgTag := reflect.StructTag(tag).Get("pg")
					gormTag := reflect.StructTag(tag).Get("gorm")
					if pgTag == "" && gormTag == "" {
						continue
					}
					if pgTag == "-" || gormTag == "-" {
						continue
					}
					for _, nm := range fld.Names {
						col := columnOf(fld, nm.Name)
						if col == "" || col == "-" {
							continue
						}
						mapped = true
						opts := strings.ToLower(pgTag + "," + gormTag)
						c := flowEntity.Column{
							Name:       col,
							NotNull:    strings.Contains(opts, "notnull") || strings.Contains(opts, "not null"),
							PrimaryKey: hasOpt(opts, "pk") || strings.Contains(opts, "primarykey") || strings.Contains(opts, "primary_key"),
						}
						table.Columns = append(table.Columns, c)

						if c.PrimaryKey {
							cons = append(cons, flowEntity.Constraint{
								Table: table.Name, Columns: []string{col}, Kind: "primary_key",
								File: file, Line: pos.Line,
							})
						}
						if hasOpt(opts, "unique") || strings.Contains(opts, "uniqueindex") {
							cons = append(cons, flowEntity.Constraint{
								Table: table.Name, Columns: []string{col}, Kind: "unique_constraint",
								File: file, Line: pos.Line,
							})
						}
					}
				}

				if mapped && len(table.Columns) > 0 {
					tables = append(tables, table)
				}
				return true
			})
		}
	}
	return tables, cons
}

func tagOf(fld *ast.Field) string {
	if fld.Tag == nil {
		return ""
	}
	return strings.Trim(fld.Tag.Value, "`")
}

func hasOpt(opts, want string) bool {
	for _, part := range strings.FieldsFunc(opts, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		if part == want {
			return true
		}
	}
	return false
}

func tableNameOf(structName string, st *ast.StructType) string {
	for _, fld := range st.Fields.List {
		isMarker := false
		for _, nm := range fld.Names {
			if nm.Name == "tableName" {
				isMarker = true
			}
		}
		if !isMarker {
			continue
		}
		tag := tagOf(fld)
		v := reflect.StructTag(tag).Get("pg")
		if v == "" {
			v = reflect.StructTag(tag).Get("gorm")
		}
		name := strings.Split(v, ",")[0]
		name = strings.TrimPrefix(name, "alias:")
		if name != "" && name != "-" {
			return cleanIdent(name)
		}
	}
	return pluralize(snakeCase(structName))
}

func pluralize(s string) string {
	switch {
	case s == "":
		return s
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 && !isVowel(s[len(s)-2]):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
