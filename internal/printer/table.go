package printer

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// Column constraint slots, in print order.
const (
	slotKey = iota
	slotNull
	slotDefault
	slotOther
	numSlots
)

// ColumnConstraintSlot returns the slot of a column constraint. Attribute
// constraints (DEFERRABLE...) return -1: they belong to the constraint
// before them.
func ColumnConstraintSlot(c *pg.Constraint) int {
	switch c.Contype {
	case pg.ConstrType_CONSTR_PRIMARY, pg.ConstrType_CONSTR_UNIQUE:
		return slotKey
	case pg.ConstrType_CONSTR_NOTNULL, pg.ConstrType_CONSTR_NULL:
		return slotNull
	case pg.ConstrType_CONSTR_DEFAULT:
		return slotDefault
	case pg.ConstrType_CONSTR_ATTR_DEFERRABLE, pg.ConstrType_CONSTR_ATTR_NOT_DEFERRABLE,
		pg.ConstrType_CONSTR_ATTR_DEFERRED, pg.ConstrType_CONSTR_ATTR_IMMEDIATE:
		return -1
	}
	return slotOther
}

func (p *printer) createTable(s *pg.CreateStmt) ([]string, error) {
	if len(s.InhRelations) > 0 || s.Partbound != nil || s.Partspec != nil || s.OfTypename != nil ||
		len(s.Constraints) > 0 || len(s.Options) > 0 || s.Tablespacename != "" || s.AccessMethod != "" ||
		s.Oncommit != pg.OnCommitAction_ONCOMMIT_NOOP {
		return nil, unsupported("CREATE TABLE option")
	}

	head := "CREATE TABLE "
	switch s.Relation.Relpersistence {
	case "u":
		head = "CREATE UNLOGGED TABLE "
	case "t":
		head = "CREATE TEMPORARY TABLE "
	}
	if s.IfNotExists {
		head += "IF NOT EXISTS "
	}
	head += rangeVar(s.Relation)

	open := p.findToken(0, pg.Token_ASCII_40)
	if open < 0 {
		return nil, unsupported("CREATE TABLE without column list")
	}
	close := p.matching(open)
	if close != len(p.stmt.Tokens)-1 || p.hasComments(0, open) {
		return nil, unsupported("CREATE TABLE layout")
	}
	if len(s.TableElts) == 0 {
		if p.hasComments(open, close) {
			return nil, unsupported("comment in empty table")
		}
		return []string{head + " ();"}, nil
	}
	segs, lc, err := p.splitList(open, close)
	if err != nil {
		return nil, err
	}
	if len(segs) != len(s.TableElts) {
		return nil, unsupported("table element count")
	}

	var cols, cons []gridRow
	for i, el := range s.TableElts {
		row := gridRow{leading: lc.leading[i], comment: lc.trailing[i]}
		switch {
		case el.GetColumnDef() != nil:
			cells, err := p.columnCells(el.GetColumnDef())
			if err != nil {
				return nil, err
			}
			row.cells = cells
			if len(cons) > 0 {
				return nil, unsupported("column after table constraint")
			}
			cols = append(cols, row)
		case el.GetConstraint() != nil:
			c := el.GetConstraint()
			def, err := p.tableConstraint(c)
			if err != nil {
				return nil, err
			}
			if c.Conname != "" {
				row.cells = []string{"CONSTRAINT", quoteIdent(c.Conname), def}
			} else {
				row.cells = []string{"", "", def}
			}
			cons = append(cons, row)
		default:
			return nil, unsupported("table element")
		}
	}
	all := append(append([]gridRow(nil), cols...), cons...)
	for i := range all {
		if i < len(all)-1 {
			all[i].suffix = ","
		}
	}
	cols, cons = all[:len(cols)], all[len(cols):]

	lines := []string{head + " ("}
	if len(cols) > 0 {
		lines = append(lines, renderGrid(4, cols)...)
	}
	if len(cols) > 0 && len(cons) > 0 {
		lines = append(lines, "")
	}
	if len(cons) > 0 {
		lines = append(lines, renderGrid(4, cons)...)
	}
	for _, c := range lc.tail {
		lines = append(lines, "    "+c)
	}
	return append(lines, ");"), nil
}

// columnCells returns name, type, and the four constraint slots of a column.
func (p *printer) columnCells(cd *pg.ColumnDef) ([]string, error) {
	if cd.Storage != "" || cd.StorageName != "" || len(cd.Fdwoptions) > 0 || cd.RawDefault != nil ||
		cd.CookedDefault != nil || cd.Identity != "" || cd.Generated != "" {
		return nil, unsupported("column option")
	}
	typ, err := p.typeName(cd.TypeName)
	if err != nil {
		return nil, err
	}
	if cd.CollClause != nil {
		name, err := quotedNameList(cd.CollClause.Collname)
		if err != nil {
			return nil, err
		}
		typ += " COLLATE " + name
	}
	if cd.Compression != "" {
		typ += " COMPRESSION " + lowerBare(cd.Compression)
	}

	var slots [numSlots][]string
	last := -1
	for _, n := range cd.Constraints {
		c := n.GetConstraint()
		if c == nil {
			return nil, unsupported("column constraint")
		}
		text, err := p.columnConstraint(c)
		if err != nil {
			return nil, err
		}
		slot := ColumnConstraintSlot(c)
		if slot < 0 {
			if last < 0 {
				return nil, unsupported("constraint attribute without constraint")
			}
			slot = last
		}
		slots[slot] = append(slots[slot], text)
		last = slot
	}
	cells := []string{quoteIdent(cd.Colname), typ}
	for _, s := range slots {
		cells = append(cells, strings.Join(s, " "))
	}
	return cells, nil
}

func (p *printer) columnConstraint(c *pg.Constraint) (string, error) {
	prefix := ""
	if c.Conname != "" {
		prefix = "CONSTRAINT " + quoteIdent(c.Conname) + " "
	}
	switch c.Contype {
	case pg.ConstrType_CONSTR_NOTNULL:
		if c.IsNoInherit {
			return prefix + "NOT NULL NO INHERIT", nil
		}
		return prefix + "NOT NULL", nil
	case pg.ConstrType_CONSTR_NULL:
		return prefix + "NULL", nil
	case pg.ConstrType_CONSTR_DEFAULT:
		// DEFAULT takes a b_expr: AND, OR, NOT, IS, IN, LIKE and the
		// comparisons need parentheses there.
		p.bExpr = true
		e, err := p.expr(c.RawExpr, precOp)
		p.bExpr = false
		return prefix + "DEFAULT " + e, err
	case pg.ConstrType_CONSTR_ATTR_DEFERRABLE:
		return "DEFERRABLE", nil
	case pg.ConstrType_CONSTR_ATTR_NOT_DEFERRABLE:
		return "NOT DEFERRABLE", nil
	case pg.ConstrType_CONSTR_ATTR_DEFERRED:
		return "INITIALLY DEFERRED", nil
	case pg.ConstrType_CONSTR_ATTR_IMMEDIATE:
		return "INITIALLY IMMEDIATE", nil
	case pg.ConstrType_CONSTR_IDENTITY:
		if len(c.Options) > 0 {
			return "", unsupported("identity options")
		}
		switch c.GeneratedWhen {
		case "a":
			return prefix + "GENERATED ALWAYS AS IDENTITY", nil
		case "d":
			return prefix + "GENERATED BY DEFAULT AS IDENTITY", nil
		}
		return "", unsupported("identity")
	case pg.ConstrType_CONSTR_GENERATED:
		e, err := p.expr(c.RawExpr, precNone)
		return prefix + "GENERATED ALWAYS AS (" + e + ") STORED", err
	}
	body, err := p.constraintBody(c, false)
	return prefix + body, err
}

// tableConstraint prints a table constraint without its CONSTRAINT name.
func (p *printer) tableConstraint(c *pg.Constraint) (string, error) {
	switch c.Contype {
	case pg.ConstrType_CONSTR_PRIMARY, pg.ConstrType_CONSTR_UNIQUE,
		pg.ConstrType_CONSTR_CHECK, pg.ConstrType_CONSTR_FOREIGN:
	default:
		return "", unsupported("table constraint kind")
	}
	s, err := p.constraintBody(c, true)
	if err != nil {
		return "", err
	}
	if c.Deferrable {
		s += " DEFERRABLE"
	}
	if c.Initdeferred {
		s += " INITIALLY DEFERRED"
	}
	return s, nil
}

// constraintBody prints PRIMARY KEY, UNIQUE, CHECK and foreign keys. Table
// constraints list their columns; column constraints do not.
func (p *printer) constraintBody(c *pg.Constraint, table bool) (string, error) {
	if len(c.Options) > 0 || c.Indexname != "" || c.Indexspace != "" || c.AccessMethod != "" || c.WhereClause != nil {
		return "", unsupported("constraint index option")
	}
	cols := func(nodes []*pg.Node) (string, error) {
		names, err := stringList(nodes)
		if err != nil {
			return "", err
		}
		return "(" + quoteQualifiedEach(names) + ")", nil
	}
	var b strings.Builder
	switch c.Contype {
	case pg.ConstrType_CONSTR_PRIMARY, pg.ConstrType_CONSTR_UNIQUE:
		if c.Contype == pg.ConstrType_CONSTR_PRIMARY {
			b.WriteString("PRIMARY KEY")
		} else {
			b.WriteString("UNIQUE")
			if c.NullsNotDistinct {
				b.WriteString(" NULLS NOT DISTINCT")
			}
		}
		if table {
			s, err := cols(c.Keys)
			if err != nil {
				return "", err
			}
			b.WriteString(" " + s)
		} else if len(c.Keys) > 0 {
			return "", unsupported("column key with columns")
		}
		if len(c.Including) > 0 {
			s, err := cols(c.Including)
			if err != nil {
				return "", err
			}
			b.WriteString(" INCLUDE " + s)
		}
	case pg.ConstrType_CONSTR_CHECK:
		e, err := p.expr(c.RawExpr, precNone)
		if err != nil {
			return "", err
		}
		b.WriteString("CHECK (" + e + ")")
		if c.IsNoInherit {
			b.WriteString(" NO INHERIT")
		}
	case pg.ConstrType_CONSTR_FOREIGN:
		if table {
			s, err := cols(c.FkAttrs)
			if err != nil {
				return "", err
			}
			b.WriteString("FOREIGN KEY " + s + " ")
		}
		b.WriteString("REFERENCES " + rangeVar(c.Pktable))
		if len(c.PkAttrs) > 0 {
			s, err := cols(c.PkAttrs)
			if err != nil {
				return "", err
			}
			b.WriteString(" " + s)
		}
		switch c.FkMatchtype {
		case "f":
			b.WriteString(" MATCH FULL")
		case "p":
			b.WriteString(" MATCH PARTIAL")
		}
		if len(c.FkDelSetCols) > 0 {
			return "", unsupported("ON DELETE SET with columns")
		}
		for _, a := range []struct{ when, action string }{{"DELETE", c.FkDelAction}, {"UPDATE", c.FkUpdAction}} {
			word := map[string]string{"r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}[a.action]
			if word != "" {
				b.WriteString(" ON " + a.when + " " + word)
			}
		}
	default:
		return "", unsupported("constraint kind")
	}
	if c.SkipValidation {
		b.WriteString(" NOT VALID")
	}
	return b.String(), nil
}

func quoteQualifiedEach(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = quoteIdent(n)
	}
	return strings.Join(quoted, ", ")
}

func (p *printer) alterTable(s *pg.AlterTableStmt) ([]string, error) {
	if s.Objtype != pg.ObjectType_OBJECT_TABLE {
		return nil, unsupported("ALTER of non-table")
	}
	head := "ALTER TABLE "
	if s.MissingOk {
		head += "IF EXISTS "
	}
	if !s.Relation.Inh {
		head += "ONLY "
	}
	head += rangeVar(s.Relation)

	// The actions start after the table name.
	nameAt := p.tokenAt(s.Relation.Location)
	if nameAt < 0 {
		return nil, unsupported("table name position")
	}
	last := nameAt
	for last+2 < len(p.stmt.Tokens) && p.stmt.Tokens[last+1].Kind == pg.Token_ASCII_46 {
		last += 2
	}
	if p.hasComments(0, last+1) {
		return nil, unsupported("comment in ALTER TABLE header")
	}
	segs, lc, err := p.splitList(last, len(p.stmt.Tokens))
	if err != nil {
		return nil, err
	}
	if len(segs) != len(s.Cmds) {
		return nil, unsupported("ALTER TABLE action count")
	}

	rows := make([]gridRow, len(s.Cmds))
	for i, n := range s.Cmds {
		cmd := n.GetAlterTableCmd()
		if cmd == nil {
			return nil, unsupported("ALTER TABLE action")
		}
		cells, err := p.alterCmd(cmd)
		if err != nil {
			return nil, err
		}
		rows[i] = gridRow{cells: cells, leading: lc.leading[i], comment: lc.trailing[i], suffix: ","}
	}
	rows[len(rows)-1].suffix = ";"
	lines := append([]string{head}, renderGrid(0, rows)...)
	return append(lines, lc.tail...), nil
}

func behavior(b pg.DropBehavior) string {
	if b == pg.DropBehavior_DROP_CASCADE {
		return " CASCADE"
	}
	return ""
}

// alterCmd returns the grid cells of one ALTER TABLE action: the action
// prefix, the object name, then the rest.
func (p *printer) alterCmd(c *pg.AlterTableCmd) ([]string, error) {
	switch c.Subtype {
	case pg.AlterTableType_AT_AddColumn:
		cd := c.Def.GetColumnDef()
		if cd == nil {
			return nil, unsupported("ADD COLUMN definition")
		}
		prefix := "ADD COLUMN"
		if c.MissingOk {
			prefix += " IF NOT EXISTS"
		}
		cells, err := p.columnCells(cd)
		if err != nil {
			return nil, err
		}
		return append([]string{prefix}, cells...), nil

	case pg.AlterTableType_AT_DropColumn:
		prefix := "DROP COLUMN"
		if c.MissingOk {
			prefix += " IF EXISTS"
		}
		return []string{prefix, quoteIdent(c.Name) + behavior(c.Behavior)}, nil

	case pg.AlterTableType_AT_AlterColumnType:
		cd := c.Def.GetColumnDef()
		if cd == nil {
			return nil, unsupported("ALTER COLUMN TYPE definition")
		}
		typ, err := p.typeName(cd.TypeName)
		if err != nil {
			return nil, err
		}
		rest := "TYPE " + typ
		if cd.CollClause != nil {
			name, err := quotedNameList(cd.CollClause.Collname)
			if err != nil {
				return nil, err
			}
			rest += " COLLATE " + name
		}
		if cd.RawDefault != nil {
			e, err := p.expr(cd.RawDefault, precNone)
			if err != nil {
				return nil, err
			}
			rest += " USING " + e
		}
		return []string{"ALTER COLUMN", quoteIdent(c.Name), rest}, nil

	case pg.AlterTableType_AT_ColumnDefault:
		if c.Def == nil {
			return []string{"ALTER COLUMN", quoteIdent(c.Name), "DROP DEFAULT"}, nil
		}
		e, err := p.expr(c.Def, precNone)
		if err != nil {
			return nil, err
		}
		return []string{"ALTER COLUMN", quoteIdent(c.Name), "SET DEFAULT " + e}, nil

	case pg.AlterTableType_AT_SetNotNull:
		return []string{"ALTER COLUMN", quoteIdent(c.Name), "SET NOT NULL"}, nil

	case pg.AlterTableType_AT_DropNotNull:
		return []string{"ALTER COLUMN", quoteIdent(c.Name), "DROP NOT NULL"}, nil

	case pg.AlterTableType_AT_AddConstraint:
		con := c.Def.GetConstraint()
		if con == nil {
			return nil, unsupported("ADD CONSTRAINT definition")
		}
		def, err := p.tableConstraint(con)
		if err != nil {
			return nil, err
		}
		if con.Conname == "" {
			return []string{"ADD", "", def}, nil
		}
		return []string{"ADD CONSTRAINT", quoteIdent(con.Conname), def}, nil

	case pg.AlterTableType_AT_DropConstraint:
		prefix := "DROP CONSTRAINT"
		if c.MissingOk {
			prefix += " IF EXISTS"
		}
		return []string{prefix, quoteIdent(c.Name) + behavior(c.Behavior)}, nil

	case pg.AlterTableType_AT_ValidateConstraint:
		return []string{"VALIDATE CONSTRAINT", quoteIdent(c.Name)}, nil

	case pg.AlterTableType_AT_EnableRowSecurity:
		return []string{"ENABLE ROW LEVEL SECURITY"}, nil
	case pg.AlterTableType_AT_DisableRowSecurity:
		return []string{"DISABLE ROW LEVEL SECURITY"}, nil
	case pg.AlterTableType_AT_ForceRowSecurity:
		return []string{"FORCE ROW LEVEL SECURITY"}, nil
	case pg.AlterTableType_AT_NoForceRowSecurity:
		return []string{"NO FORCE ROW LEVEL SECURITY"}, nil
	}
	return nil, unsupported("ALTER TABLE action " + c.Subtype.String())
}
