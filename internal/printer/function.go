package printer

import (
	"strconv"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

func (p *printer) createFunction(s *pg.CreateFunctionStmt) ([]string, error) {
	if s.SqlBody != nil {
		return nil, unsupported("SQL-standard function body")
	}
	head := "CREATE "
	if s.Replace {
		head += "OR REPLACE "
	}
	if s.IsProcedure {
		head += "PROCEDURE "
	} else {
		head += "FUNCTION "
	}
	name, err := quotedNameList(s.Funcname)
	if err != nil {
		return nil, err
	}
	head += name

	var lines []string
	if len(s.Parameters) == 0 {
		lines = []string{head + "()"}
	} else {
		rows := make([]gridRow, len(s.Parameters))
		for i, n := range s.Parameters {
			fp := n.GetFunctionParameter()
			if fp == nil {
				return nil, unsupported("function parameter")
			}
			cells, err := p.paramCells(fp)
			if err != nil {
				return nil, err
			}
			rows[i] = gridRow{cells: cells, suffix: ","}
		}
		rows[len(rows)-1].suffix = ""
		lines = append([]string{head + "("}, renderGrid(4, rows)...)
		lines = append(lines, ")")
	}

	var body, language string
	var opts []string
	for _, n := range s.Options {
		d := n.GetDefElem()
		if d == nil {
			return nil, unsupported("function option")
		}
		switch d.Defname {
		case "as":
			list := d.Arg.GetList()
			if list == nil || len(list.Items) != 1 || list.Items[0].GetString_() == nil {
				return nil, unsupported("function body")
			}
			body = list.Items[0].GetString_().Sval
		case "language":
			str := d.Arg.GetString_()
			if str == nil {
				return nil, unsupported("language")
			}
			language = lowerBare(str.Sval)
		default:
			opt, err := p.functionOption(d)
			if err != nil {
				return nil, err
			}
			opts = append(opts, opt)
		}
	}
	if language == "" {
		return nil, unsupported("function without LANGUAGE")
	}

	tail := ""
	if s.ReturnType != nil {
		for _, n := range s.Parameters {
			if n.GetFunctionParameter().Mode == pg.FunctionParameterMode_FUNC_PARAM_TABLE {
				return nil, unsupported("RETURNS TABLE")
			}
		}
		ret, err := p.typeName(s.ReturnType)
		if err != nil {
			return nil, err
		}
		tail = "RETURNS " + ret + " "
	}
	switch language {
	case "plpgsql":
		// A body that plpgsql rejects stays verbatim: the safety check can
		// only compare bodies that compile.
		if _, err := pgquery.ParsePlPgSqlToJSON(p.sourceText()); err == nil {
			if formatted, err := p.plpgsqlBody(body); err == nil {
				body = formatted
			}
		}
	case "sql":
		if FormatSQL != nil && strings.TrimSpace(body) != "" {
			if formatted, err := FormatSQL(body); err == nil {
				body = "\n" + formatted
			}
		}
	}
	tag := dollarTag(body)
	tail += "AS " + tag + body + tag + " LANGUAGE " + language
	for _, o := range opts {
		tail += " " + o
	}
	tail += ";"
	return append(lines, strings.Split(tail, "\n")...), nil
}

// dollarTag picks a dollar-quote tag that does not occur in the body.
func dollarTag(body string) string {
	safe := func(tag string) bool {
		return !strings.Contains(body, tag) && !strings.HasSuffix(body, "$")
	}
	if safe("$$") {
		return "$$"
	}
	if safe("$body$") {
		return "$body$"
	}
	for i := 1; ; i++ {
		tag := "$body" + strconv.Itoa(i) + "$"
		if !strings.Contains(body, tag) {
			return tag
		}
	}
}

func (p *printer) functionOption(d *pg.DefElem) (string, error) {
	str := func() string {
		if s := d.Arg.GetString_(); s != nil {
			return s.Sval
		}
		return ""
	}
	boolean := func() (bool, bool) {
		if b := d.Arg.GetBoolean(); b != nil {
			return b.Boolval, true
		}
		return false, false
	}
	number := func() string {
		if i := d.Arg.GetInteger(); i != nil {
			return strconv.Itoa(int(i.Ival))
		}
		if f := d.Arg.GetFloat(); f != nil {
			return f.Fval
		}
		return ""
	}
	switch d.Defname {
	case "volatility":
		switch v := str(); v {
		case "immutable", "stable", "volatile":
			return strings.ToUpper(v), nil
		}
	case "parallel":
		switch v := str(); v {
		case "safe", "restricted", "unsafe":
			return "PARALLEL " + strings.ToUpper(v), nil
		}
	case "strict":
		if v, ok := boolean(); ok {
			if v {
				return "STRICT", nil
			}
			return "CALLED ON NULL INPUT", nil
		}
	case "security":
		if v, ok := boolean(); ok {
			if v {
				return "SECURITY DEFINER", nil
			}
			return "SECURITY INVOKER", nil
		}
	case "leakproof":
		if v, ok := boolean(); ok {
			if v {
				return "LEAKPROOF", nil
			}
			return "NOT LEAKPROOF", nil
		}
	case "window":
		if v, ok := boolean(); ok && v {
			return "WINDOW", nil
		}
	case "cost", "rows":
		if n := number(); n != "" {
			return strings.ToUpper(d.Defname) + " " + n, nil
		}
	}
	return "", unsupported("function option " + d.Defname)
}

// sourceText returns the source of the statement being printed.
func (p *printer) sourceText() string {
	toks := p.stmt.Tokens
	if len(toks) == 0 {
		return ""
	}
	return p.stmt.Src[toks[0].Start:toks[len(toks)-1].End]
}
