// Package light formats a statement that has no print rule. It keeps the
// original line breaks and only uppercases keywords and cleans whitespace.
package light

import (
	"strings"

	"github.com/phenpessoa/sqlf/internal/printer"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// Format returns the statement made of toks, ending with ";".
func Format(src string, toks []printer.Token) string {
	if len(toks) == 0 {
		return ";"
	}
	base := 0
	if first := toks[0]; onlySpaceBefore(src, first.Start) {
		base = indentOf(src, first.Start)
	}

	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			gap := src[toks[i-1].End:t.Start]
			switch nl := strings.Count(gap, "\n"); {
			case nl == 0 && gap != "":
				b.WriteString(" ")
			case nl == 1:
				b.WriteString("\n")
				b.WriteString(strings.Repeat(" ", max(indentOf(src, t.Start)-base, 0)))
			case nl > 1:
				b.WriteString("\n\n")
				b.WriteString(strings.Repeat(" ", max(indentOf(src, t.Start)-base, 0)))
			}
		}
		b.WriteString(tokenText(src, t))
	}
	b.WriteString(";")
	return b.String()
}

func tokenText(src string, t printer.Token) string {
	text := src[t.Start:t.End]
	if t.IsComment() {
		lines := strings.Split(text, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " \t\r")
		}
		return strings.Join(lines, "\n")
	}
	if t.Kind == pg.Token_NOT_EQUALS && text == "!=" {
		return "<>" // the parser reads != as <>
	}
	if t.KeywordKind != pg.KeywordKind_NO_KEYWORD && isBareWord(text) {
		return strings.ToUpper(text)
	}
	return text
}

func isBareWord(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_') {
			return false
		}
	}
	return s != ""
}

// lineStart returns the offset where the line holding off begins.
func lineStart(src string, off int) int {
	return strings.LastIndexByte(src[:off], '\n') + 1
}

func onlySpaceBefore(src string, off int) bool {
	return strings.TrimLeft(src[lineStart(src, off):off], " \t") == ""
}

// indentOf returns the indentation width of the line holding off, with
// tabs counted as 4 spaces.
func indentOf(src string, off int) int {
	w := 0
	for _, r := range src[lineStart(src, off):off] {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}
