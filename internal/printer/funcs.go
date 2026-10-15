package printer

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

func (p *printer) funcCall(f *pg.FuncCall) (string, int, error) {
	if f.Funcformat == pg.CoercionForm_COERCE_SQL_SYNTAX {
		return p.sqlSyntaxCall(f)
	}
	name, err := funcName(f.Funcname)
	if err != nil {
		return "", 0, err
	}

	var b strings.Builder
	b.WriteString(name)
	b.WriteString("(")
	switch {
	case f.AggStar:
		b.WriteString("*")
	default:
		if f.AggDistinct {
			b.WriteString("DISTINCT ")
		}
		for i, a := range f.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			if f.FuncVariadic && i == len(f.Args)-1 {
				b.WriteString("VARIADIC ")
			}
			s, err := p.expr(a, precNone)
			if err != nil {
				return "", 0, err
			}
			b.WriteString(s)
		}
	}
	var order string
	if len(f.AggOrder) > 0 {
		var err error
		if order, err = p.sortList(f.AggOrder); err != nil {
			return "", 0, err
		}
		if !f.AggWithinGroup {
			b.WriteString(" ORDER BY " + order)
		}
	}
	b.WriteString(")")
	if f.AggWithinGroup {
		b.WriteString(" WITHIN GROUP (ORDER BY " + order + ")")
	}
	if f.AggFilter != nil {
		cond, err := p.expr(f.AggFilter, precNone)
		if err != nil {
			return "", 0, err
		}
		b.WriteString(" FILTER (WHERE " + cond + ")")
	}
	if f.Over != nil {
		over, err := p.windowDef(f.Over)
		if err != nil {
			return "", 0, err
		}
		b.WriteString(" OVER " + over)
	}
	return b.String(), precAtom, nil
}

func (p *printer) sortList(nodes []*pg.Node) (string, error) {
	parts, err := p.sortItems(nodes)
	return strings.Join(parts, ", "), err
}

func (p *printer) sortItems(nodes []*pg.Node) ([]string, error) {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		sb := n.GetSortBy()
		if sb == nil {
			return nil, unsupported("sort item")
		}
		s, err := p.expr(sb.Node, precNone)
		if err != nil {
			return nil, err
		}
		switch sb.SortbyDir {
		case pg.SortByDir_SORTBY_ASC:
			s += " ASC"
		case pg.SortByDir_SORTBY_DESC:
			s += " DESC"
		case pg.SortByDir_SORTBY_USING:
			return nil, unsupported("ORDER BY USING")
		}
		switch sb.SortbyNulls {
		case pg.SortByNulls_SORTBY_NULLS_FIRST:
			s += " NULLS FIRST"
		case pg.SortByNulls_SORTBY_NULLS_LAST:
			s += " NULLS LAST"
		}
		parts = append(parts, s)
	}
	return parts, nil
}

// sqlSyntaxCall prints functions that the grammar builds from special SQL
// syntax (AT TIME ZONE, EXTRACT, POSITION...). The parser marks them with
// COERCE_SQL_SYNTAX and stores them under pg_catalog names.
func (p *printer) sqlSyntaxCall(f *pg.FuncCall) (string, int, error) {
	parts, err := stringList(f.Funcname)
	if err != nil {
		return "", 0, err
	}
	if len(parts) != 2 || parts[0] != "pg_catalog" {
		return "", 0, unsupported("SQL syntax function")
	}
	args := f.Args
	arg := func(i, min int) (string, error) { return p.expr(args[i], min) }

	switch name := parts[1]; {
	case name == "timezone" && len(args) == 2:
		e, err := arg(1, precAt)
		if err != nil {
			return "", 0, err
		}
		z, err := arg(0, precAt+1)
		if err != nil {
			return "", 0, err
		}
		return e + " AT TIME ZONE " + z, p.restricted(precAt), nil

	case name == "timezone" && len(args) == 1:
		e, err := arg(0, precAt)
		if err != nil {
			return "", 0, err
		}
		return e + " AT LOCAL", p.restricted(precAt), nil

	case name == "extract" && len(args) == 2:
		field := args[0].GetAConst().GetSval()
		if field == nil {
			return "", 0, unsupported("EXTRACT field")
		}
		src, err := arg(1, precNone)
		if err != nil {
			return "", 0, err
		}
		fieldText := stringLiteral(field.Sval)
		if bareWordOK(field.Sval) {
			fieldText = strings.ToUpper(field.Sval)
		}
		return "EXTRACT(" + fieldText + " FROM " + src + ")", precAtom, nil

	case name == "position" && len(args) == 2:
		haystack, err := arg(0, precNone)
		if err != nil {
			return "", 0, err
		}
		needle, err := arg(1, precIn+1)
		if err != nil {
			return "", 0, err
		}
		return "POSITION(" + needle + " IN " + haystack + ")", precAtom, nil

	case name == "substring" && (len(args) == 2 || len(args) == 3):
		s, err := arg(0, precNone)
		if err != nil {
			return "", 0, err
		}
		from, err := arg(1, precNone)
		if err != nil {
			return "", 0, err
		}
		out := "SUBSTRING(" + s + " FROM " + from
		if len(args) == 3 {
			n, err := arg(2, precNone)
			if err != nil {
				return "", 0, err
			}
			out += " FOR " + n
		}
		return out + ")", precAtom, nil

	case (name == "btrim" || name == "ltrim" || name == "rtrim") && (len(args) == 1 || len(args) == 2):
		where := map[string]string{"btrim": "BOTH", "ltrim": "LEADING", "rtrim": "TRAILING"}[name]
		s, err := arg(0, precNone)
		if err != nil {
			return "", 0, err
		}
		if len(args) == 1 {
			return "TRIM(" + where + " FROM " + s + ")", precAtom, nil
		}
		chars, err := arg(1, precNone)
		if err != nil {
			return "", 0, err
		}
		return "TRIM(" + where + " " + chars + " FROM " + s + ")", precAtom, nil

	case name == "overlay" && (len(args) == 3 || len(args) == 4):
		var ps [4]string
		for i := range args {
			if ps[i], err = arg(i, precNone); err != nil {
				return "", 0, err
			}
		}
		out := "OVERLAY(" + ps[0] + " PLACING " + ps[1] + " FROM " + ps[2]
		if len(args) == 4 {
			out += " FOR " + ps[3]
		}
		return out + ")", precAtom, nil

	case name == "overlaps" && len(args) == 4:
		var ps [4]string
		for i := range args {
			if ps[i], err = arg(i, precNone); err != nil {
				return "", 0, err
			}
		}
		return "(" + ps[0] + ", " + ps[1] + ") OVERLAPS (" + ps[2] + ", " + ps[3] + ")", precCmp, nil

	case name == "pg_collation_for" && len(args) == 1:
		s, err := arg(0, precNone)
		if err != nil {
			return "", 0, err
		}
		return "COLLATION FOR (" + s + ")", precAtom, nil
	}
	return "", 0, unsupported("SQL syntax function " + strings.Join(parts, "."))
}

// Window frame option bits, from PostgreSQL's parsenodes.h.
const (
	frameNonDefault        = 0x00001
	frameRange             = 0x00002
	frameRows              = 0x00004
	frameGroups            = 0x00008
	frameBetween           = 0x00010
	frameStartUnboundedPre = 0x00020
	frameEndUnboundedPre   = 0x00040
	frameStartUnboundedFol = 0x00080
	frameEndUnboundedFol   = 0x00100
	frameStartCurrentRow   = 0x00200
	frameEndCurrentRow     = 0x00400
	frameStartOffsetPre    = 0x00800
	frameEndOffsetPre      = 0x01000
	frameStartOffsetFol    = 0x02000
	frameEndOffsetFol      = 0x04000
	frameExcludeCurrentRow = 0x08000
	frameExcludeGroup      = 0x10000
	frameExcludeTies       = 0x20000
)

// windowDef prints a window: a name, or a parenthesized specification.
func (p *printer) windowDef(w *pg.WindowDef) (string, error) {
	if w.Name != "" {
		return quoteIdent(w.Name), nil
	}
	var parts []string
	if w.Refname != "" {
		parts = append(parts, quoteIdent(w.Refname))
	}
	if len(w.PartitionClause) > 0 {
		list, err := p.exprList(w.PartitionClause)
		if err != nil {
			return "", err
		}
		parts = append(parts, "PARTITION BY "+list)
	}
	if len(w.OrderClause) > 0 {
		order, err := p.sortList(w.OrderClause)
		if err != nil {
			return "", err
		}
		parts = append(parts, "ORDER BY "+order)
	}
	if w.FrameOptions&frameNonDefault != 0 {
		frame, err := p.frame(w)
		if err != nil {
			return "", err
		}
		parts = append(parts, frame)
	}
	return "(" + strings.Join(parts, " ") + ")", nil
}

func (p *printer) frame(w *pg.WindowDef) (string, error) {
	o := w.FrameOptions
	var mode string
	switch {
	case o&frameRange != 0:
		mode = "RANGE"
	case o&frameRows != 0:
		mode = "ROWS"
	case o&frameGroups != 0:
		mode = "GROUPS"
	default:
		return "", unsupported("frame mode")
	}
	bound := func(unboundedPre, unboundedFol, current, offsetPre, offsetFol int32, offset *pg.Node) (string, error) {
		switch {
		case o&unboundedPre != 0:
			return "UNBOUNDED PRECEDING", nil
		case o&unboundedFol != 0:
			return "UNBOUNDED FOLLOWING", nil
		case o&current != 0:
			return "CURRENT ROW", nil
		case o&(offsetPre|offsetFol) != 0:
			e, err := p.expr(offset, precNone)
			if err != nil {
				return "", err
			}
			if o&offsetPre != 0 {
				return e + " PRECEDING", nil
			}
			return e + " FOLLOWING", nil
		}
		return "", unsupported("frame bound")
	}
	start, err := bound(frameStartUnboundedPre, frameStartUnboundedFol, frameStartCurrentRow, frameStartOffsetPre, frameStartOffsetFol, w.StartOffset)
	if err != nil {
		return "", err
	}
	s := mode + " " + start
	if o&frameBetween != 0 {
		end, err := bound(frameEndUnboundedPre, frameEndUnboundedFol, frameEndCurrentRow, frameEndOffsetPre, frameEndOffsetFol, w.EndOffset)
		if err != nil {
			return "", err
		}
		s = mode + " BETWEEN " + start + " AND " + end
	}
	switch {
	case o&frameExcludeCurrentRow != 0:
		s += " EXCLUDE CURRENT ROW"
	case o&frameExcludeGroup != 0:
		s += " EXCLUDE GROUP"
	case o&frameExcludeTies != 0:
		s += " EXCLUDE TIES"
	}
	return s, nil
}
