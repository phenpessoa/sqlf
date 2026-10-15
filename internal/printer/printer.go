// Package printer turns parsed PostgreSQL statements into the sqlf style.
package printer

import (
	"errors"
	"fmt"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// Token is a scanner token of the statement source, with line numbers.
type Token struct {
	Start, End  int
	Kind        pg.Token
	KeywordKind pg.KeywordKind
	Line        int // line of Start (0-based)
	EndLine     int // line of End
}

// IsComment reports whether the token is a -- or /* */ comment.
func (t Token) IsComment() bool {
	return t.Kind == pg.Token_SQL_COMMENT || t.Kind == pg.Token_C_COMMENT
}

// Stmt is one statement to print.
type Stmt struct {
	Node   *pg.Node
	Src    string  // the whole file
	Tokens []Token // tokens from the first to the last token of the statement, without the semicolon
}

// ErrUnsupported marks a statement (or part of one) that has no print rule.
// The caller falls back to light formatting.
var ErrUnsupported = errors.New("unsupported")

func unsupported(what string) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, what)
}

type printer struct {
	stmt Stmt
	// triggerRecords makes NEW and OLD print as uppercase keywords, as in a
	// trigger WHEN clause.
	triggerRecords bool
	// bExpr marks a b_expr context (column DEFAULT), where AT TIME ZONE and
	// COLLATE are not allowed without parentheses.
	bExpr bool
	// excludedRecord makes EXCLUDED print as an uppercase keyword, as in
	// ON CONFLICT DO UPDATE.
	excludedRecord bool
	// plNames makes plpgsql's own variables (NEW, FOUND, TG_OP...) print in
	// uppercase without quotes.
	plNames bool
}

// FormatSQL formats the body of a LANGUAGE sql function. The format package
// sets it; printer cannot import format.
var FormatSQL func(body string) (string, error)

// Print returns the formatted lines of a statement, ending with ";".
// It returns an error wrapping ErrUnsupported when the statement (or a
// comment inside it) has no print rule.
func Print(s Stmt) ([]string, error) {
	p := &printer{stmt: s}
	n := s.Node
	switch {
	case n.GetCreateStmt() != nil:
		return p.createTable(n.GetCreateStmt())
	case n.GetAlterTableStmt() != nil:
		return p.alterTable(n.GetAlterTableStmt())
	}

	// The remaining printers cannot place comments inside a statement.
	if p.hasComments(0, len(s.Tokens)) {
		return nil, unsupported("comment inside statement")
	}
	switch {
	case n.GetIndexStmt() != nil:
		return p.createIndex(n.GetIndexStmt())
	case n.GetCreateTrigStmt() != nil:
		return p.createTrigger(n.GetCreateTrigStmt())
	case n.GetCreateFunctionStmt() != nil:
		return p.createFunction(n.GetCreateFunctionStmt())
	case n.GetDropStmt() != nil:
		return p.drop(n.GetDropStmt())
	case n.GetCreateEnumStmt() != nil:
		return p.createEnum(n.GetCreateEnumStmt())
	case n.GetAlterEnumStmt() != nil:
		return p.alterEnum(n.GetAlterEnumStmt())
	case n.GetViewStmt() != nil:
		return p.createView(n.GetViewStmt())
	case n.GetCreateSeqStmt() != nil:
		return p.createSequence(n.GetCreateSeqStmt())
	case n.GetCreateSchemaStmt() != nil:
		return p.createSchema(n.GetCreateSchemaStmt())
	case n.GetCreateExtensionStmt() != nil:
		return p.createExtension(n.GetCreateExtensionStmt())
	case n.GetGrantStmt() != nil:
		return p.grant(n.GetGrantStmt())
	case n.GetCommentStmt() != nil:
		return p.comment(n.GetCommentStmt())
	case n.GetRenameStmt() != nil:
		return p.rename(n.GetRenameStmt())
	case n.GetSelectStmt() != nil, n.GetInsertStmt() != nil, n.GetUpdateStmt() != nil, n.GetDeleteStmt() != nil:
		lines, err := p.stmtLines(n, 0)
		if err != nil {
			return nil, err
		}
		return endStmt(lines), nil
	}
	return nil, unsupported(fmt.Sprintf("statement %T", n.Node))
}

func (p *printer) hasComments(from, to int) bool {
	for _, t := range p.stmt.Tokens[from:to] {
		if t.IsComment() {
			return true
		}
	}
	return false
}

func (p *printer) text(t Token) string {
	return p.stmt.Src[t.Start:t.End]
}

// commentText returns a comment with \r and trailing spaces removed from
// each of its lines.
func (p *printer) commentText(t Token) string {
	lines := strings.Split(p.text(t), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// endStmt appends ";" to the last line.
func endStmt(lines []string) []string {
	lines[len(lines)-1] += ";"
	return lines
}

// listComments holds the comments of a comma-separated list, assigned to
// its elements.
type listComments struct {
	leading  [][]string // full-line comments above each element
	trailing []string   // end-of-line comment of each element
	tail     []string   // full-line comments after the last element
}

// splitList splits the tokens in (open, close) at commas of depth 0 and
// assigns every comment in that range to an element. It fails when a
// comment sits inside an element, where the printer cannot keep it.
func (p *printer) splitList(open, close int) ([][]Token, listComments, error) {
	toks := p.stmt.Tokens
	var segs [][]Token
	var sepLines []int
	var cur []Token
	depth := 0
	for i := open + 1; i < close; i++ {
		t := toks[i]
		switch t.Kind {
		case pg.Token_ASCII_40, pg.Token_ASCII_91: // ( [
			depth++
		case pg.Token_ASCII_41, pg.Token_ASCII_93: // ) ]
			depth--
		case pg.Token_ASCII_44: // ,
			if depth == 0 {
				segs = append(segs, cur)
				sepLines = append(sepLines, t.Line)
				cur = nil
				continue
			}
		}
		cur = append(cur, t)
	}
	segs = append(segs, cur)

	lc := listComments{
		leading:  make([][]string, len(segs)),
		trailing: make([]string, len(segs)),
	}
	content := make([][]Token, len(segs))
	pending := []string(nil) // comments waiting for the next element
	for k, seg := range segs {
		first, last := -1, -1
		for i, t := range seg {
			if !t.IsComment() {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 {
			if len(seg) == 0 && len(segs) == 1 {
				return nil, lc, nil // empty list
			}
			return nil, lc, unsupported("empty list element")
		}
		lc.leading[k] = pending
		pending = nil
		for i, t := range seg {
			if !t.IsComment() {
				content[k] = append(content[k], t)
				continue
			}
			text := p.commentText(t)
			switch {
			case i < first:
				if k > 0 && t.Line == sepLines[k-1] && lc.trailing[k-1] == "" {
					lc.trailing[k-1] = text
				} else {
					lc.leading[k] = append(lc.leading[k], text)
				}
			case i > last:
				if t.Line == seg[last].EndLine && lc.trailing[k] == "" {
					lc.trailing[k] = text
				} else {
					pending = append(pending, text)
				}
			default:
				return nil, lc, unsupported("comment inside list element")
			}
		}
	}
	lc.tail = pending
	return content, lc, nil
}

// findToken returns the index of the first token at or after from with the
// given kind at depth 0, or -1.
func (p *printer) findToken(from int, kind pg.Token) int {
	depth := 0
	for i := from; i < len(p.stmt.Tokens); i++ {
		t := p.stmt.Tokens[i]
		if depth == 0 && t.Kind == kind {
			return i
		}
		switch t.Kind {
		case pg.Token_ASCII_40, pg.Token_ASCII_91:
			depth++
		case pg.Token_ASCII_41, pg.Token_ASCII_93:
			depth--
		}
	}
	return -1
}

// matching returns the index of the token that closes the bracket at open.
func (p *printer) matching(open int) int {
	depth := 0
	for i := open; i < len(p.stmt.Tokens); i++ {
		switch p.stmt.Tokens[i].Kind {
		case pg.Token_ASCII_40, pg.Token_ASCII_91:
			depth++
		case pg.Token_ASCII_41, pg.Token_ASCII_93:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// tokenAt returns the index of the token that starts at offset, or -1.
func (p *printer) tokenAt(offset int32) int {
	for i, t := range p.stmt.Tokens {
		if t.Start == int(offset) {
			return i
		}
	}
	return -1
}
