package printer

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// maxWidth is the line width past which expressions wrap.
const maxWidth = 120

func spaces(n int) string {
	return strings.Repeat(" ", max(n, 0))
}

// forcesBreak reports whether n holds a construct that always spans several
// lines (a subquery or a CASE expression).
func forcesBreak(n *pg.Node) bool {
	if n == nil {
		return false
	}
	found := false
	var visit func(m protoreflect.Message)
	visit = func(m protoreflect.Message) {
		if found {
			return
		}
		switch m.Interface().(type) {
		case *pg.SubLink, *pg.CaseExpr:
			found = true
			return
		}
		m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			switch {
			case fd.IsList() && fd.Message() != nil:
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					visit(l.Get(i).Message())
				}
			case fd.Message() != nil && !fd.IsMap():
				visit(v.Message())
			}
			return !found
		})
	}
	visit(n.ProtoReflect())
	return found
}

// joinLines appends more to lines, continuing the last line of lines with
// the first line of more.
func joinLines(lines []string, more []string) []string {
	if len(more) == 0 {
		return lines
	}
	if len(lines) == 0 {
		return append([]string(nil), more...)
	}
	out := append([]string(nil), lines...)
	out[len(out)-1] += more[0]
	return append(out, more[1:]...)
}

// lastCol returns the column where text appended to the last line would
// start, given that the first line starts at col.
func lastCol(lines []string, col int) int {
	if len(lines) == 1 {
		return col + width(lines[0])
	}
	return width(lines[len(lines)-1])
}

// layout prints an expression that starts at column col. The first line has
// no indentation (it continues the current line); later lines carry their
// absolute indentation. Blocks and continuations indent from indent, the
// column where the content of the current line starts.
func (p *printer) layout(n *pg.Node, col, indent, min int) ([]string, error) {
	if !forcesBreak(n) {
		flat, err := p.expr(n, min)
		if err != nil {
			return nil, err
		}
		if col+width(flat) <= maxWidth {
			return []string{flat}, nil
		}
	}
	lines, prec, err := p.layoutPrec(n, col, indent)
	if err != nil {
		return nil, err
	}
	if prec < min {
		lines = joinLines([]string{"("}, lines)
		lines[len(lines)-1] += ")"
	}
	return lines, nil
}

func (p *printer) layoutPrec(n *pg.Node, col, indent int) ([]string, int, error) {
	switch {
	case n.GetCaseExpr() != nil:
		lines, err := p.caseLines(n.GetCaseExpr(), col)
		return lines, precAtom, err
	case n.GetSubLink() != nil:
		return p.subLinkLines(n.GetSubLink(), col, indent)
	case n.GetBoolExpr() != nil:
		b := n.GetBoolExpr()
		if b.Boolop == pg.BoolExprType_NOT_EXPR && len(b.Args) == 1 {
			if s := b.Args[0].GetSubLink(); s != nil && s.SubLinkType == pg.SubLinkType_ANY_SUBLINK &&
				len(s.OperName) == 0 {
				return p.subLinkLines(s, col, indent, "NOT IN")
			}
			arg, err := p.layout(b.Args[0], col+4, indent, precNot)
			return joinLines([]string{"NOT "}, arg), precNot, err
		}
		prec, word := precAnd, "AND"
		if b.Boolop == pg.BoolExprType_OR_EXPR {
			prec, word = precOr, "OR"
		}
		ops := make([]string, len(b.Args))
		for i := range ops {
			ops[i] = word
		}
		lines, err := p.chain(b.Args, ops, prec+1, prec+1, col, indent)
		return lines, prec, err
	case n.GetAExpr() != nil:
		e := n.GetAExpr()
		if e.Kind == pg.A_Expr_Kind_AEXPR_OP && e.Lexpr != nil && e.Rexpr != nil {
			operands, ops, err := flattenChain(e)
			if err != nil {
				return nil, 0, err
			}
			prec := binaryPrec(ops[0])
			leftMin := prec
			if prec == precCmp {
				leftMin = prec + 1
			}
			lines, err := p.chain(operands, append([]string{""}, ops...), leftMin, prec+1, col, indent)
			return lines, prec, err
		}
		if e.Kind == pg.A_Expr_Kind_AEXPR_IN {
			return p.inListLines(e, col, indent)
		}
	case n.GetFuncCall() != nil:
		f := n.GetFuncCall()
		if f.Funcformat != pg.CoercionForm_COERCE_SQL_SYNTAX && !f.AggStar && !f.AggDistinct &&
			!f.FuncVariadic && len(f.AggOrder) == 0 && f.AggFilter == nil && f.Over == nil && len(f.Args) > 0 {
			name, err := funcName(f.Funcname)
			if err != nil {
				return nil, 0, err
			}
			block, err := p.listBlock(f.Args, indent)
			if err != nil {
				return nil, 0, err
			}
			return joinLines([]string{name + "("}, block), precAtom, nil
		}
	case n.GetTypeCast() != nil:
		t := n.GetTypeCast()
		typ, err := p.typeName(t.TypeName)
		if err != nil {
			return nil, 0, err
		}
		arg, err := p.layout(t.Arg, col, indent, precCast)
		if err != nil {
			return nil, 0, err
		}
		arg[len(arg)-1] += "::" + typ
		return arg, precCast, nil
	case n.GetNullTest() != nil:
		t := n.GetNullTest()
		arg, err := p.layout(t.Arg, col, indent, precIs+1)
		if err != nil {
			return nil, 0, err
		}
		if t.Nulltesttype == pg.NullTestType_IS_NOT_NULL {
			arg[len(arg)-1] += " IS NOT NULL"
		} else {
			arg[len(arg)-1] += " IS NULL"
		}
		return arg, precIs, nil
	}
	s, prec, err := p.exprPrec(n)
	return []string{s}, prec, err
}

// flattenChain collects a left-deep chain of binary operators with the same
// precedence: a + b - c gives [a b c] and [+ -].
func flattenChain(e *pg.A_Expr) ([]*pg.Node, []string, error) {
	parts, err := stringList(e.Name)
	if err != nil || len(parts) != 1 {
		return nil, nil, unsupported("operator")
	}
	op := parts[0]
	prec := binaryPrec(op)
	var operands []*pg.Node
	var ops []string
	if l := e.Lexpr.GetAExpr(); l != nil && l.Kind == pg.A_Expr_Kind_AEXPR_OP && l.Lexpr != nil && l.Rexpr != nil &&
		prec != precCmp {
		if lp, err := stringList(l.Name); err == nil && len(lp) == 1 && binaryPrec(lp[0]) == prec {
			operands, ops, err = flattenChain(l)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if operands == nil {
		operands = []*pg.Node{e.Lexpr}
	}
	return append(operands, e.Rexpr), append(ops, op), nil
}

// chain prints operands joined by operators, filling lines and breaking
// before an operator; continuation lines start at indent+4. ops[i] is the
// operator before operands[i] (ops[0] is ignored).
func (p *printer) chain(operands []*pg.Node, ops []string, firstMin, restMin, col, indent int) ([]string, error) {
	cont := indent + 4
	lines, err := p.layout(operands[0], col, indent, firstMin)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(operands); i++ {
		at := lastCol(lines, col)
		piece, err := p.layout(operands[i], at+len(ops[i])+2, cont, restMin)
		if err != nil {
			return nil, err
		}
		piece = joinLines([]string{ops[i] + " "}, piece)
		if len(piece) == 1 && at+1+width(piece[0]) <= maxWidth {
			lines[len(lines)-1] += " " + piece[0]
			continue
		}
		piece, err = p.layout(operands[i], cont+len(ops[i])+1, cont, restMin)
		if err != nil {
			return nil, err
		}
		piece = joinLines([]string{spaces(cont) + ops[i] + " "}, piece)
		lines = append(lines, piece...)
	}
	return lines, nil
}

// listBlock prints "(" items ")" with the items on their own lines, filled
// up to the width, at indent+4, and ")" at indent. The first line is "".
func (p *printer) listBlock(items []*pg.Node, indent int) ([]string, error) {
	inner := indent + 4
	var rendered [][]string
	multi := false
	for _, it := range items {
		l, err := p.layout(it, inner, inner, precNone)
		if err != nil {
			return nil, err
		}
		if len(l) > 1 {
			multi = true
		}
		rendered = append(rendered, l)
	}
	lines := []string{""}
	cur := ""
	for i, l := range rendered {
		sep := ","
		if i == len(rendered)-1 {
			sep = ""
		}
		if multi {
			l = joinLines([]string{spaces(inner)}, l)
			l[len(l)-1] += sep
			lines = append(lines, l...)
			continue
		}
		piece := l[0] + sep
		switch {
		case cur == "":
			cur = spaces(inner) + piece
		case width(cur)+1+width(piece) <= maxWidth:
			cur += " " + piece
		default:
			lines = append(lines, cur)
			cur = spaces(inner) + piece
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return append(lines, spaces(indent)+")"), nil
}

func (p *printer) inListLines(e *pg.A_Expr, col, indent int) ([]string, int, error) {
	list := e.Rexpr.GetList()
	if list == nil {
		return nil, 0, unsupported("IN without list")
	}
	parts, err := stringList(e.Name)
	if err != nil || len(parts) != 1 {
		return nil, 0, unsupported("IN operator")
	}
	word := " IN ("
	if parts[0] == "<>" {
		word = " NOT IN ("
	}
	l, err := p.layout(e.Lexpr, col, indent, precIn+1)
	if err != nil {
		return nil, 0, err
	}
	block, err := p.listBlock(list.Items, indent)
	if err != nil {
		return nil, 0, err
	}
	l[len(l)-1] += strings.TrimSuffix(word, "(")
	return joinLines(l, joinLines([]string{"("}, block)), precIn, nil
}

func (p *printer) caseLines(c *pg.CaseExpr, col int) ([]string, error) {
	head := "CASE"
	if c.Arg != nil {
		arg, err := p.expr(c.Arg, precNone)
		if err != nil {
			return nil, err
		}
		head += " " + arg
	}
	inner := col + 4
	var rows []gridRow
	for _, w := range c.Args {
		cw := w.GetCaseWhen()
		if cw == nil {
			return nil, unsupported("CASE branch")
		}
		cond, err := p.expr(cw.Expr, precNone)
		if err != nil {
			return nil, err
		}
		res, err := p.expr(cw.Result, precNone)
		if err != nil {
			return nil, err
		}
		rows = append(rows, gridRow{cells: []string{"WHEN " + cond, "THEN " + res}})
	}
	lines := []string{head}
	lines = append(lines, renderGrid(inner, rows)...)
	if c.Defresult != nil {
		d, err := p.expr(c.Defresult, precNone)
		if err != nil {
			return nil, err
		}
		lines = append(lines, spaces(inner)+"ELSE "+d)
	}
	return append(lines, spaces(col)+"END"), nil
}

// subLinkLines prints a subquery expression: the subquery gets its own
// river at indent+4 and ")" sits at indent.
func (p *printer) subLinkLines(s *pg.SubLink, col, indent int, notIn ...string) ([]string, int, error) {
	var head []string
	prec := precAtom
	open := "("
	test := func(min int) error {
		l, err := p.layout(s.Testexpr, col, indent, min)
		head = l
		return err
	}
	switch s.SubLinkType {
	case pg.SubLinkType_EXISTS_SUBLINK:
		head = []string{"EXISTS "}
	case pg.SubLinkType_EXPR_SUBLINK:
		head = []string{""}
	case pg.SubLinkType_ARRAY_SUBLINK:
		head = []string{"ARRAY"}
	case pg.SubLinkType_ANY_SUBLINK, pg.SubLinkType_ALL_SUBLINK:
		if len(s.OperName) == 0 {
			prec = precIn
			if err := test(precIn + 1); err != nil {
				return nil, 0, err
			}
			word := " IN "
			if len(notIn) > 0 {
				word = " NOT IN "
			}
			head[len(head)-1] += word
			break
		}
		ops, err := stringList(s.OperName)
		if err != nil || len(ops) != 1 {
			return nil, 0, unsupported("subquery operator")
		}
		prec = binaryPrec(ops[0])
		if err := test(prec + 1); err != nil {
			return nil, 0, err
		}
		word := " ANY "
		if s.SubLinkType == pg.SubLinkType_ALL_SUBLINK {
			word = " ALL "
		}
		head[len(head)-1] += " " + ops[0] + word
	default:
		return nil, 0, unsupported("subquery kind")
	}
	body, err := p.stmtLines(s.Subselect, indent+4)
	if err != nil {
		return nil, 0, err
	}
	lines := joinLines(head, []string{open})
	lines = append(lines, body...)
	return append(lines, spaces(indent)+")"), prec, nil
}
