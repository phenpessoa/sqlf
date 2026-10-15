package printer

import (
	"strings"
	"sync"

	"github.com/phenpessoa/sqlf/internal/builtins"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// quoteIdent always double-quotes an identifier. The parser has already
// folded unquoted names to lowercase, so quoting never changes meaning.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteQualified quotes every part of a dotted name.
func quoteQualified(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		quoted = append(quoted, quoteIdent(p))
	}
	return strings.Join(quoted, ".")
}

// stringList extracts the svals of a list of String nodes.
func stringList(nodes []*pg.Node) ([]string, error) {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		s := n.GetString_()
		if s == nil {
			return nil, unsupported("non-string name part")
		}
		out = append(out, s.Sval)
	}
	return out, nil
}

func quotedNameList(nodes []*pg.Node) (string, error) {
	parts, err := stringList(nodes)
	if err != nil {
		return "", err
	}
	return quoteQualified(parts...), nil
}

func rangeVar(rv *pg.RangeVar) string {
	return quoteQualified(rv.Catalogname, rv.Schemaname, rv.Relname)
}

var keywordKinds sync.Map // string -> pg.KeywordKind

// keywordKind reports how the Postgres scanner classifies a bare word.
func keywordKind(word string) pg.KeywordKind {
	if k, ok := keywordKinds.Load(word); ok {
		return k.(pg.KeywordKind)
	}
	kind := pg.KeywordKind_NO_KEYWORD
	res, err := pgquery.Scan(word)
	if err == nil && len(res.Tokens) == 1 {
		kind = res.Tokens[0].KeywordKind
	}
	keywordKinds.Store(word, kind)
	return kind
}

// bareWordOK reports whether a lowercase name can be written unquoted and
// in uppercase without changing what the parser sees, in a position where
// type and function names are allowed.
func bareWordOK(name string) bool {
	if name == "" || name != strings.ToLower(name) {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9', r == '$':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	switch keywordKind(name) {
	case pg.KeywordKind_NO_KEYWORD, pg.KeywordKind_UNRESERVED_KEYWORD, pg.KeywordKind_TYPE_FUNC_NAME_KEYWORD:
		return true
	}
	return false
}

// funcName prints a function name. Built-in functions are uppercase and
// unquoted; everything else is quoted.
func funcName(nodes []*pg.Node) (string, error) {
	parts, err := stringList(nodes)
	if err != nil {
		return "", err
	}
	if len(parts) == 1 && builtins.IsFunction(parts[0]) && bareWordOK(parts[0]) {
		return strings.ToUpper(parts[0]), nil
	}
	return quoteQualified(parts...), nil
}

// lowerBare prints names that the style keeps lowercase and unquoted
// (languages, index methods, extensions), quoting only when needed.
func lowerBare(name string) string {
	if bareWordOK(name) {
		switch keywordKind(name) {
		case pg.KeywordKind_NO_KEYWORD, pg.KeywordKind_UNRESERVED_KEYWORD:
			return name
		}
	}
	return quoteIdent(name)
}
