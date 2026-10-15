package printer

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

func (p *printer) createIndex(s *pg.IndexStmt) ([]string, error) {
	if len(s.Options) > 0 || s.TableSpace != "" || len(s.ExcludeOpNames) > 0 || s.NullsNotDistinct {
		return nil, unsupported("CREATE INDEX option")
	}
	head := "CREATE "
	if s.Unique {
		head += "UNIQUE "
	}
	head += "INDEX"
	if s.Concurrent {
		head += " CONCURRENTLY"
	}
	if s.IfNotExists {
		head += " IF NOT EXISTS"
	}
	if s.Idxname != "" {
		head += " " + quoteIdent(s.Idxname)
	}

	on := "ON "
	if !s.Relation.Inh {
		on += "ONLY "
	}
	on += rangeVar(s.Relation)
	if s.AccessMethod != "" && s.AccessMethod != "btree" {
		on += " USING " + lowerBare(s.AccessMethod)
	}
	params, err := p.indexParams(s.IndexParams)
	if err != nil {
		return nil, err
	}
	on += " (" + params + ")"

	lines := []string{head, on}
	if len(s.IndexIncludingParams) > 0 {
		inc, err := p.indexParams(s.IndexIncludingParams)
		if err != nil {
			return nil, err
		}
		lines = append(lines, "INCLUDE ("+inc+")")
	}
	if s.WhereClause != nil {
		w, err := p.expr(s.WhereClause, precNone)
		if err != nil {
			return nil, err
		}
		lines = append(lines, "WHERE "+w)
	}
	return endStmt(lines), nil
}

func (p *printer) indexParams(nodes []*pg.Node) (string, error) {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		e := n.GetIndexElem()
		if e == nil || e.Indexcolname != "" || len(e.Opclassopts) > 0 {
			return "", unsupported("index element")
		}
		var s string
		switch {
		case e.Name != "":
			s = quoteIdent(e.Name)
		case e.Expr != nil && e.Expr.GetFuncCall() != nil:
			var err error
			if s, err = p.expr(e.Expr, precNone); err != nil {
				return "", err
			}
		case e.Expr != nil:
			x, err := p.expr(e.Expr, precNone)
			if err != nil {
				return "", err
			}
			s = "(" + x + ")"
		default:
			return "", unsupported("index element")
		}
		if len(e.Collation) > 0 {
			c, err := quotedNameList(e.Collation)
			if err != nil {
				return "", err
			}
			s += " COLLATE " + c
		}
		if len(e.Opclass) > 0 {
			names, err := stringList(e.Opclass)
			if err != nil {
				return "", err
			}
			if len(names) == 1 {
				s += " " + lowerBare(names[0])
			} else {
				s += " " + quoteQualified(names...)
			}
		}
		switch e.Ordering {
		case pg.SortByDir_SORTBY_ASC:
			s += " ASC"
		case pg.SortByDir_SORTBY_DESC:
			s += " DESC"
		case pg.SortByDir_SORTBY_USING:
			return "", unsupported("index ordering")
		}
		switch e.NullsOrdering {
		case pg.SortByNulls_SORTBY_NULLS_FIRST:
			s += " NULLS FIRST"
		case pg.SortByNulls_SORTBY_NULLS_LAST:
			s += " NULLS LAST"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", "), nil
}

// Trigger timing and event bits, from PostgreSQL's trigger.h.
const (
	triggerTypeBefore   = 1 << 1
	triggerTypeInsert   = 1 << 2
	triggerTypeDelete   = 1 << 3
	triggerTypeUpdate   = 1 << 4
	triggerTypeTruncate = 1 << 5
	triggerTypeInstead  = 1 << 6
)

func (p *printer) createTrigger(s *pg.CreateTrigStmt) ([]string, error) {
	if s.Isconstraint || len(s.TransitionRels) > 0 || s.Constrrel != nil || s.Deferrable || s.Initdeferred {
		return nil, unsupported("CREATE TRIGGER option")
	}
	head := "CREATE "
	if s.Replace {
		head += "OR REPLACE "
	}
	head += "TRIGGER " + quoteIdent(s.Trigname)

	var timing string
	switch {
	case s.Timing&triggerTypeBefore != 0:
		timing = "BEFORE"
	case s.Timing&triggerTypeInstead != 0:
		timing = "INSTEAD OF"
	default:
		timing = "AFTER"
	}
	var events []string
	if s.Events&triggerTypeInsert != 0 {
		events = append(events, "INSERT")
	}
	if s.Events&triggerTypeUpdate != 0 {
		ev := "UPDATE"
		if len(s.Columns) > 0 {
			names, err := stringList(s.Columns)
			if err != nil {
				return nil, err
			}
			ev += " OF " + quoteQualifiedEach(names)
		}
		events = append(events, ev)
	}
	if s.Events&triggerTypeDelete != 0 {
		events = append(events, "DELETE")
	}
	if s.Events&triggerTypeTruncate != 0 {
		events = append(events, "TRUNCATE")
	}
	if len(events) == 0 {
		return nil, unsupported("trigger events")
	}

	lines := []string{
		head,
		timing + " " + strings.Join(events, " OR ") + " ON " + rangeVar(s.Relation),
	}
	if s.Row {
		lines = append(lines, "FOR EACH ROW")
	} else {
		lines = append(lines, "FOR EACH STATEMENT")
	}
	if s.WhenClause != nil {
		p.triggerRecords = true
		w, err := p.expr(s.WhenClause, precNone)
		p.triggerRecords = false
		if err != nil {
			return nil, err
		}
		lines = append(lines, "WHEN ("+w+")")
	}

	fn, err := funcName(s.Funcname)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, len(s.Args))
	for _, a := range s.Args {
		str := a.GetString_()
		if str == nil {
			return nil, unsupported("trigger argument")
		}
		args = append(args, stringLiteral(str.Sval))
	}
	lines = append(lines, "EXECUTE FUNCTION "+fn+"("+strings.Join(args, ", ")+")")
	return endStmt(lines), nil
}

func (p *printer) drop(s *pg.DropStmt) ([]string, error) {
	kinds := map[pg.ObjectType]string{
		pg.ObjectType_OBJECT_TABLE:         "TABLE",
		pg.ObjectType_OBJECT_VIEW:          "VIEW",
		pg.ObjectType_OBJECT_MATVIEW:       "MATERIALIZED VIEW",
		pg.ObjectType_OBJECT_INDEX:         "INDEX",
		pg.ObjectType_OBJECT_SEQUENCE:      "SEQUENCE",
		pg.ObjectType_OBJECT_FOREIGN_TABLE: "FOREIGN TABLE",
		pg.ObjectType_OBJECT_TYPE:          "TYPE",
		pg.ObjectType_OBJECT_DOMAIN:        "DOMAIN",
		pg.ObjectType_OBJECT_SCHEMA:        "SCHEMA",
		pg.ObjectType_OBJECT_EXTENSION:     "EXTENSION",
		pg.ObjectType_OBJECT_FUNCTION:      "FUNCTION",
		pg.ObjectType_OBJECT_PROCEDURE:     "PROCEDURE",
		pg.ObjectType_OBJECT_ROUTINE:       "ROUTINE",
		pg.ObjectType_OBJECT_AGGREGATE:     "AGGREGATE",
		pg.ObjectType_OBJECT_TRIGGER:       "TRIGGER",
		pg.ObjectType_OBJECT_POLICY:        "POLICY",
		pg.ObjectType_OBJECT_RULE:          "RULE",
	}
	kind, ok := kinds[s.RemoveType]
	if !ok {
		return nil, unsupported("DROP " + s.RemoveType.String())
	}
	line := "DROP " + kind
	if s.Concurrent {
		line += " CONCURRENTLY"
	}
	if s.MissingOk {
		line += " IF EXISTS"
	}

	names := make([]string, 0, len(s.Objects))
	for _, o := range s.Objects {
		name, err := p.dropObject(s.RemoveType, o)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	line += " " + strings.Join(names, ", ") + behavior(s.Behavior)
	return []string{line + ";"}, nil
}

func (p *printer) dropObject(kind pg.ObjectType, o *pg.Node) (string, error) {
	switch kind {
	case pg.ObjectType_OBJECT_TYPE, pg.ObjectType_OBJECT_DOMAIN:
		return p.typeName(o.GetTypeName())
	case pg.ObjectType_OBJECT_SCHEMA:
		s := o.GetString_()
		if s == nil {
			return "", unsupported("schema name")
		}
		return quoteIdent(s.Sval), nil
	case pg.ObjectType_OBJECT_EXTENSION:
		s := o.GetString_()
		if s == nil {
			return "", unsupported("extension name")
		}
		return lowerBare(s.Sval), nil
	case pg.ObjectType_OBJECT_FUNCTION, pg.ObjectType_OBJECT_PROCEDURE,
		pg.ObjectType_OBJECT_ROUTINE, pg.ObjectType_OBJECT_AGGREGATE:
		return p.objectWithArgs(o.GetObjectWithArgs())
	case pg.ObjectType_OBJECT_TRIGGER, pg.ObjectType_OBJECT_POLICY, pg.ObjectType_OBJECT_RULE:
		list := o.GetList()
		if list == nil || len(list.Items) < 2 {
			return "", unsupported("DROP ... ON name")
		}
		names, err := stringList(list.Items)
		if err != nil {
			return "", err
		}
		obj := names[len(names)-1]
		return quoteIdent(obj) + " ON " + quoteQualified(names[:len(names)-1]...), nil
	}
	list := o.GetList()
	if list == nil {
		return "", unsupported("object name")
	}
	return quotedNameList(list.Items)
}

func (p *printer) objectWithArgs(o *pg.ObjectWithArgs) (string, error) {
	if o == nil {
		return "", unsupported("function signature")
	}
	name, err := quotedNameList(o.Objname)
	if err != nil {
		return "", err
	}
	if o.ArgsUnspecified {
		return name, nil
	}
	args := make([]string, 0, len(o.Objfuncargs))
	if len(o.Objfuncargs) > 0 {
		for _, a := range o.Objfuncargs {
			fp := a.GetFunctionParameter()
			if fp == nil {
				return "", unsupported("function argument")
			}
			s, err := p.paramText(fp)
			if err != nil {
				return "", err
			}
			args = append(args, s)
		}
	} else {
		for _, a := range o.Objargs {
			s, err := p.typeName(a.GetTypeName())
			if err != nil {
				return "", err
			}
			args = append(args, s)
		}
	}
	return name + "(" + strings.Join(args, ", ") + ")", nil
}

var paramModes = map[pg.FunctionParameterMode]string{
	pg.FunctionParameterMode_FUNC_PARAM_IN:       "IN",
	pg.FunctionParameterMode_FUNC_PARAM_OUT:      "OUT",
	pg.FunctionParameterMode_FUNC_PARAM_INOUT:    "INOUT",
	pg.FunctionParameterMode_FUNC_PARAM_VARIADIC: "VARIADIC",
}

// paramCells returns mode, name, type and default of a function parameter.
func (p *printer) paramCells(fp *pg.FunctionParameter) ([]string, error) {
	mode, ok := paramModes[fp.Mode]
	if !ok && fp.Mode != pg.FunctionParameterMode_FUNC_PARAM_DEFAULT {
		return nil, unsupported("parameter mode")
	}
	typ, err := p.typeName(fp.ArgType)
	if err != nil {
		return nil, err
	}
	name := ""
	if fp.Name != "" {
		name = quoteIdent(fp.Name)
	}
	def := ""
	if fp.Defexpr != nil {
		e, err := p.expr(fp.Defexpr, precNone)
		if err != nil {
			return nil, err
		}
		def = "DEFAULT " + e
	}
	return []string{mode, name, typ, def}, nil
}

func (p *printer) paramText(fp *pg.FunctionParameter) (string, error) {
	cells, err := p.paramCells(fp)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, c := range cells {
		if c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " "), nil
}
