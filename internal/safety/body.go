package safety

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"google.golang.org/protobuf/proto"
)

func stmtText(src string, raw *pg.RawStmt) string {
	start := int(raw.StmtLocation)
	end := len(src)
	if raw.StmtLen > 0 {
		end = start + int(raw.StmtLen)
	}
	return src[start:end]
}

// functionBody returns the language and the body string of a function
// with a string body, or "" when the statement is something else.
func functionBody(n *pg.Node) (lang, body string, ok bool) {
	f := n.GetCreateFunctionStmt()
	if f == nil {
		return "", "", false
	}
	for _, o := range f.Options {
		d := o.GetDefElem()
		switch d.GetDefname() {
		case "language":
			lang = d.GetArg().GetString_().GetSval()
		case "as":
			if l := d.GetArg().GetList(); l != nil && len(l.Items) == 1 {
				body, ok = l.Items[0].GetString_().GetSval(), l.Items[0].GetString_() != nil
			}
		}
	}
	return lang, body, ok
}

// clearCheckedBody blanks the body of plpgsql and sql functions; checkBody
// compares those bodies by their own parse trees.
func clearCheckedBody(f *pg.CreateFunctionStmt) {
	n := &pg.Node{Node: &pg.Node_CreateFunctionStmt{CreateFunctionStmt: f}}
	lang, _, ok := functionBody(n)
	if !ok || (lang != "plpgsql" && lang != "sql") {
		return
	}
	for _, o := range f.Options {
		if d := o.GetDefElem(); d.GetDefname() == "as" {
			d.GetArg().GetList().Items[0].GetString_().Sval = ""
		}
	}
}

// checkBody compares the bodies of two function statements.
func checkBody(inText, outText string, a, b *pg.Node) error {
	lang, bodyA, ok := functionBody(a)
	_, bodyB, _ := functionBody(b)
	if !ok || (lang != "plpgsql" && lang != "sql") {
		return nil
	}
	ca, err := comments(bodyA)
	if err != nil {
		return nil // an unscannable body was kept as it was
	}
	cb, err := comments(bodyB)
	if err != nil || !slices.Equal(ca, cb) {
		return fmt.Errorf("formatting would change the comments in the function body")
	}
	if lang == "sql" {
		ta, errA := pgquery.Parse(bodyA)
		tb, errB := pgquery.Parse(bodyB)
		if errA != nil || errB != nil {
			if bodyA != bodyB {
				return fmt.Errorf("formatting would change the function body")
			}
			return nil
		}
		if len(ta.Stmts) != len(tb.Stmts) {
			return fmt.Errorf("formatting would change the function body")
		}
		for i := range ta.Stmts {
			normalize(ta.Stmts[i].Stmt)
			normalize(tb.Stmts[i].Stmt)
			if !proto.Equal(ta.Stmts[i].Stmt, tb.Stmts[i].Stmt) {
				return fmt.Errorf("formatting would change the function body")
			}
		}
		return nil
	}

	ja, errA := pgquery.ParsePlPgSqlToJSON(inText)
	jb, errB := pgquery.ParsePlPgSqlToJSON(outText)
	if errA != nil {
		// The body does not compile as plpgsql; it must be kept verbatim.
		if bodyA != bodyB {
			return fmt.Errorf("formatting changed a function body that does not parse")
		}
		return nil
	}
	if errB != nil {
		return fmt.Errorf("formatted function body does not parse: %w", errB)
	}
	na, err := normalizeJSON(ja)
	if err != nil {
		return err
	}
	nb, err := normalizeJSON(jb)
	if err != nil {
		return err
	}
	if na != nb {
		return fmt.Errorf("formatting would change the function body")
	}
	return nil
}

func normalizeJSON(s string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "", err
	}
	v = normalizeValue(v)
	out, err := json.Marshal(v)
	return string(out), err
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		// Line numbers move with the layout. Record fields (NEW."end") and
		// the slot numbers that point at datums depend on how references
		// were spelled, not on what they mean; names, types and SQL text
		// are still compared.
		for _, k := range []string{"lineno", "varno", "dno", "recparentno", "new_varno", "old_varno", "curvar", "cursor_explicit_argrow"} {
			delete(t, k)
		}
		if q, ok := t["query"].(string); ok {
			mode, _ := t["parseMode"].(float64)
			t["query"] = normalizeQuery(q, int(mode))
		}
		if tn, ok := t["typname"].(string); ok {
			t["typname"] = normalizeTypeText(tn)
		}
		for k, x := range t {
			t[k] = normalizeValue(x)
		}
		return t
	case []any:
		out := t[:0]
		for _, x := range t {
			if m, ok := x.(map[string]any); ok && m["PLpgSQL_recfield"] != nil {
				continue
			}
			out = append(out, normalizeValue(x))
		}
		return out
	}
	return v
}

// treeKey parses sql and returns a canonical encoding of its normalized tree.
func treeKey(sql string) (string, bool) {
	tree, err := pgquery.Parse(sql)
	if err != nil {
		return "", false
	}
	for _, st := range tree.Stmts {
		normalize(st.Stmt)
		st.StmtLocation, st.StmtLen = 0, 0
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(tree)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%x", b), true
}

// Parse modes of PLpgSQL_expr, from PostgreSQL's parser.h.
const (
	rawParseDefault     = 0
	rawParsePlpgsqlExpr = 2
)

func normalizeQuery(q string, mode int) string {
	switch mode {
	case rawParseDefault:
		if k, ok := treeKey(q); ok {
			return k
		}
	case rawParsePlpgsqlExpr:
		if k, ok := treeKey("SELECT " + q); ok {
			return k
		}
	default: // assignments: target := expr, or target = expr
		res, err := pgquery.Scan(q)
		if err != nil {
			return q
		}
		depth := 0
		for _, t := range res.Tokens {
			switch t.Token {
			case pg.Token_ASCII_40, pg.Token_ASCII_91:
				depth++
			case pg.Token_ASCII_41, pg.Token_ASCII_93:
				depth--
			case pg.Token_COLON_EQUALS, pg.Token_ASCII_61:
				if depth != 0 {
					continue
				}
				target, ok1 := treeKey("SELECT " + q[:t.Start])
				value, ok2 := treeKey("SELECT " + q[t.End:])
				if ok1 && ok2 {
					return target + ":=" + value
				}
				return q
			}
		}
	}
	return q
}

func normalizeTypeText(t string) string {
	if strings.HasPrefix(t, "pg_catalog.") {
		return t
	}
	if k, ok := treeKey("SELECT NULL::" + t); ok {
		return k
	}
	t = strings.TrimSpace(t)
	if !strings.Contains(t, `"`) {
		t = strings.ToLower(t)
	}
	return t
}
