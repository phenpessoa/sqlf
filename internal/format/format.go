// Package format formats a whole .sql file.
package format

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"github.com/wasilibs/go-pgquery/parser"

	"github.com/phenpessoa/sqlf/internal/light"
	"github.com/phenpessoa/sqlf/internal/printer"
	"github.com/phenpessoa/sqlf/internal/safety"
)

// Error is a formatting failure. Line and Col are 1-based; they are 0 when
// the error has no position.
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}

func init() {
	printer.FormatSQL = Format
}

// Format returns src in the sqlf style.
func Format(src string) (string, error) {
	src = strings.TrimPrefix(src, "\xef\xbb\xbf")

	tree, err := pgquery.Parse(src)
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) && pe.Cursorpos > 0 {
			line, col := position(src, pe.Cursorpos-1)
			return "", &Error{Line: line, Col: col, Msg: pe.Message}
		}
		return "", &Error{Msg: err.Error()}
	}
	toks, err := printer.Tokenize(src)
	if err != nil {
		return "", &Error{Msg: err.Error()}
	}
	items, err := buildItems(src, tree.Stmts, toks)
	if err != nil {
		return "", err
	}
	out := render(src, items)
	if err := safety.Check(src, out); err != nil {
		return "", &Error{Msg: "internal error: " + err.Error() + "; file left unchanged"}
	}
	return out, nil
}

// position converts a byte offset to a 1-based line and column.
func position(src string, off int) (int, int) {
	off = min(off, len(src))
	line := strings.Count(src[:off], "\n") + 1
	col := off - (strings.LastIndexByte(src[:off], '\n') + 1) + 1
	return line, col
}

type itemKind int

const (
	itemStmt itemKind = iota
	itemComment
)

// item is a top-level statement or comment, in source order.
type item struct {
	kind       itemKind
	start, end int // source span; for statements, end includes the semicolon
	stmt       printer.Stmt
	text       string // comment text
}

func buildItems(src string, raws []*pg.RawStmt, toks []printer.Token) ([]item, error) {
	inStmt := make([]bool, len(toks))
	var items []item
	for _, raw := range raws {
		lo := int(raw.StmtLocation)
		hi := len(src)
		if raw.StmtLen > 0 {
			hi = lo + int(raw.StmtLen)
		}
		first, last := -1, -1
		for i, t := range toks {
			if t.IsComment() || t.Start < lo || t.End > hi {
				continue
			}
			if first < 0 {
				first = i
			}
			last = i
		}
		for last >= first && first >= 0 && toks[last].Kind == pg.Token_ASCII_59 {
			last--
		}
		if last < first {
			first = -1
		}
		if first < 0 {
			return nil, &Error{Msg: "internal error: statement without tokens"}
		}
		end := toks[last].End
		for i := first; i <= last; i++ {
			inStmt[i] = true
		}
		if last+1 < len(toks) && toks[last+1].Kind == pg.Token_ASCII_59 {
			inStmt[last+1] = true
			end = toks[last+1].End
		}
		items = append(items, item{
			kind:  itemStmt,
			start: toks[first].Start,
			end:   end,
			stmt:  printer.Stmt{Node: raw.Stmt, Src: src, Tokens: toks[first : last+1]},
		})
	}
	for i, t := range toks {
		if inStmt[i] {
			continue
		}
		if !t.IsComment() {
			if t.Kind == pg.Token_ASCII_59 {
				continue // a stray semicolon
			}
			return nil, &Error{Msg: "internal error: token outside any statement"}
		}
		items = append(items, item{kind: itemComment, start: t.Start, end: t.End, text: commentText(src[t.Start:t.End])})
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].start < items[b].start })
	return items, nil
}

func commentText(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// unit is a group of lines separated from the next unit by one blank line.
type unit struct {
	leading []string
	body    []string
	inline  string // comment on the same line as the end of the statement
	trailer []string
	isStmt  bool
}

func newlines(src string, from, to int) int {
	if from < 0 || to < from {
		return 2
	}
	return strings.Count(src[from:to], "\n")
}

func render(src string, items []item) string {
	var units []*unit
	var lastStmt *unit
	prevEnd := -1 // end of the previous item
	var pending []string

	flushStandalone := func() {
		if len(pending) > 0 {
			units = append(units, &unit{body: pending})
			pending = nil
		}
	}

	for i := 0; i < len(items); i++ {
		it := items[i]
		if it.kind == itemStmt {
			u := &unit{isStmt: true, leading: pending, body: printStmt(it.stmt)}
			pending = nil
			units = append(units, u)
			lastStmt = u
			prevEnd = it.end
			continue
		}

		// A comment on the same line as the end of a statement stays there.
		if lastStmt != nil && prevEnd >= 0 && units[len(units)-1] == lastStmt &&
			lastStmt.inline == "" && len(lastStmt.trailer) == 0 && newlines(src, prevEnd, it.start) == 0 {
			lastStmt.inline = it.text
			prevEnd = it.end
			continue
		}

		// Collect the block of comments that touch each other.
		gapBefore := newlines(src, prevEnd, it.start)
		block := []string{it.text}
		end := it.end
		for i+1 < len(items) && items[i+1].kind == itemComment && newlines(src, end, items[i+1].start) <= 1 {
			i++
			block = append(block, items[i].text)
			end = items[i].end
		}
		gluedAbove := units != nil && units[len(units)-1] == lastStmt && lastStmt != nil && gapBefore <= 1
		gluedBelow := i+1 < len(items) && items[i+1].kind == itemStmt && newlines(src, end, items[i+1].start) <= 1
		prevEnd = end

		switch {
		case gluedBelow:
			flushStandalone()
			pending = block
		case gluedAbove:
			lastStmt.trailer = append(lastStmt.trailer, block...)
		default:
			flushStandalone()
			units = append(units, &unit{body: block})
			lastStmt = nil
		}
	}
	flushStandalone()

	var b strings.Builder
	for i, u := range units {
		if i > 0 {
			b.WriteString("\n\n")
		}
		var lines []string
		lines = append(lines, u.leading...)
		body := append([]string(nil), u.body...)
		if u.inline != "" {
			body[len(body)-1] += " " + u.inline
		}
		lines = append(lines, body...)
		lines = append(lines, u.trailer...)
		b.WriteString(strings.Join(lines, "\n"))
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

func printStmt(s printer.Stmt) []string {
	lines, err := printer.Print(s)
	if err != nil {
		return strings.Split(light.Format(s.Src, s.Tokens), "\n")
	}
	return lines
}
