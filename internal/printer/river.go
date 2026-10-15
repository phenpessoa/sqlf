package printer

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// riverWidth is the width of the river: clause keywords right-align to the
// end of SELECT.
const riverWidth = 6

// riverKW places a clause keyword for a river that starts at base. Only the
// first word right-aligns; a word longer than the river starts at base.
func riverKW(base int, kw string) string {
	first, _, _ := strings.Cut(kw, " ")
	return spaces(max(base+riverWidth-len(first), base)) + kw
}

// contentCol returns the column where the content of a clause starts.
func contentCol(base int, kw string) int {
	return width(riverKW(base, kw)) + 1
}

// clause puts content after a river keyword.
func clause(base int, kw string, content []string) []string {
	return joinLines([]string{riverKW(base, kw) + " "}, content)
}

// stmtLines prints a query statement whose river starts at base. Lines carry
// their absolute indentation; the last line has no semicolon.
func (p *printer) stmtLines(n *pg.Node, base int) ([]string, error) {
	switch {
	case n.GetSelectStmt() != nil:
		return p.selectLines(n.GetSelectStmt(), base)
	case n.GetInsertStmt() != nil:
		return p.insertLines(n.GetInsertStmt(), base)
	case n.GetUpdateStmt() != nil:
		return p.updateLines(n.GetUpdateStmt(), base)
	case n.GetDeleteStmt() != nil:
		return p.deleteLines(n.GetDeleteStmt(), base)
	}
	return nil, unsupported("nested statement")
}

func (p *printer) withLines(w *pg.WithClause, base int) ([]string, error) {
	if w == nil {
		return nil, nil
	}
	var lines []string
	for i, n := range w.Ctes {
		cte := n.GetCommonTableExpr()
		if cte == nil || cte.SearchClause != nil || cte.CycleClause != nil {
			return nil, unsupported("CTE")
		}
		head := quoteIdent(cte.Ctename)
		if len(cte.Aliascolnames) > 0 {
			names, err := stringList(cte.Aliascolnames)
			if err != nil {
				return nil, err
			}
			head += "(" + quoteQualifiedEach(names) + ")"
		}
		head += " AS "
		switch cte.Ctematerialized {
		case pg.CTEMaterialize_CTEMaterializeAlways:
			head += "MATERIALIZED "
		case pg.CTEMaterialize_CTEMaterializeNever:
			head += "NOT MATERIALIZED "
		}
		if i == 0 {
			kw := "WITH "
			if w.Recursive {
				kw = "WITH RECURSIVE "
			}
			head = kw + head
		}
		body, err := p.stmtLines(cte.Ctequery, base+4)
		if err != nil {
			return nil, err
		}
		lines = append(lines, spaces(base)+head+"(")
		lines = append(lines, body...)
		end := spaces(base) + ")"
		if i < len(w.Ctes)-1 {
			end += ","
		}
		lines = append(lines, end)
	}
	return lines, nil
}

func (p *printer) selectLines(s *pg.SelectStmt, base int) ([]string, error) {
	lines, err := p.withLines(s.WithClause, base)
	if err != nil {
		return nil, err
	}
	body, err := p.selectBody(s, base)
	if err != nil {
		return nil, err
	}
	lines = append(lines, body...)

	if len(s.SortClause) > 0 {
		order, err := p.sortItems(s.SortClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, p.fillList(base, "ORDER BY", order)...)
	}
	switch s.LimitOption {
	case pg.LimitOption_LIMIT_OPTION_DEFAULT, pg.LimitOption_LIMIT_OPTION_COUNT:
	default:
		return nil, unsupported("FETCH WITH TIES")
	}
	if s.LimitCount != nil {
		text := "ALL"
		if c := s.LimitCount.GetAConst(); c == nil || !c.Isnull {
			if text, err = p.expr(s.LimitCount, precNone); err != nil {
				return nil, err
			}
		}
		lines = append(lines, riverKW(base, "LIMIT")+" "+text)
	}
	if s.LimitOffset != nil {
		off, err := p.expr(s.LimitOffset, precNone)
		if err != nil {
			return nil, err
		}
		lines = append(lines, riverKW(base, "OFFSET")+" "+off)
	}
	for _, n := range s.LockingClause {
		lc := n.GetLockingClause()
		if lc == nil {
			return nil, unsupported("locking clause")
		}
		strength := map[pg.LockClauseStrength]string{
			pg.LockClauseStrength_LCS_FORKEYSHARE:    "FOR KEY SHARE",
			pg.LockClauseStrength_LCS_FORSHARE:       "FOR SHARE",
			pg.LockClauseStrength_LCS_FORNOKEYUPDATE: "FOR NO KEY UPDATE",
			pg.LockClauseStrength_LCS_FORUPDATE:      "FOR UPDATE",
		}[lc.Strength]
		if strength == "" {
			return nil, unsupported("lock strength")
		}
		line := riverKW(base, strength)
		if len(lc.LockedRels) > 0 {
			var rels []string
			for _, r := range lc.LockedRels {
				rv := r.GetRangeVar()
				if rv == nil {
					return nil, unsupported("locked relation")
				}
				rels = append(rels, rangeVar(rv))
			}
			line += " OF " + strings.Join(rels, ", ")
		}
		switch lc.WaitPolicy {
		case pg.LockWaitPolicy_LockWaitSkip:
			line += " SKIP LOCKED"
		case pg.LockWaitPolicy_LockWaitError:
			line += " NOWAIT"
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// selectBody prints a SELECT without its WITH, ORDER BY and LIMIT parts:
// a set operation, a VALUES list, or a plain SELECT.
func (p *printer) selectBody(s *pg.SelectStmt, base int) ([]string, error) {
	if s.Op != pg.SetOperation_SETOP_NONE {
		return p.setOpLines(s, base)
	}
	if len(s.ValuesLists) > 0 {
		return p.valuesLines(s.ValuesLists, base)
	}
	if s.IntoClause != nil || len(s.WindowClause) > 0 {
		return nil, unsupported("SELECT INTO or WINDOW")
	}

	kw := "SELECT"
	if len(s.DistinctClause) > 0 {
		kw = "SELECT DISTINCT"
		// DISTINCT alone is stored as a list holding one nil node.
		if len(s.DistinctClause) != 1 || s.DistinctClause[0].Node != nil {
			on, err := p.exprList(s.DistinctClause)
			if err != nil {
				return nil, err
			}
			kw += " ON (" + on + ")"
		}
	}
	lines, err := p.targetLines(base, kw, s.TargetList)
	if err != nil {
		return nil, err
	}
	if len(s.FromClause) > 0 {
		from, err := p.fromLines(base, "FROM", s.FromClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, from...)
	}
	if s.WhereClause != nil {
		w, err := p.condLines(base, "WHERE", s.WhereClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, w...)
	}
	if len(s.GroupClause) > 0 {
		kw := "GROUP BY"
		if s.GroupDistinct {
			kw = "GROUP BY DISTINCT"
		}
		var items []string
		for _, g := range s.GroupClause {
			e, err := p.expr(g, precNone)
			if err != nil {
				return nil, err
			}
			items = append(items, e)
		}
		lines = append(lines, p.fillList(base, kw, items)...)
	}
	if s.HavingClause != nil {
		h, err := p.condLines(base, "HAVING", s.HavingClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, h...)
	}
	return lines, nil
}

// fillList prints a clause whose items stay inline, wrapping past the width
// with continuation lines aligned to the first item.
func (p *printer) fillList(base int, kw string, items []string) []string {
	col := contentCol(base, kw)
	cur := riverKW(base, kw) + " "
	var lines []string
	for i, it := range items {
		if i < len(items)-1 {
			it += ","
		}
		switch {
		case i == 0:
			cur += it
		case width(cur)+1+width(it) <= maxWidth:
			cur += " " + it
		default:
			lines = append(lines, cur)
			cur = spaces(col) + it
		}
	}
	return append(lines, cur)
}

func (p *printer) setOpLines(s *pg.SelectStmt, base int) ([]string, error) {
	var parts []*pg.SelectStmt
	var ops []string
	var collect func(s *pg.SelectStmt) error
	collect = func(s *pg.SelectStmt) error {
		if s.Op == pg.SetOperation_SETOP_NONE {
			if len(s.SortClause) > 0 || s.LimitCount != nil || s.LimitOffset != nil || s.WithClause != nil ||
				len(s.LockingClause) > 0 {
				return unsupported("parenthesized set operand")
			}
			parts = append(parts, s)
			return nil
		}
		if s.Rarg.Op != pg.SetOperation_SETOP_NONE {
			return unsupported("right-nested set operation")
		}
		if err := collect(s.Larg); err != nil {
			return err
		}
		op := map[pg.SetOperation]string{
			pg.SetOperation_SETOP_UNION:     "UNION",
			pg.SetOperation_SETOP_INTERSECT: "INTERSECT",
			pg.SetOperation_SETOP_EXCEPT:    "EXCEPT",
		}[s.Op]
		if s.All {
			op += " ALL"
		}
		ops = append(ops, op)
		return collect(s.Rarg)
	}
	if err := collect(&pg.SelectStmt{Op: s.Op, All: s.All, Larg: s.Larg, Rarg: s.Rarg}); err != nil {
		return nil, err
	}
	var lines []string
	for i, part := range parts {
		if i > 0 {
			lines = append(lines, spaces(base)+ops[i-1])
		}
		body, err := p.selectBody(part, base)
		if err != nil {
			return nil, err
		}
		lines = append(lines, body...)
	}
	return lines, nil
}

func (p *printer) valuesLines(rows []*pg.Node, base int) ([]string, error) {
	col := contentCol(base, "VALUES")
	var lines []string
	for i, r := range rows {
		list := r.GetList()
		if list == nil {
			return nil, unsupported("VALUES row")
		}
		row, err := p.rowLines(list.Items, col)
		if err != nil {
			return nil, err
		}
		if i < len(rows)-1 {
			row[len(row)-1] += ","
		}
		if i == 0 {
			lines = clause(base, "VALUES", row)
		} else {
			lines = append(lines, joinLines([]string{spaces(col)}, row)...)
		}
	}
	return lines, nil
}

// rowLines prints "(a, b, ...)", breaking into a filled block past the width.
func (p *printer) rowLines(items []*pg.Node, col int) ([]string, error) {
	flat, err := p.exprList(items)
	if err == nil && col+width(flat)+2 <= maxWidth {
		hasBreak := false
		for _, it := range items {
			hasBreak = hasBreak || forcesBreak(it)
		}
		if !hasBreak {
			return []string{"(" + flat + ")"}, nil
		}
	}
	block, err := p.listBlock(items, col)
	if err != nil {
		return nil, err
	}
	return joinLines([]string{"("}, block), nil
}

// targetLines prints a SELECT list: one item per line, with the AS of
// single-line items aligned on the tab-stop grid.
func (p *printer) targetLines(base int, kw string, targets []*pg.Node) ([]string, error) {
	first := contentCol(base, kw)
	col := contentCol(base, "SELECT")
	type item struct {
		lines []string
		alias string
		start int
	}
	items := make([]item, 0, len(targets))
	for i, t := range targets {
		rt := t.GetResTarget()
		if rt == nil || len(rt.Indirection) > 0 {
			return nil, unsupported("select item")
		}
		start := col
		if i == 0 {
			start = first
		}
		l, err := p.layout(rt.Val, start, col, precNone)
		if err != nil {
			return nil, err
		}
		alias := ""
		if rt.Name != "" {
			alias = "AS " + quoteIdent(rt.Name)
		}
		items = append(items, item{lines: l, alias: alias, start: start})
	}
	asCol := 0
	for _, it := range items {
		if it.alias != "" && len(it.lines) == 1 {
			asCol = max(asCol, it.start+width(it.lines[0]))
		}
	}
	asCol = tabStop(asCol + 4)

	if len(items) == 0 {
		return []string{riverKW(base, kw)}, nil
	}
	var lines []string
	for i, it := range items {
		l := append([]string(nil), it.lines...)
		if it.alias != "" {
			if len(l) == 1 {
				l[0] += spaces(asCol-it.start-width(l[0])) + it.alias
			} else {
				l[len(l)-1] += " " + it.alias
			}
		}
		if i < len(items)-1 {
			l[len(l)-1] += ","
		}
		if i == 0 {
			lines = clause(base, kw, l)
		} else {
			lines = append(lines, joinLines([]string{spaces(col)}, l)...)
		}
	}
	return lines, nil
}

// fromRow is one line group of a FROM clause: a table, a join, or ON lines.
type fromRow struct {
	prefix string   // river keyword part, or indentation
	lines  []string // the item (first line continues prefix)
	alias  string   // "AS ..." for the alias grid; "" when none
	extra  []string // lines printed after the row (ON/USING)
}

func (p *printer) fromLines(base int, kw string, items []*pg.Node) ([]string, error) {
	col := contentCol(base, "SELECT")
	var rows []*fromRow
	for i, it := range items {
		prefix := riverKW(base, kw) + " "
		if i > 0 {
			prefix = spaces(col)
			rows[len(rows)-1].suffix(",")
		}
		rel, joins, err := flattenJoins(it)
		if err != nil {
			return nil, err
		}
		row, err := p.fromItem(rel, prefix, width(prefix), base)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		for _, j := range joins {
			jkw, err := joinKeyword(j)
			if err != nil {
				return nil, err
			}
			prefix := riverKW(base, jkw) + " "
			row, err := p.fromItem(j.Rarg, prefix, width(prefix), base)
			if err != nil {
				return nil, err
			}
			switch {
			case j.Quals != nil:
				on, err := p.condLines(base, "ON", j.Quals)
				if err != nil {
					return nil, err
				}
				row.extra = on
			case len(j.UsingClause) > 0:
				names, err := stringList(j.UsingClause)
				if err != nil {
					return nil, err
				}
				row.extra = []string{riverKW(base, "USING") + " (" + quoteQualifiedEach(names) + ")"}
			}
			rows = append(rows, row)
		}
	}

	asCol := 0
	for _, r := range rows {
		if r.alias != "" && len(r.lines) == 1 {
			asCol = max(asCol, width(r.prefix)+width(r.lines[0]))
		}
	}
	asCol = tabStop(asCol + 4)

	var lines []string
	for _, r := range rows {
		l := joinLines([]string{r.prefix}, r.lines)
		if r.alias != "" {
			if len(r.lines) == 1 {
				head, comma := strings.CutSuffix(l[0], ",")
				l[0] = head + spaces(asCol-width(head)) + r.alias
				if comma {
					l[0] += ","
				}
			} else {
				last, comma := strings.CutSuffix(l[len(l)-1], ",")
				l[len(l)-1] = last + " " + r.alias
				if comma {
					l[len(l)-1] += ","
				}
			}
		}
		lines = append(lines, l...)
		lines = append(lines, r.extra...)
	}
	return lines, nil
}

// suffix appends text to the last line printed for the row.
func (r *fromRow) suffix(s string) {
	if len(r.extra) > 0 {
		r.extra[len(r.extra)-1] += s
		return
	}
	if r.alias != "" {
		r.alias += s
		return
	}
	r.lines[len(r.lines)-1] += s
}

// flattenJoins splits a left-deep join tree into its first relation and the
// joins that follow it.
func flattenJoins(n *pg.Node) (*pg.Node, []*pg.JoinExpr, error) {
	j := n.GetJoinExpr()
	if j == nil {
		return n, nil, nil
	}
	if j.Alias != nil || j.JoinUsingAlias != nil || j.Rarg.GetJoinExpr() != nil {
		return nil, nil, unsupported("nested join")
	}
	rel, joins, err := flattenJoins(j.Larg)
	if err != nil {
		return nil, nil, err
	}
	return rel, append(joins, j), nil
}

func joinKeyword(j *pg.JoinExpr) (string, error) {
	var kw string
	switch j.Jointype {
	case pg.JoinType_JOIN_INNER:
		kw = "JOIN"
		if j.Quals == nil && len(j.UsingClause) == 0 && !j.IsNatural {
			kw = "CROSS JOIN"
		}
	case pg.JoinType_JOIN_LEFT:
		kw = "LEFT JOIN"
	case pg.JoinType_JOIN_RIGHT:
		kw = "RIGHT JOIN"
	case pg.JoinType_JOIN_FULL:
		kw = "FULL JOIN"
	default:
		return "", unsupported("join type")
	}
	if j.IsNatural {
		kw = "NATURAL " + kw
	}
	return kw, nil
}

func aliasText(a *pg.Alias) (string, error) {
	if a == nil {
		return "", nil
	}
	s := "AS " + quoteIdent(a.Aliasname)
	if len(a.Colnames) > 0 {
		names, err := stringList(a.Colnames)
		if err != nil {
			return "", err
		}
		s += "(" + quoteQualifiedEach(names) + ")"
	}
	return s, nil
}

// fromItem prints a table, subquery or function in FROM. col is the column
// where it starts; base is the river of the enclosing statement.
func (p *printer) fromItem(n *pg.Node, prefix string, col, base int) (*fromRow, error) {
	row := &fromRow{prefix: prefix}
	var alias *pg.Alias
	switch {
	case n.GetRangeVar() != nil:
		rv := n.GetRangeVar()
		text := rangeVar(rv)
		if !rv.Inh {
			text = "ONLY " + text
		}
		row.lines = []string{text}
		alias = rv.Alias
	case n.GetRangeSubselect() != nil:
		rs := n.GetRangeSubselect()
		indent := contentCol(base, "SELECT")
		body, err := p.stmtLines(rs.Subquery, indent+4)
		if err != nil {
			return nil, err
		}
		open := "("
		if rs.Lateral {
			open = "LATERAL ("
		}
		row.lines = append(append([]string{open}, body...), spaces(indent)+")")
		alias = rs.Alias
	case n.GetRangeFunction() != nil:
		rf := n.GetRangeFunction()
		if rf.IsRowsfrom || len(rf.Coldeflist) > 0 || len(rf.Functions) != 1 {
			return nil, unsupported("FROM function")
		}
		pair := rf.Functions[0].GetList()
		if pair == nil || len(pair.Items) == 0 || (len(pair.Items) > 1 && pair.Items[1].Node != nil) {
			return nil, unsupported("FROM function")
		}
		var text string
		var err error
		if tc := pair.Items[0].GetTypeCast(); tc != nil {
			// FROM takes only function-call syntax, so a cast keeps CAST().
			arg, err := p.expr(tc.Arg, precNone)
			if err != nil {
				return nil, err
			}
			typ, err := p.typeName(tc.TypeName)
			if err != nil {
				return nil, err
			}
			text = "CAST(" + arg + " AS " + typ + ")"
		} else if text, err = p.expr(pair.Items[0], precNone); err != nil {
			return nil, err
		}
		if rf.Lateral {
			text = "LATERAL " + text
		}
		if rf.Ordinality {
			text += " WITH ORDINALITY"
		}
		row.lines = []string{text}
		alias = rf.Alias
	default:
		return nil, unsupported("FROM item")
	}
	a, err := aliasText(alias)
	if err != nil {
		return nil, err
	}
	row.alias = a
	return row, nil
}

// condLines prints a WHERE, HAVING or ON condition: each top-level AND/OR
// starts a line on the river. An OR group inside AND opens "(" and gets its
// own river inside. An empty kw prints the first condition without one.
func (p *printer) condLines(base int, kw string, cond *pg.Node) ([]string, error) {
	type part struct {
		kw   string
		node *pg.Node
		min  int
	}
	var parts []part
	add := func(kw string, n *pg.Node, min int) { parts = append(parts, part{kw, n, min}) }
	if b := cond.GetBoolExpr(); b != nil && b.Boolop == pg.BoolExprType_OR_EXPR {
		for i, a := range b.Args {
			k := "OR"
			if i == 0 {
				k = kw
			}
			if ab := a.GetBoolExpr(); ab != nil && ab.Boolop == pg.BoolExprType_AND_EXPR {
				for j, x := range ab.Args {
					if j == 0 {
						add(k, x, precAnd+1)
					} else {
						add("AND", x, precAnd+1)
					}
				}
				continue
			}
			add(k, a, precOr+1)
		}
	} else if b != nil && b.Boolop == pg.BoolExprType_AND_EXPR {
		for i, a := range b.Args {
			k := "AND"
			if i == 0 {
				k = kw
			}
			add(k, a, precAnd+1)
		}
	} else {
		add(kw, cond, precNone)
	}

	col := base + riverWidth + 1
	var lines []string
	for _, pt := range parts {
		prefix := spaces(col)
		if pt.kw != "" {
			prefix = riverKW(base, pt.kw) + " "
		}
		group, not := pt.node, ""
		if b := group.GetBoolExpr(); b != nil && b.Boolop == pg.BoolExprType_NOT_EXPR && len(b.Args) == 1 {
			if ib := b.Args[0].GetBoolExpr(); ib != nil && ib.Boolop != pg.BoolExprType_NOT_EXPR {
				group, not = b.Args[0], "NOT "
			}
		}
		if b := group.GetBoolExpr(); b != nil && b.Boolop != pg.BoolExprType_NOT_EXPR &&
			(not != "" || pt.min > precOr+1 || b.Boolop == pg.BoolExprType_OR_EXPR && pt.min > precOr) {
			inner, err := p.condLines(base+4, "", group)
			if err != nil {
				return nil, err
			}
			lines = append(lines, prefix+not+"(")
			lines = append(lines, inner...)
			lines = append(lines, spaces(col)+")")
			continue
		}
		l, err := p.layout(pt.node, col, col, pt.min)
		if err != nil {
			return nil, err
		}
		lines = append(lines, joinLines([]string{prefix}, l)...)
	}
	return lines, nil
}

func (p *printer) returningLines(base int, list []*pg.Node) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	var items []string
	for _, t := range list {
		rt := t.GetResTarget()
		if rt == nil {
			return nil, unsupported("RETURNING item")
		}
		e, err := p.expr(rt.Val, precNone)
		if err != nil {
			return nil, err
		}
		if rt.Name != "" {
			e += " AS " + quoteIdent(rt.Name)
		}
		items = append(items, e)
	}
	return p.fillList(base, "RETURNING", items), nil
}

func targetName(rt *pg.ResTarget) (string, error) {
	var s strings.Builder
	s.WriteString(quoteIdent(rt.Name))
	for _, ind := range rt.Indirection {
		switch {
		case ind.GetString_() != nil:
			s.WriteString("." + quoteIdent(ind.GetString_().Sval))
		default:
			return "", unsupported("assignment target")
		}
	}
	return s.String(), nil
}

func (p *printer) insertLines(s *pg.InsertStmt, base int) ([]string, error) {
	lines, err := p.withLines(s.WithClause, base)
	if err != nil {
		return nil, err
	}
	head := spaces(base) + "INSERT INTO " + rangeVar(s.Relation)
	if s.Relation.Alias != nil {
		a, err := aliasText(s.Relation.Alias)
		if err != nil {
			return nil, err
		}
		head += " " + a
	}
	if len(s.Cols) == 0 {
		lines = append(lines, head)
	} else {
		lines = append(lines, head+" (")
		for i, c := range s.Cols {
			rt := c.GetResTarget()
			if rt == nil {
				return nil, unsupported("insert column")
			}
			name, err := targetName(rt)
			if err != nil {
				return nil, err
			}
			if i < len(s.Cols)-1 {
				name += ","
			}
			lines = append(lines, spaces(base+4)+name)
		}
		lines = append(lines, spaces(base)+")")
	}
	switch s.Override {
	case pg.OverridingKind_OVERRIDING_SYSTEM_VALUE:
		lines = append(lines, spaces(base)+"OVERRIDING SYSTEM VALUE")
	case pg.OverridingKind_OVERRIDING_USER_VALUE:
		lines = append(lines, spaces(base)+"OVERRIDING USER VALUE")
	}
	if s.SelectStmt == nil {
		lines = append(lines, spaces(base)+"DEFAULT VALUES")
	} else {
		body, err := p.stmtLines(s.SelectStmt, base)
		if err != nil {
			return nil, err
		}
		lines = append(lines, body...)
	}
	if oc := s.OnConflictClause; oc != nil {
		conflict, err := p.onConflictLines(oc, base)
		if err != nil {
			return nil, err
		}
		lines = append(lines, conflict...)
	}
	ret, err := p.returningLines(base, s.ReturningList)
	if err != nil {
		return nil, err
	}
	return append(lines, ret...), nil
}

func (p *printer) onConflictLines(oc *pg.OnConflictClause, base int) ([]string, error) {
	line := riverKW(base, "ON CONFLICT")
	if inf := oc.Infer; inf != nil {
		switch {
		case inf.Conname != "":
			line += " ON CONSTRAINT " + quoteIdent(inf.Conname)
		case len(inf.IndexElems) > 0:
			params, err := p.indexParams(inf.IndexElems)
			if err != nil {
				return nil, err
			}
			line += " (" + params + ")"
			if inf.WhereClause != nil {
				w, err := p.expr(inf.WhereClause, precNone)
				if err != nil {
					return nil, err
				}
				line += " WHERE " + w
			}
		}
	}
	switch oc.Action {
	case pg.OnConflictAction_ONCONFLICT_NOTHING:
		return []string{line + " DO NOTHING"}, nil
	case pg.OnConflictAction_ONCONFLICT_UPDATE:
	default:
		return nil, unsupported("ON CONFLICT action")
	}
	p.excludedRecord = true
	defer func() { p.excludedRecord = false }()
	lines := []string{line + " DO UPDATE"}
	set, err := p.setLines(base, oc.TargetList)
	if err != nil {
		return nil, err
	}
	lines = append(lines, set...)
	if oc.WhereClause != nil {
		w, err := p.condLines(base, "WHERE", oc.WhereClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, w...)
	}
	return lines, nil
}

// setLines prints SET assignments, one per line, with "=" aligned on the
// tab-stop grid. Multi-line values continue below their row.
func (p *printer) setLines(base int, targets []*pg.Node) ([]string, error) {
	col := contentCol(base, "SET")
	rows := make([]gridRow, len(targets))
	extra := make([][]string, len(targets))
	names := make([]string, len(targets))
	for i, t := range targets {
		rt := t.GetResTarget()
		if rt == nil || rt.Val.GetMultiAssignRef() != nil {
			return nil, unsupported("SET item")
		}
		name, err := targetName(rt)
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	nameW := 0
	for _, n := range names {
		nameW = max(nameW, width(n))
	}
	valCol := tabStop(col+nameW+4) + 2
	for i, t := range targets {
		rt := t.GetResTarget()
		val, err := p.layout(rt.Val, valCol, col, precNone)
		if err != nil {
			return nil, err
		}
		rows[i] = gridRow{cells: []string{names[i], "= " + val[0]}}
		extra[i] = val[1:]
		if i < len(targets)-1 {
			if len(extra[i]) > 0 {
				extra[i][len(extra[i])-1] += ","
			} else {
				rows[i].suffix = ","
			}
		}
	}
	grid := renderGrid(col, rows)
	var lines []string
	for i, g := range grid {
		if i == 0 {
			g = riverKW(base, "SET") + g[width(riverKW(base, "SET")):]
		}
		lines = append(lines, g)
		lines = append(lines, extra[i]...)
	}
	return lines, nil
}

func (p *printer) updateLines(s *pg.UpdateStmt, base int) ([]string, error) {
	lines, err := p.withLines(s.WithClause, base)
	if err != nil {
		return nil, err
	}
	head := spaces(base) + "UPDATE "
	if !s.Relation.Inh {
		head += "ONLY "
	}
	head += rangeVar(s.Relation)
	if s.Relation.Alias != nil {
		a, err := aliasText(s.Relation.Alias)
		if err != nil {
			return nil, err
		}
		head += " " + a
	}
	lines = append(lines, head)
	set, err := p.setLines(base, s.TargetList)
	if err != nil {
		return nil, err
	}
	lines = append(lines, set...)
	if len(s.FromClause) > 0 {
		from, err := p.fromLines(base, "FROM", s.FromClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, from...)
	}
	if s.WhereClause != nil {
		w, err := p.condLines(base, "WHERE", s.WhereClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, w...)
	}
	ret, err := p.returningLines(base, s.ReturningList)
	if err != nil {
		return nil, err
	}
	return append(lines, ret...), nil
}

func (p *printer) deleteLines(s *pg.DeleteStmt, base int) ([]string, error) {
	lines, err := p.withLines(s.WithClause, base)
	if err != nil {
		return nil, err
	}
	head := spaces(base) + "DELETE FROM "
	if !s.Relation.Inh {
		head += "ONLY "
	}
	head += rangeVar(s.Relation)
	if s.Relation.Alias != nil {
		a, err := aliasText(s.Relation.Alias)
		if err != nil {
			return nil, err
		}
		head += " " + a
	}
	lines = append(lines, head)
	if len(s.UsingClause) > 0 {
		using, err := p.fromLines(base, "USING", s.UsingClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, using...)
	}
	if s.WhereClause != nil {
		w, err := p.condLines(base, "WHERE", s.WhereClause)
		if err != nil {
			return nil, err
		}
		lines = append(lines, w...)
	}
	ret, err := p.returningLines(base, s.ReturningList)
	if err != nil {
		return nil, err
	}
	return append(lines, ret...), nil
}
