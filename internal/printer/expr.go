package printer

import (
	"fmt"
	"strconv"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// Operator precedence, from the PostgreSQL manual (higher binds tighter).
const (
	precNone = iota
	precOr
	precAnd
	precNot
	precIs      // IS, ISNULL, NOTNULL
	precCmp     // < > = <= >= <>
	precIn      // BETWEEN IN LIKE ILIKE SIMILAR
	precOp      // any other operator
	precAdd     // + -
	precMul     // * / %
	precExp     // ^
	precAt      // AT TIME ZONE, AT LOCAL
	precCollate // COLLATE
	precUnary   // unary + -
	precCast    // ::
	precAtom
)

func binaryPrec(op string) int {
	switch op {
	case "^":
		return precExp
	case "*", "/", "%":
		return precMul
	case "+", "-":
		return precAdd
	case "<", ">", "=", "<=", ">=", "<>":
		return precCmp
	}
	return precOp
}

func paren(s string, prec, min int) string {
	if prec < min {
		return "(" + s + ")"
	}
	return s
}

// expr prints an expression on one line, adding parentheses when its
// precedence is lower than min.
func (p *printer) expr(n *pg.Node, min int) (string, error) {
	s, prec, err := p.exprPrec(n)
	if err != nil {
		return "", err
	}
	return paren(s, prec, min), nil
}

func (p *printer) exprList(nodes []*pg.Node) (string, error) {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		s, err := p.expr(n, precNone)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", "), nil
}

func (p *printer) exprPrec(n *pg.Node) (string, int, error) {
	if n == nil {
		return "", 0, unsupported("nil expression")
	}
	switch {
	case n.GetAConst() != nil:
		return p.aConst(n.GetAConst())
	case n.GetColumnRef() != nil:
		s, err := p.columnRef(n.GetColumnRef())
		return s, precAtom, err
	case n.GetParamRef() != nil:
		return "$" + strconv.Itoa(int(n.GetParamRef().Number)), precAtom, nil
	case n.GetAExpr() != nil:
		return p.aExpr(n.GetAExpr())
	case n.GetBoolExpr() != nil:
		return p.boolExpr(n.GetBoolExpr())
	case n.GetNullTest() != nil:
		t := n.GetNullTest()
		arg, err := p.expr(t.Arg, precIs+1)
		if err != nil {
			return "", 0, err
		}
		if t.Nulltesttype == pg.NullTestType_IS_NOT_NULL {
			return arg + " IS NOT NULL", precIs, nil
		}
		return arg + " IS NULL", precIs, nil
	case n.GetBooleanTest() != nil:
		t := n.GetBooleanTest()
		arg, err := p.expr(t.Arg, precIs+1)
		if err != nil {
			return "", 0, err
		}
		suffix := map[pg.BoolTestType]string{
			pg.BoolTestType_IS_TRUE:        " IS TRUE",
			pg.BoolTestType_IS_NOT_TRUE:    " IS NOT TRUE",
			pg.BoolTestType_IS_FALSE:       " IS FALSE",
			pg.BoolTestType_IS_NOT_FALSE:   " IS NOT FALSE",
			pg.BoolTestType_IS_UNKNOWN:     " IS UNKNOWN",
			pg.BoolTestType_IS_NOT_UNKNOWN: " IS NOT UNKNOWN",
		}[t.Booltesttype]
		if suffix == "" {
			return "", 0, unsupported("boolean test")
		}
		return arg + suffix, precIs, nil
	case n.GetTypeCast() != nil:
		t := n.GetTypeCast()
		arg, err := p.expr(t.Arg, precCast)
		if err != nil {
			return "", 0, err
		}
		typ, err := p.typeName(t.TypeName)
		if err != nil {
			return "", 0, err
		}
		return arg + "::" + typ, precCast, nil
	case n.GetCollateClause() != nil:
		c := n.GetCollateClause()
		arg, err := p.expr(c.Arg, precCollate)
		if err != nil {
			return "", 0, err
		}
		name, err := quotedNameList(c.Collname)
		if err != nil {
			return "", 0, err
		}
		return arg + " COLLATE " + name, p.restricted(precCollate), nil
	case n.GetFuncCall() != nil:
		return p.funcCall(n.GetFuncCall())
	case n.GetSqlvalueFunction() != nil:
		s, err := sqlValueFunction(n.GetSqlvalueFunction())
		return s, precAtom, err
	case n.GetCoalesceExpr() != nil:
		args, err := p.exprList(n.GetCoalesceExpr().Args)
		return "COALESCE(" + args + ")", precAtom, err
	case n.GetMinMaxExpr() != nil:
		m := n.GetMinMaxExpr()
		args, err := p.exprList(m.Args)
		if m.Op == pg.MinMaxOp_IS_LEAST {
			return "LEAST(" + args + ")", precAtom, err
		}
		return "GREATEST(" + args + ")", precAtom, err
	case n.GetAArrayExpr() != nil:
		s, err := p.arrayExpr(n.GetAArrayExpr())
		return s, precAtom, err
	case n.GetRowExpr() != nil:
		r := n.GetRowExpr()
		args, err := p.exprList(r.Args)
		if err != nil {
			return "", 0, err
		}
		if r.RowFormat == pg.CoercionForm_COERCE_IMPLICIT_CAST && len(r.Args) > 1 {
			return "(" + args + ")", precAtom, nil
		}
		return "ROW(" + args + ")", precAtom, nil
	case n.GetAIndirection() != nil:
		return p.indirection(n.GetAIndirection())
	case n.GetCaseExpr() != nil:
		s, err := p.caseFlat(n.GetCaseExpr())
		return s, precAtom, err
	case n.GetSetToDefault() != nil:
		return "DEFAULT", precAtom, nil
	case n.GetNamedArgExpr() != nil:
		a := n.GetNamedArgExpr()
		arg, err := p.expr(a.Arg, precNone)
		return quoteIdent(a.Name) + " => " + arg, precAtom, err
	}
	return "", 0, unsupported(fmt.Sprintf("expression %T", n.Node))
}

// restricted lowers the precedence of constructs that a b_expr cannot hold,
// so they get parentheses there.
func (p *printer) restricted(prec int) int {
	if p.bExpr {
		return precNone
	}
	return prec
}

func (p *printer) aConst(c *pg.A_Const) (string, int, error) {
	if c.Isnull {
		return "NULL", precAtom, nil
	}
	switch v := c.Val.(type) {
	case *pg.A_Const_Ival:
		s := strconv.Itoa(int(v.Ival.Ival))
		if v.Ival.Ival < 0 {
			return s, precUnary, nil
		}
		return s, precAtom, nil
	case *pg.A_Const_Fval:
		if strings.HasPrefix(v.Fval.Fval, "-") {
			return v.Fval.Fval, precUnary, nil
		}
		return v.Fval.Fval, precAtom, nil
	case *pg.A_Const_Boolval:
		if v.Boolval.Boolval {
			return "TRUE", precAtom, nil
		}
		return "FALSE", precAtom, nil
	case *pg.A_Const_Sval:
		return stringLiteral(v.Sval.Sval), precAtom, nil
	case *pg.A_Const_Bsval:
		bs := v.Bsval.Bsval
		if bs == "" {
			return "", 0, unsupported("empty bit string")
		}
		return strings.ToUpper(bs[:1]) + "'" + bs[1:] + "'", precAtom, nil
	}
	return "", 0, unsupported("constant")
}

// stringLiteral writes a plain '...' literal, or E'...' with escapes when
// the value holds a control character.
func stringLiteral(s string) string {
	needE := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			needE = true
			break
		}
	}
	if !needE {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	var b strings.Builder
	b.WriteString("E'")
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`''`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteString("'")
	return b.String()
}

func (p *printer) columnRef(c *pg.ColumnRef) (string, error) {
	parts := make([]string, 0, len(c.Fields))
	for i, f := range c.Fields {
		switch {
		case f.GetString_() != nil:
			name := f.GetString_().Sval
			if i == 0 && (len(c.Fields) > 1 && p.specialRecord(name) || p.plNames && plSpecialNames[name]) {
				parts = append(parts, strings.ToUpper(name))
				continue
			}
			parts = append(parts, quoteIdent(name))
		case f.GetAStar() != nil:
			parts = append(parts, "*")
		default:
			return "", unsupported("column reference")
		}
	}
	return strings.Join(parts, "."), nil
}

// specialRecord reports whether name is a trigger record (NEW/OLD) in a
// context where the style writes it in uppercase without quotes.
func (p *printer) specialRecord(name string) bool {
	switch {
	case p.triggerRecords && (name == "new" || name == "old"):
		return true
	case p.excludedRecord && name == "excluded":
		return true
	}
	return false
}

func (p *printer) aExpr(e *pg.A_Expr) (string, int, error) {
	op := ""
	if len(e.Name) > 0 {
		parts, err := stringList(e.Name)
		if err != nil {
			return "", 0, err
		}
		if len(parts) != 1 {
			return "", 0, unsupported("qualified operator")
		}
		op = parts[0]
	}

	switch e.Kind {
	case pg.A_Expr_Kind_AEXPR_OP:
		if e.Lexpr == nil {
			prec := precOp
			if op == "-" || op == "+" {
				prec = precUnary
			}
			arg, err := p.expr(e.Rexpr, prec)
			if err != nil {
				return "", 0, err
			}
			if strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "+") || prec == precOp {
				return op + " " + arg, prec, nil
			}
			return op + arg, prec, nil
		}
		if e.Rexpr == nil {
			return "", 0, unsupported("postfix operator")
		}
		prec := binaryPrec(op)
		leftMin, rightMin := prec, prec+1
		if prec == precCmp {
			leftMin = prec + 1
		}
		l, err := p.expr(e.Lexpr, leftMin)
		if err != nil {
			return "", 0, err
		}
		r, err := p.expr(e.Rexpr, rightMin)
		if err != nil {
			return "", 0, err
		}
		return l + " " + op + " " + r, prec, nil

	case pg.A_Expr_Kind_AEXPR_OP_ANY, pg.A_Expr_Kind_AEXPR_OP_ALL:
		l, err := p.expr(e.Lexpr, binaryPrec(op)+1)
		if err != nil {
			return "", 0, err
		}
		r, err := p.expr(e.Rexpr, precNone)
		if err != nil {
			return "", 0, err
		}
		word := "ANY"
		if e.Kind == pg.A_Expr_Kind_AEXPR_OP_ALL {
			word = "ALL"
		}
		return l + " " + op + " " + word + "(" + r + ")", binaryPrec(op), nil

	case pg.A_Expr_Kind_AEXPR_DISTINCT, pg.A_Expr_Kind_AEXPR_NOT_DISTINCT:
		l, err := p.expr(e.Lexpr, precIs+1)
		if err != nil {
			return "", 0, err
		}
		r, err := p.expr(e.Rexpr, precIs+1)
		if err != nil {
			return "", 0, err
		}
		if e.Kind == pg.A_Expr_Kind_AEXPR_NOT_DISTINCT {
			return l + " IS NOT DISTINCT FROM " + r, precIs, nil
		}
		return l + " IS DISTINCT FROM " + r, precIs, nil

	case pg.A_Expr_Kind_AEXPR_NULLIF:
		l, err := p.expr(e.Lexpr, precNone)
		if err != nil {
			return "", 0, err
		}
		r, err := p.expr(e.Rexpr, precNone)
		if err != nil {
			return "", 0, err
		}
		return "NULLIF(" + l + ", " + r + ")", precAtom, nil

	case pg.A_Expr_Kind_AEXPR_IN:
		l, err := p.expr(e.Lexpr, precIn+1)
		if err != nil {
			return "", 0, err
		}
		list := e.Rexpr.GetList()
		if list == nil {
			return "", 0, unsupported("IN without list")
		}
		items, err := p.exprList(list.Items)
		if err != nil {
			return "", 0, err
		}
		if op == "<>" {
			return l + " NOT IN (" + items + ")", precIn, nil
		}
		return l + " IN (" + items + ")", precIn, nil

	case pg.A_Expr_Kind_AEXPR_LIKE, pg.A_Expr_Kind_AEXPR_ILIKE, pg.A_Expr_Kind_AEXPR_SIMILAR:
		return p.likeExpr(e, op)

	case pg.A_Expr_Kind_AEXPR_BETWEEN, pg.A_Expr_Kind_AEXPR_NOT_BETWEEN,
		pg.A_Expr_Kind_AEXPR_BETWEEN_SYM, pg.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM:
		l, err := p.expr(e.Lexpr, precIn+1)
		if err != nil {
			return "", 0, err
		}
		list := e.Rexpr.GetList()
		if list == nil || len(list.Items) != 2 {
			return "", 0, unsupported("BETWEEN bounds")
		}
		lo, err := p.expr(list.Items[0], precIn+1)
		if err != nil {
			return "", 0, err
		}
		hi, err := p.expr(list.Items[1], precIn+1)
		if err != nil {
			return "", 0, err
		}
		word := map[pg.A_Expr_Kind]string{
			pg.A_Expr_Kind_AEXPR_BETWEEN:         " BETWEEN ",
			pg.A_Expr_Kind_AEXPR_NOT_BETWEEN:     " NOT BETWEEN ",
			pg.A_Expr_Kind_AEXPR_BETWEEN_SYM:     " BETWEEN SYMMETRIC ",
			pg.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM: " NOT BETWEEN SYMMETRIC ",
		}[e.Kind]
		return l + word + lo + " AND " + hi, precIn, nil
	}
	return "", 0, unsupported("operator expression")
}

func (p *printer) likeExpr(e *pg.A_Expr, op string) (string, int, error) {
	var word string
	switch op {
	case "~~":
		word = "LIKE"
	case "!~~":
		word = "NOT LIKE"
	case "~~*":
		word = "ILIKE"
	case "!~~*":
		word = "NOT ILIKE"
	case "~":
		word = "SIMILAR TO"
	case "!~":
		word = "NOT SIMILAR TO"
	default:
		return "", 0, unsupported("pattern operator")
	}
	l, err := p.expr(e.Lexpr, precIn+1)
	if err != nil {
		return "", 0, err
	}

	pattern, escape := e.Rexpr, (*pg.Node)(nil)
	if f := e.Rexpr.GetFuncCall(); f != nil {
		parts, _ := stringList(f.Funcname)
		name := strings.Join(parts, ".")
		switch {
		case name == "pg_catalog.like_escape" && len(f.Args) == 2:
			pattern, escape = f.Args[0], f.Args[1]
		case name == "pg_catalog.similar_to_escape" && len(f.Args) >= 1 && len(f.Args) <= 2:
			pattern = f.Args[0]
			if len(f.Args) == 2 {
				escape = f.Args[1]
			}
		}
	} else if e.Kind == pg.A_Expr_Kind_AEXPR_SIMILAR {
		return "", 0, unsupported("SIMILAR TO without escape function")
	}

	r, err := p.expr(pattern, precIn+1)
	if err != nil {
		return "", 0, err
	}
	s := l + " " + word + " " + r
	if escape != nil {
		esc, err := p.expr(escape, precIn+1)
		if err != nil {
			return "", 0, err
		}
		s += " ESCAPE " + esc
	}
	return s, precIn, nil
}

func (p *printer) boolExpr(b *pg.BoolExpr) (string, int, error) {
	switch b.Boolop {
	case pg.BoolExprType_NOT_EXPR:
		if len(b.Args) != 1 {
			return "", 0, unsupported("NOT arity")
		}
		arg, err := p.expr(b.Args[0], precNot)
		return "NOT " + arg, precNot, err
	case pg.BoolExprType_AND_EXPR, pg.BoolExprType_OR_EXPR:
		prec, word := precAnd, " AND "
		if b.Boolop == pg.BoolExprType_OR_EXPR {
			prec, word = precOr, " OR "
		}
		parts := make([]string, 0, len(b.Args))
		for _, a := range b.Args {
			s, err := p.expr(a, prec+1)
			if err != nil {
				return "", 0, err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, word), prec, nil
	}
	return "", 0, unsupported("boolean expression")
}

func (p *printer) arrayExpr(a *pg.A_ArrayExpr) (string, error) {
	parts := make([]string, 0, len(a.Elements))
	for _, el := range a.Elements {
		if sub := el.GetAArrayExpr(); sub != nil {
			s, err := p.arrayExpr(sub)
			if err != nil {
				return "", err
			}
			// Nested arrays drop the ARRAY keyword: ARRAY[[1, 2], [3, 4]].
			parts = append(parts, strings.TrimPrefix(s, "ARRAY"))
			continue
		}
		s, err := p.expr(el, precNone)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return "ARRAY[" + strings.Join(parts, ", ") + "]", nil
}

func (p *printer) indirection(a *pg.A_Indirection) (string, int, error) {
	base, prec, err := p.exprPrec(a.Arg)
	if err != nil {
		return "", 0, err
	}
	if a.Arg.GetColumnRef() == nil && a.Arg.GetParamRef() == nil {
		base = paren(base, prec, precAtom+1)
	}
	var b strings.Builder
	b.WriteString(base)
	for _, ind := range a.Indirection {
		switch {
		case ind.GetAIndices() != nil:
			idx := ind.GetAIndices()
			b.WriteString("[")
			if idx.Lidx != nil {
				s, err := p.expr(idx.Lidx, precNone)
				if err != nil {
					return "", 0, err
				}
				b.WriteString(s)
			}
			if idx.IsSlice {
				b.WriteString(":")
			}
			if idx.Uidx != nil {
				s, err := p.expr(idx.Uidx, precNone)
				if err != nil {
					return "", 0, err
				}
				b.WriteString(s)
			}
			b.WriteString("]")
		case ind.GetString_() != nil:
			b.WriteString("." + quoteIdent(ind.GetString_().Sval))
		case ind.GetAStar() != nil:
			b.WriteString(".*")
		default:
			return "", 0, unsupported("indirection")
		}
	}
	return b.String(), precAtom, nil
}

func sqlValueFunction(f *pg.SQLValueFunction) (string, error) {
	withTypmod := func(name string) string {
		if f.Typmod >= 0 {
			return fmt.Sprintf("%s(%d)", name, f.Typmod)
		}
		return name
	}
	switch f.Op {
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_DATE:
		return "CURRENT_DATE", nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_TIME:
		return "CURRENT_TIME", nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_TIME_N:
		return withTypmod("CURRENT_TIME"), nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP:
		return "CURRENT_TIMESTAMP", nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP_N:
		return withTypmod("CURRENT_TIMESTAMP"), nil
	case pg.SQLValueFunctionOp_SVFOP_LOCALTIME:
		return "LOCALTIME", nil
	case pg.SQLValueFunctionOp_SVFOP_LOCALTIME_N:
		return withTypmod("LOCALTIME"), nil
	case pg.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP:
		return "LOCALTIMESTAMP", nil
	case pg.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP_N:
		return withTypmod("LOCALTIMESTAMP"), nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_ROLE:
		return "CURRENT_ROLE", nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_USER:
		return "CURRENT_USER", nil
	case pg.SQLValueFunctionOp_SVFOP_USER:
		return "USER", nil
	case pg.SQLValueFunctionOp_SVFOP_SESSION_USER:
		return "SESSION_USER", nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_CATALOG:
		return "CURRENT_CATALOG", nil
	case pg.SQLValueFunctionOp_SVFOP_CURRENT_SCHEMA:
		return "CURRENT_SCHEMA", nil
	}
	return "", unsupported("SQL value function")
}

// caseFlat prints a CASE expression on one line, for places that cannot
// break (grid cells).
func (p *printer) caseFlat(c *pg.CaseExpr) (string, error) {
	var b strings.Builder
	b.WriteString("CASE")
	if c.Arg != nil {
		arg, err := p.expr(c.Arg, precNone)
		if err != nil {
			return "", err
		}
		b.WriteString(" " + arg)
	}
	for _, w := range c.Args {
		cw := w.GetCaseWhen()
		if cw == nil {
			return "", unsupported("CASE branch")
		}
		cond, err := p.expr(cw.Expr, precNone)
		if err != nil {
			return "", err
		}
		res, err := p.expr(cw.Result, precNone)
		if err != nil {
			return "", err
		}
		b.WriteString(" WHEN " + cond + " THEN " + res)
	}
	if c.Defresult != nil {
		d, err := p.expr(c.Defresult, precNone)
		if err != nil {
			return "", err
		}
		b.WriteString(" ELSE " + d)
	}
	b.WriteString(" END")
	return b.String(), nil
}
