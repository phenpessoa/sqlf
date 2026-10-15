package printer

import (
	"strconv"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

func (p *printer) createEnum(s *pg.CreateEnumStmt) ([]string, error) {
	name, err := quotedNameList(s.TypeName)
	if err != nil {
		return nil, err
	}
	head := "CREATE TYPE " + name + " AS ENUM ("
	if len(s.Vals) == 0 {
		return []string{head + ");"}, nil
	}
	vals, err := stringList(s.Vals)
	if err != nil {
		return nil, err
	}
	lines := []string{head}
	for i, v := range vals {
		line := "    " + stringLiteral(v)
		if i < len(vals)-1 {
			line += ","
		}
		lines = append(lines, line)
	}
	return append(lines, ");"), nil
}

func (p *printer) alterEnum(s *pg.AlterEnumStmt) ([]string, error) {
	name, err := quotedNameList(s.TypeName)
	if err != nil {
		return nil, err
	}
	line := "ALTER TYPE " + name
	if s.OldVal != "" {
		line += " RENAME VALUE " + stringLiteral(s.OldVal) + " TO " + stringLiteral(s.NewVal)
		return []string{line + ";"}, nil
	}
	line += " ADD VALUE "
	if s.SkipIfNewValExists {
		line += "IF NOT EXISTS "
	}
	line += stringLiteral(s.NewVal)
	if s.NewValNeighbor != "" {
		if s.NewValIsAfter {
			line += " AFTER "
		} else {
			line += " BEFORE "
		}
		line += stringLiteral(s.NewValNeighbor)
	}
	return []string{line + ";"}, nil
}

func (p *printer) createView(s *pg.ViewStmt) ([]string, error) {
	if len(s.Options) > 0 || s.WithCheckOption != pg.ViewCheckOption_NO_CHECK_OPTION {
		return nil, unsupported("view option")
	}
	head := "CREATE "
	if s.Replace {
		head += "OR REPLACE "
	}
	switch s.View.Relpersistence {
	case "t":
		head += "TEMPORARY "
	case "p":
	default:
		return nil, unsupported("view persistence")
	}
	head += "VIEW " + rangeVar(s.View)
	if len(s.Aliases) > 0 {
		names, err := stringList(s.Aliases)
		if err != nil {
			return nil, err
		}
		head += " (" + quoteQualifiedEach(names) + ")"
	}
	body, err := p.stmtLines(s.Query, 0)
	if err != nil {
		return nil, err
	}
	return endStmt(append([]string{head + " AS"}, body...)), nil
}

// numeric prints an Integer or Float option value.
func numeric(n *pg.Node) (string, bool) {
	if i := n.GetInteger(); i != nil {
		return strconv.Itoa(int(i.Ival)), true
	}
	if f := n.GetFloat(); f != nil {
		return f.Fval, true
	}
	return "", false
}

func (p *printer) createSequence(s *pg.CreateSeqStmt) ([]string, error) {
	if s.ForIdentity || s.OwnerId != 0 {
		return nil, unsupported("sequence option")
	}
	head := "CREATE "
	switch s.Sequence.Relpersistence {
	case "u":
		head += "UNLOGGED "
	case "t":
		head += "TEMPORARY "
	}
	head += "SEQUENCE "
	if s.IfNotExists {
		head += "IF NOT EXISTS "
	}
	lines := []string{head + rangeVar(s.Sequence)}
	for _, n := range s.Options {
		d := n.GetDefElem()
		if d == nil {
			return nil, unsupported("sequence option")
		}
		opt, err := p.sequenceOption(d)
		if err != nil {
			return nil, err
		}
		lines = append(lines, opt)
	}
	return endStmt(lines), nil
}

func (p *printer) sequenceOption(d *pg.DefElem) (string, error) {
	num := func(prefix string) (string, error) {
		if v, ok := numeric(d.Arg); ok {
			return prefix + v, nil
		}
		return "", unsupported("sequence value")
	}
	switch d.Defname {
	case "as":
		typ, err := p.typeName(d.Arg.GetTypeName())
		return "AS " + typ, err
	case "increment":
		return num("INCREMENT BY ")
	case "start":
		return num("START WITH ")
	case "cache":
		return num("CACHE ")
	case "minvalue", "maxvalue":
		if d.Arg == nil {
			return "NO " + strings.ToUpper(d.Defname), nil
		}
		return num(strings.ToUpper(d.Defname) + " ")
	case "cycle":
		if b := d.Arg.GetBoolean(); b != nil {
			if b.Boolval {
				return "CYCLE", nil
			}
			return "NO CYCLE", nil
		}
	case "owned_by":
		list := d.Arg.GetList()
		if list == nil {
			return "", unsupported("OWNED BY")
		}
		names, err := stringList(list.Items)
		if err != nil {
			return "", err
		}
		if len(names) == 1 && names[0] == "none" {
			return "OWNED BY NONE", nil
		}
		return "OWNED BY " + quoteQualified(names...), nil
	}
	return "", unsupported("sequence option " + d.Defname)
}

func roleSpec(r *pg.RoleSpec) (string, error) {
	switch r.Roletype {
	case pg.RoleSpecType_ROLESPEC_CSTRING:
		return quoteIdent(r.Rolename), nil
	case pg.RoleSpecType_ROLESPEC_CURRENT_ROLE:
		return "CURRENT_ROLE", nil
	case pg.RoleSpecType_ROLESPEC_CURRENT_USER:
		return "CURRENT_USER", nil
	case pg.RoleSpecType_ROLESPEC_SESSION_USER:
		return "SESSION_USER", nil
	case pg.RoleSpecType_ROLESPEC_PUBLIC:
		return "PUBLIC", nil
	}
	return "", unsupported("role")
}

func (p *printer) createSchema(s *pg.CreateSchemaStmt) ([]string, error) {
	if len(s.SchemaElts) > 0 {
		return nil, unsupported("CREATE SCHEMA elements")
	}
	line := "CREATE SCHEMA "
	if s.IfNotExists {
		line += "IF NOT EXISTS "
	}
	if s.Schemaname != "" {
		line += quoteIdent(s.Schemaname)
	}
	if s.Authrole != nil {
		r, err := roleSpec(s.Authrole)
		if err != nil {
			return nil, err
		}
		if s.Schemaname != "" {
			line += " "
		}
		line += "AUTHORIZATION " + r
	}
	return []string{line + ";"}, nil
}

func (p *printer) createExtension(s *pg.CreateExtensionStmt) ([]string, error) {
	line := "CREATE EXTENSION "
	if s.IfNotExists {
		line += "IF NOT EXISTS "
	}
	line += lowerBare(s.Extname)
	var opts []string
	for _, n := range s.Options {
		d := n.GetDefElem()
		switch d.GetDefname() {
		case "schema":
			opts = append(opts, "SCHEMA "+quoteIdent(d.Arg.GetString_().GetSval()))
		case "new_version":
			opts = append(opts, "VERSION "+stringLiteral(d.Arg.GetString_().GetSval()))
		case "cascade":
			if b := d.Arg.GetBoolean(); b != nil && b.Boolval {
				opts = append(opts, "CASCADE")
				continue
			}
			return nil, unsupported("extension option")
		default:
			return nil, unsupported("extension option")
		}
	}
	if len(opts) > 0 {
		line += " WITH " + strings.Join(opts, " ")
	}
	return []string{line + ";"}, nil
}

func (p *printer) grant(s *pg.GrantStmt) ([]string, error) {
	if s.Grantor != nil {
		return nil, unsupported("GRANTED BY")
	}
	var privs []string
	if len(s.Privileges) == 0 {
		privs = []string{"ALL"}
	}
	for _, n := range s.Privileges {
		ap := n.GetAccessPriv()
		if ap == nil {
			return nil, unsupported("privilege")
		}
		priv := strings.ToUpper(ap.PrivName)
		if ap.PrivName == "" {
			priv = "ALL"
		}
		if len(ap.Cols) > 0 {
			names, err := stringList(ap.Cols)
			if err != nil {
				return nil, err
			}
			priv += " (" + quoteQualifiedEach(names) + ")"
		}
		privs = append(privs, priv)
	}

	var on string
	switch s.Targtype {
	case pg.GrantTargetType_ACL_TARGET_OBJECT:
		objs, err := p.grantObjects(s.Objtype, s.Objects)
		if err != nil {
			return nil, err
		}
		on = objs
	case pg.GrantTargetType_ACL_TARGET_ALL_IN_SCHEMA:
		kind := map[pg.ObjectType]string{
			pg.ObjectType_OBJECT_TABLE:     "TABLES",
			pg.ObjectType_OBJECT_SEQUENCE:  "SEQUENCES",
			pg.ObjectType_OBJECT_FUNCTION:  "FUNCTIONS",
			pg.ObjectType_OBJECT_PROCEDURE: "PROCEDURES",
			pg.ObjectType_OBJECT_ROUTINE:   "ROUTINES",
		}[s.Objtype]
		if kind == "" {
			return nil, unsupported("GRANT ALL IN SCHEMA kind")
		}
		names, err := stringList(s.Objects)
		if err != nil {
			return nil, err
		}
		on = "ALL " + kind + " IN SCHEMA " + quoteQualifiedEach(names)
	default:
		return nil, unsupported("GRANT target")
	}

	var roles []string
	for _, g := range s.Grantees {
		r := g.GetRoleSpec()
		if r == nil {
			return nil, unsupported("grantee")
		}
		name, err := roleSpec(r)
		if err != nil {
			return nil, err
		}
		roles = append(roles, name)
	}

	var line string
	if s.IsGrant {
		line = "GRANT " + strings.Join(privs, ", ") + " ON " + on + " TO " + strings.Join(roles, ", ")
		if s.GrantOption {
			line += " WITH GRANT OPTION"
		}
	} else {
		line = "REVOKE "
		if s.GrantOption {
			line += "GRANT OPTION FOR "
		}
		line += strings.Join(privs, ", ") + " ON " + on + " FROM " + strings.Join(roles, ", ") + behavior(s.Behavior)
	}
	return []string{line + ";"}, nil
}

func (p *printer) grantObjects(kind pg.ObjectType, objects []*pg.Node) (string, error) {
	var names []string
	for _, o := range objects {
		var s string
		var err error
		switch kind {
		case pg.ObjectType_OBJECT_TABLE, pg.ObjectType_OBJECT_SEQUENCE:
			rv := o.GetRangeVar()
			if rv == nil {
				return "", unsupported("grant object")
			}
			s = rangeVar(rv)
		case pg.ObjectType_OBJECT_FUNCTION, pg.ObjectType_OBJECT_PROCEDURE, pg.ObjectType_OBJECT_ROUTINE:
			s, err = p.objectWithArgs(o.GetObjectWithArgs())
		case pg.ObjectType_OBJECT_SCHEMA, pg.ObjectType_OBJECT_DATABASE:
			str := o.GetString_()
			if str == nil {
				return "", unsupported("grant object")
			}
			s = quoteIdent(str.Sval)
		case pg.ObjectType_OBJECT_TYPE, pg.ObjectType_OBJECT_DOMAIN:
			list := o.GetList()
			if list == nil {
				return "", unsupported("grant object")
			}
			s, err = quotedNameList(list.Items)
		default:
			return "", unsupported("grant object kind")
		}
		if err != nil {
			return "", err
		}
		names = append(names, s)
	}
	prefix := map[pg.ObjectType]string{
		pg.ObjectType_OBJECT_TABLE:     "",
		pg.ObjectType_OBJECT_SEQUENCE:  "SEQUENCE ",
		pg.ObjectType_OBJECT_FUNCTION:  "FUNCTION ",
		pg.ObjectType_OBJECT_PROCEDURE: "PROCEDURE ",
		pg.ObjectType_OBJECT_ROUTINE:   "ROUTINE ",
		pg.ObjectType_OBJECT_SCHEMA:    "SCHEMA ",
		pg.ObjectType_OBJECT_DATABASE:  "DATABASE ",
		pg.ObjectType_OBJECT_TYPE:      "TYPE ",
		pg.ObjectType_OBJECT_DOMAIN:    "DOMAIN ",
	}[kind]
	return prefix + strings.Join(names, ", "), nil
}

func (p *printer) comment(s *pg.CommentStmt) ([]string, error) {
	kinds := map[pg.ObjectType]string{
		pg.ObjectType_OBJECT_TABLE:         "TABLE",
		pg.ObjectType_OBJECT_COLUMN:        "COLUMN",
		pg.ObjectType_OBJECT_VIEW:          "VIEW",
		pg.ObjectType_OBJECT_MATVIEW:       "MATERIALIZED VIEW",
		pg.ObjectType_OBJECT_INDEX:         "INDEX",
		pg.ObjectType_OBJECT_SEQUENCE:      "SEQUENCE",
		pg.ObjectType_OBJECT_SCHEMA:        "SCHEMA",
		pg.ObjectType_OBJECT_EXTENSION:     "EXTENSION",
		pg.ObjectType_OBJECT_TYPE:          "TYPE",
		pg.ObjectType_OBJECT_DOMAIN:        "DOMAIN",
		pg.ObjectType_OBJECT_FUNCTION:      "FUNCTION",
		pg.ObjectType_OBJECT_PROCEDURE:     "PROCEDURE",
		pg.ObjectType_OBJECT_TRIGGER:       "TRIGGER",
		pg.ObjectType_OBJECT_TABCONSTRAINT: "CONSTRAINT",
	}
	kind, ok := kinds[s.Objtype]
	if !ok {
		return nil, unsupported("COMMENT ON " + s.Objtype.String())
	}
	var obj string
	var err error
	switch s.Objtype {
	case pg.ObjectType_OBJECT_TRIGGER, pg.ObjectType_OBJECT_TABCONSTRAINT:
		obj, err = p.dropObject(pg.ObjectType_OBJECT_TRIGGER, s.Object)
	default:
		obj, err = p.dropObject(s.Objtype, s.Object)
	}
	if err != nil {
		return nil, err
	}
	text := "NULL"
	if s.Comment != "" {
		text = stringLiteral(s.Comment)
	}
	return []string{"COMMENT ON " + kind + " " + obj + " IS " + text + ";"}, nil
}

// rename prints ALTER TABLE ... RENAME in the ALTER TABLE shape: the
// header line, then the action at column 0.
func (p *printer) rename(s *pg.RenameStmt) ([]string, error) {
	if s.RelationType != pg.ObjectType_OBJECT_TABLE && s.RenameType != pg.ObjectType_OBJECT_TABLE {
		return nil, unsupported("RENAME of non-table")
	}
	head := "ALTER TABLE "
	if s.MissingOk {
		head += "IF EXISTS "
	}
	if s.Relation == nil {
		return nil, unsupported("RENAME without table")
	}
	if !s.Relation.Inh {
		head += "ONLY "
	}
	head += rangeVar(s.Relation)
	var action string
	switch s.RenameType {
	case pg.ObjectType_OBJECT_TABLE:
		action = "RENAME TO " + quoteIdent(s.Newname)
	case pg.ObjectType_OBJECT_COLUMN:
		action = "RENAME COLUMN " + quoteIdent(s.Subname) + " TO " + quoteIdent(s.Newname)
	case pg.ObjectType_OBJECT_TABCONSTRAINT:
		action = "RENAME CONSTRAINT " + quoteIdent(s.Subname) + " TO " + quoteIdent(s.Newname)
	default:
		return nil, unsupported("RENAME kind")
	}
	return []string{head, action + ";"}, nil
}
