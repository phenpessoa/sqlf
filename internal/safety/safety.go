// Package safety checks that formatting did not change what the SQL means:
// the parse trees and the comments of input and output must match.
package safety

import (
	"fmt"
	"slices"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/phenpessoa/sqlf/internal/builtins"
	"github.com/phenpessoa/sqlf/internal/printer"
)

// Check returns an error when output does not parse to the same tree as
// input (ignoring positions and the canonical forms the printer chooses),
// or when the comments differ.
func Check(input, output string) error {
	in, err := pgquery.Parse(input)
	if err != nil {
		return fmt.Errorf("input does not parse: %w", err)
	}
	out, err := pgquery.Parse(output)
	if err != nil {
		return fmt.Errorf("formatted output does not parse: %w", err)
	}
	if len(in.Stmts) != len(out.Stmts) {
		return fmt.Errorf("formatted output has %d statements, input has %d", len(out.Stmts), len(in.Stmts))
	}
	for i := range in.Stmts {
		a, b := in.Stmts[i].Stmt, out.Stmts[i].Stmt
		if err := checkBody(stmtText(input, in.Stmts[i]), stmtText(output, out.Stmts[i]), a, b); err != nil {
			return fmt.Errorf("statement %d: %w", i+1, err)
		}
		normalize(a)
		normalize(b)
		if !proto.Equal(a, b) {
			return fmt.Errorf("formatting would change statement %d", i+1)
		}
	}

	ca, err := comments(input)
	if err != nil {
		return err
	}
	cb, err := comments(output)
	if err != nil {
		return err
	}
	if !slices.Equal(ca, cb) {
		return fmt.Errorf("formatting would change the comments")
	}
	return nil
}

func comments(src string) ([]string, error) {
	res, err := pgquery.Scan(src)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range res.Tokens {
		if t.Token != pg.Token_SQL_COMMENT && t.Token != pg.Token_C_COMMENT {
			continue
		}
		lines := strings.Split(src[t.Start:t.End], "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " \t\r")
		}
		out = append(out, strings.Join(lines, "\n"))
	}
	return out, nil
}

// normalize rewrites a tree in place so that the printer's canonical
// choices compare equal to what the user wrote.
func normalize(n *pg.Node) {
	walk(n.ProtoReflect())
}

func walk(m protoreflect.Message) {
	switch v := m.Interface().(type) {
	case *pg.TypeName:
		normalizeTypeName(v)
	case *pg.ColumnDef:
		sortColumnConstraints(v)
	case *pg.CreateFunctionStmt:
		sortFunctionOptions(v)
		clearCheckedBody(v)
	}
	var positions []protoreflect.FieldDescriptor
	m.Range(func(fd protoreflect.FieldDescriptor, val protoreflect.Value) bool {
		switch {
		case isPosition(fd):
			positions = append(positions, fd)
		case fd.IsList() && fd.Message() != nil:
			list := val.List()
			for i := 0; i < list.Len(); i++ {
				walk(list.Get(i).Message())
			}
		case fd.Message() != nil && !fd.IsMap():
			walk(val.Message())
		}
		return true
	})
	for _, fd := range positions {
		m.Clear(fd)
	}
}

func isPosition(fd protoreflect.FieldDescriptor) bool {
	switch fd.Name() {
	case "location", "stmt_location", "stmt_len":
		return fd.Kind() == protoreflect.Int32Kind
	}
	return false
}

// normalizeTypeName drops the pg_catalog schema of built-in types. The
// printer writes INTEGER for int4 and TIMESTAMPTZ for
// pg_catalog.timestamptz; both resolve to the same pg_catalog type.
func normalizeTypeName(t *pg.TypeName) {
	if len(t.Names) != 2 {
		return
	}
	schema, name := t.Names[0].GetString_(), t.Names[1].GetString_()
	if schema == nil || name == nil || schema.Sval != "pg_catalog" || !builtins.IsType(name.Sval) {
		return
	}
	t.Names = t.Names[1:]
}

// sortColumnConstraints puts column constraints in slot order, keeping each
// attribute (DEFERRABLE...) after the constraint it belongs to.
func sortColumnConstraints(cd *pg.ColumnDef) {
	type group struct {
		slot  int
		nodes []*pg.Node
	}
	var groups []group
	for _, n := range cd.Constraints {
		c := n.GetConstraint()
		if c == nil {
			return
		}
		slot := printer.ColumnConstraintSlot(c)
		if slot < 0 && len(groups) > 0 {
			groups[len(groups)-1].nodes = append(groups[len(groups)-1].nodes, n)
			continue
		}
		groups = append(groups, group{slot: slot, nodes: []*pg.Node{n}})
	}
	slices.SortStableFunc(groups, func(a, b group) int { return a.slot - b.slot })
	cd.Constraints = cd.Constraints[:0]
	for _, g := range groups {
		cd.Constraints = append(cd.Constraints, g.nodes...)
	}
}

// sortFunctionOptions puts AS and LANGUAGE first; the printer writes the
// body and the language before the other options.
func sortFunctionOptions(f *pg.CreateFunctionStmt) {
	rank := func(n *pg.Node) int {
		switch n.GetDefElem().GetDefname() {
		case "as":
			return 0
		case "language":
			return 1
		}
		return 2
	}
	slices.SortStableFunc(f.Options, func(a, b *pg.Node) int { return rank(a) - rank(b) })
}
