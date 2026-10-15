package printer

import (
	"slices"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// plSpecialNames are the variables plpgsql creates on its own. The style
// writes them in uppercase without quotes.
var plSpecialNames = map[string]bool{
	"new": true, "old": true, "found": true, "sqlstate": true, "sqlerrm": true,
	"tg_op": true, "tg_name": true, "tg_when": true, "tg_level": true, "tg_relid": true,
	"tg_table_name": true, "tg_table_schema": true, "tg_nargs": true, "tg_argv": true,
}

// plKeywords are plpgsql words the core scanner does not mark as keywords.
// Light-formatted plpgsql statements print them in uppercase.
var plKeywords = map[string]bool{
	"raise": true, "notice": true, "warning": true, "info": true, "log": true, "debug": true,
	"exception": true, "perform": true, "elsif": true, "elseif": true, "loop": true, "exit": true,
	"foreach": true, "slice": true, "reverse": true, "get": true, "diagnostics": true,
	"stacked": true, "open": true, "fetch": true, "move": true, "close": true, "assert": true,
	"return": true, "next": true, "query": true, "execute": true, "strict": true, "alias": true,
	"constant": true, "cursor": true, "scroll": true, "using": true, "errcode": true,
	"message": true, "detail": true, "hint": true,
}

// plFormatter formats a plpgsql body from its scanner tokens.
type plFormatter struct {
	p        *printer
	src      string
	toks     []Token
	i        int
	out      []string
	lastLine int // source line of the last consumed token
}

// plpgsqlBody formats a plpgsql function body. The result starts and ends
// with a newline. On error the caller keeps the body as it was.
func (p *printer) plpgsqlBody(body string) (string, error) {
	toks, err := Tokenize(body)
	if err != nil {
		return "", err
	}
	sub := &printer{plNames: true}
	f := &plFormatter{p: sub, src: body, toks: toks, lastLine: -1}
	f.comments(0)
	if err := f.block(0); err != nil {
		return "", err
	}
	f.comments(0)
	if f.i != len(f.toks) {
		return "", unsupported("text after the function body")
	}
	return "\n" + strings.Join(f.out, "\n") + "\n", nil
}

func (f *plFormatter) done() bool { return f.i >= len(f.toks) }

func (f *plFormatter) tokText(i int) string { return f.src[f.toks[i].Start:f.toks[i].End] }

// word returns the lowercase text of a bare word token, or "".
func (f *plFormatter) word(i int) string {
	if i >= len(f.toks) {
		return ""
	}
	t := f.toks[i]
	if t.Kind != pg.Token_IDENT && t.KeywordKind == pg.KeywordKind_NO_KEYWORD {
		return ""
	}
	s := f.tokText(i)
	if strings.HasPrefix(s, `"`) {
		return ""
	}
	return strings.ToLower(s)
}

func (f *plFormatter) at(words ...string) bool {
	w := f.word(f.i)
	return slices.Contains(words, w)
}

func (f *plFormatter) expect(word string) error {
	if !f.at(word) {
		return unsupported("plpgsql: expected " + word)
	}
	f.consume()
	return nil
}

func (f *plFormatter) consume() {
	f.lastLine = f.toks[f.i].EndLine
	f.i++
}

// isName reports whether token i can be a variable or column name: an
// identifier or a non-reserved keyword.
func (f *plFormatter) isName(i int) bool {
	if i >= len(f.toks) {
		return false
	}
	t := f.toks[i]
	switch {
	case t.Kind == pg.Token_IDENT:
		return true
	case t.KeywordKind == pg.KeywordKind_UNRESERVED_KEYWORD, t.KeywordKind == pg.KeywordKind_COL_NAME_KEYWORD:
		return true
	}
	return false
}

func (f *plFormatter) isKind(i int, k pg.Token) bool {
	return i < len(f.toks) && f.toks[i].Kind == k
}

func (f *plFormatter) emit(indent int, s string) {
	f.out = append(f.out, spaces(indent)+s)
}

// comments prints the comments at the cursor: on the previous line when
// they start on the same source line, else on their own lines.
func (f *plFormatter) comments(indent int) {
	for !f.done() && f.toks[f.i].IsComment() {
		t := f.toks[f.i]
		text := f.p.commentTextOf(f.src, t)
		if t.Line == f.lastLine && len(f.out) > 0 {
			f.out[len(f.out)-1] += " " + text
		} else {
			f.emit(indent, text)
		}
		f.consume()
	}
}

// scanUntil returns the index of the first token at depth 0 for which stop
// is true, starting at from. Parentheses, brackets and CASE ... END nest.
func (f *plFormatter) scanUntil(from int, stop func(i int) bool) int {
	depth, cases := 0, 0
	for i := from; i < len(f.toks); i++ {
		if depth == 0 && cases == 0 && stop(i) {
			return i
		}
		switch f.toks[i].Kind {
		case pg.Token_ASCII_40, pg.Token_ASCII_91:
			depth++
		case pg.Token_ASCII_41, pg.Token_ASCII_93:
			depth--
		}
		switch f.word(i) {
		case "case":
			cases++
		case "end":
			if cases > 0 {
				cases--
			}
		}
	}
	return -1
}

func (f *plFormatter) untilWord(from int, words ...string) int {
	return f.scanUntil(from, func(i int) bool {
		w := f.word(i)
		return slices.Contains(words, w)
	})
}

func (f *plFormatter) untilSemicolon(from int) int {
	return f.scanUntil(from, func(i int) bool { return f.toks[i].Kind == pg.Token_ASCII_59 })
}

func (f *plFormatter) text(from, to int) string {
	if from >= to {
		return ""
	}
	return f.src[f.toks[from].Start:f.toks[to-1].End]
}

func (f *plFormatter) hasComment(from, to int) bool {
	for i := from; i < to; i++ {
		if f.toks[i].IsComment() {
			return true
		}
	}
	return false
}

// expr prints the expression in tokens [from, to) starting at column col.
// It falls back to light formatting when the printer cannot handle it.
func (f *plFormatter) expr(from, to, col, indent int) ([]string, error) {
	if from >= to {
		return nil, unsupported("plpgsql: empty expression")
	}
	if !f.hasComment(from, to) {
		if lines, err := f.p.exprText(f.text(from, to), col, indent); err == nil {
			return lines, nil
		}
	}
	return f.light(from, to, indent), nil
}

// light joins tokens with single spaces, uppercasing keywords. A -- comment
// ends its line; the next token continues at indent+4.
func (f *plFormatter) light(from, to, indent int) []string {
	lines := []string{""}
	for i := from; i < to; i++ {
		t := f.toks[i]
		s := f.tokText(i)
		switch {
		case t.IsComment():
			s = f.p.commentTextOf(f.src, t)
		case t.Kind == pg.Token_NOT_EQUALS && s == "!=":
			s = "<>"
		case f.word(i) != "" && (t.KeywordKind != pg.KeywordKind_NO_KEYWORD || plKeywords[f.word(i)] || plSpecialNames[f.word(i)]):
			s = strings.ToUpper(s)
		}
		cur := &lines[len(lines)-1]
		if i > from && *cur != "" && f.toks[i-1].End < t.Start {
			*cur += " "
		}
		*cur += s
		if t.Kind == pg.Token_SQL_COMMENT && i < to-1 {
			lines = append(lines, spaces(indent+4))
		}
	}
	return lines
}

func (f *plFormatter) emitLines(indent int, prefix string, lines []string, suffix string) {
	lines = joinLines([]string{spaces(indent) + prefix}, lines)
	lines[len(lines)-1] += suffix
	f.out = append(f.out, lines...)
}

// label reads <<name>> at the cursor.
func (f *plFormatter) label() (string, bool) {
	if f.done() || f.tokText(f.i) != "<<" || f.i+2 >= len(f.toks) || f.tokText(f.i+2) != ">>" {
		return "", false
	}
	return f.tokText(f.i + 1), true
}

func (f *plFormatter) block(indent int) error {
	if name, ok := f.label(); ok {
		f.emit(indent, "<<"+name+">>")
		f.consume()
		f.consume()
		f.consume()
		f.comments(indent)
	}
	if f.at("declare") {
		f.emit(indent, "DECLARE")
		f.consume()
		if err := f.decls(indent + 4); err != nil {
			return err
		}
	}
	if err := f.expect("begin"); err != nil {
		return err
	}
	f.emit(indent, "BEGIN")
	if err := f.stmts(indent+4, "end", "exception"); err != nil {
		return err
	}
	if f.at("exception") {
		f.emit(indent, "EXCEPTION")
		f.consume()
		for {
			f.comments(indent + 4)
			if !f.at("when") {
				break
			}
			f.consume()
			then := f.untilWord(f.i, "then")
			if then < 0 {
				return unsupported("plpgsql: WHEN without THEN")
			}
			cond := f.light(f.i, then, indent+4)
			f.i = then
			f.consume()
			f.emitLines(indent+4, "WHEN ", cond, " THEN")
			if err := f.stmts(indent+8, "when", "end"); err != nil {
				return err
			}
		}
	}
	return f.end(indent, "")
}

// end reads END [what] [label] ; and prints it.
func (f *plFormatter) end(indent int, what string) error {
	if err := f.expect("end"); err != nil {
		return err
	}
	line := "END"
	if what != "" {
		if err := f.expect(what); err != nil {
			return err
		}
		line += " " + strings.ToUpper(what)
	}
	if !f.done() && !f.isKind(f.i, pg.Token_ASCII_59) && !f.toks[f.i].IsComment() {
		line += " " + f.tokText(f.i)
		f.consume()
	}
	if f.isKind(f.i, pg.Token_ASCII_59) {
		f.consume()
	} else if what != "" {
		return unsupported("plpgsql: END without semicolon")
	}
	f.emit(indent, line+";")
	return nil
}

func (f *plFormatter) decls(indent int) error {
	var rows []gridRow
	var pending []string
	flush := func() {
		if len(rows) > 0 {
			f.out = append(f.out, renderGrid(indent, rows)...)
			rows = nil
		}
	}
	for {
		for !f.done() && f.toks[f.i].IsComment() {
			t := f.toks[f.i]
			text := f.p.commentTextOf(f.src, t)
			if t.Line == f.lastLine && len(rows) > 0 && rows[len(rows)-1].comment == "" {
				rows[len(rows)-1].comment = text
			} else {
				pending = append(pending, text)
			}
			f.consume()
		}
		if f.done() {
			return unsupported("plpgsql: DECLARE without BEGIN")
		}
		if f.at("begin") {
			if len(pending) > 0 {
				flush()
				for _, c := range pending {
					f.emit(indent, c)
				}
			}
			flush()
			return nil
		}
		semi := f.untilSemicolon(f.i)
		if semi < 0 {
			return unsupported("plpgsql: declaration without semicolon")
		}
		row, ok := f.decl(f.i, semi)
		if ok {
			row.leading = pending
			rows = append(rows, row)
		} else {
			flush()
			for _, c := range pending {
				f.emit(indent, c)
			}
			f.emitLines(indent, "", f.light(f.i, semi, indent), ";")
		}
		pending = nil
		f.i = semi
		f.consume()
	}
}

// decl parses name [CONSTANT] type [COLLATE c] [NOT NULL] [{:= | = | DEFAULT} expr]
// in tokens [from, to) into grid cells. It reports false for other forms
// (ALIAS, CURSOR), which print light.
func (f *plFormatter) decl(from, to int) (gridRow, bool) {
	if f.hasComment(from, to) || to-from < 2 || !f.isName(from) {
		return gridRow{}, false
	}
	for i := from; i < to; i++ {
		if w := f.word(i); w == "alias" || w == "cursor" || w == "refcursor" && i == from+1 {
			return gridRow{}, false
		}
	}
	name := quoteIdent(identValue(f.tokText(from)))
	k := from + 1
	var typeParts []string
	if f.word(k) == "constant" {
		typeParts = append(typeParts, "CONSTANT")
		k++
	}
	sep := f.scanUntil(k, func(i int) bool {
		return i >= to || f.toks[i].Kind == pg.Token_COLON_EQUALS || f.toks[i].Kind == pg.Token_ASCII_61 ||
			f.word(i) == "default"
	})
	if sep < 0 || sep > to {
		sep = to
	}
	typeEnd := sep
	notNull := false
	if typeEnd-k >= 2 && f.word(typeEnd-2) == "not" && f.word(typeEnd-1) == "null" {
		notNull = true
		typeEnd -= 2
	}
	collate := ""
	for i := k; i < typeEnd; i++ {
		if f.word(i) == "collate" {
			collate = " COLLATE " + quoteIdent(identValue(f.text(i+1, typeEnd)))
			if typeEnd-i != 2 {
				return gridRow{}, false
			}
			typeEnd = i
			break
		}
	}
	if typeEnd <= k {
		return gridRow{}, false
	}
	typ, err := f.p.typeText(f.text(k, typeEnd))
	if err != nil {
		if typ, err = f.pctType(k, typeEnd); err != nil {
			typ = strings.Join(f.light(k, typeEnd, 0), " ")
		}
	}
	typeParts = append(typeParts, typ+collate)
	if notNull {
		typeParts = append(typeParts, "NOT NULL")
	}
	def := ""
	if sep < to {
		lines, err := f.expr(sep+1, to, 0, 0)
		if err != nil || len(lines) != 1 {
			return gridRow{}, false
		}
		def = ":= " + lines[0]
	}
	return gridRow{cells: []string{name, strings.Join(typeParts, " "), def}, suffix: ";"}, true
}

// identValue returns the value of an identifier token: quoted ones lose
// their quotes, unquoted ones fold to lowercase.
func identValue(s string) string {
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && len(s) >= 2 {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return strings.ToLower(s)
}

func (f *plFormatter) stmts(indent int, stops ...string) error {
	for {
		f.comments(indent)
		if f.done() {
			return unsupported("plpgsql: unterminated block")
		}
		if f.at(stops...) {
			return nil
		}
		if err := f.stmt(indent); err != nil {
			return err
		}
	}
}

func (f *plFormatter) stmt(indent int) error {
	if name, ok := f.label(); ok {
		if w := f.word(f.i + 3); w == "declare" || w == "begin" {
			return f.block(indent)
		}
		f.emit(indent, "<<"+name+">>")
		f.consume()
		f.consume()
		f.consume()
		f.comments(indent)
	}
	switch f.word(f.i) {
	case "declare", "begin":
		return f.block(indent)
	case "if":
		return f.ifStmt(indent)
	case "case":
		return f.caseStmt(indent)
	case "loop":
		f.consume()
		f.emit(indent, "LOOP")
		return f.loopBody(indent)
	case "while":
		f.consume()
		loop := f.untilWord(f.i, "loop")
		if loop < 0 {
			return unsupported("plpgsql: WHILE without LOOP")
		}
		cond, err := f.expr(f.i, loop, indent+6, indent)
		if err != nil {
			return err
		}
		f.emitLines(indent, "WHILE ", cond, " LOOP")
		f.i = loop
		f.consume()
		return f.loopBody(indent)
	case "for":
		return f.forStmt(indent)
	case "exit", "continue":
		return f.exitStmt(indent)
	case "return":
		return f.returnStmt(indent)
	case "perform":
		return f.perform(indent)
	case "null":
		if f.isKind(f.i+1, pg.Token_ASCII_59) {
			f.consume()
			f.consume()
			f.emit(indent, "NULL;")
			return nil
		}
	case "select", "insert", "update", "delete", "with":
		return f.sqlStmt(indent)
	}
	if ok, err := f.assignment(indent); ok || err != nil {
		return err
	}
	return f.lightStmt(indent)
}

func (f *plFormatter) lightStmt(indent int) error {
	semi := f.untilSemicolon(f.i)
	if semi < 0 {
		return unsupported("plpgsql: statement without semicolon")
	}
	f.emitLines(indent, "", f.light(f.i, semi, indent), ";")
	f.i = semi
	f.consume()
	return nil
}

func (f *plFormatter) loopBody(indent int) error {
	if err := f.stmts(indent+4, "end"); err != nil {
		return err
	}
	return f.end(indent, "loop")
}

func (f *plFormatter) ifStmt(indent int) error {
	head := "IF "
	for {
		f.consume() // IF or ELSIF
		then := f.untilWord(f.i, "then")
		if then < 0 {
			return unsupported("plpgsql: IF without THEN")
		}
		cond, err := f.expr(f.i, then, indent+len(head), indent)
		if err != nil {
			return err
		}
		f.emitLines(indent, head, cond, " THEN")
		f.i = then
		f.consume()
		if err := f.stmts(indent+4, "elsif", "elseif", "else", "end"); err != nil {
			return err
		}
		if !f.at("elsif", "elseif") {
			break
		}
		head = "ELSIF "
	}
	if f.at("else") {
		f.consume()
		f.emit(indent, "ELSE")
		if err := f.stmts(indent+4, "end"); err != nil {
			return err
		}
	}
	return f.end(indent, "if")
}

func (f *plFormatter) caseStmt(indent int) error {
	f.consume()
	head := "CASE"
	if !f.at("when") {
		when := f.untilWord(f.i, "when")
		if when < 0 {
			return unsupported("plpgsql: CASE without WHEN")
		}
		e, err := f.expr(f.i, when, indent+5, indent)
		if err != nil || len(e) != 1 {
			return unsupported("plpgsql: CASE expression")
		}
		head += " " + e[0]
		f.i = when
	}
	f.emit(indent, head)
	for {
		f.comments(indent + 4)
		if !f.at("when") {
			break
		}
		f.consume()
		then := f.untilWord(f.i, "then")
		if then < 0 {
			return unsupported("plpgsql: WHEN without THEN")
		}
		list := f.light(f.i, then, indent+4)
		if !f.hasComment(f.i, then) {
			if s, err := f.p.exprListText(f.text(f.i, then)); err == nil {
				list = []string{s}
			}
		}
		f.emitLines(indent+4, "WHEN ", list, " THEN")
		f.i = then
		f.consume()
		if err := f.stmts(indent+8, "when", "else", "end"); err != nil {
			return err
		}
	}
	if f.at("else") {
		f.consume()
		f.emit(indent+4, "ELSE")
		if err := f.stmts(indent+8, "end"); err != nil {
			return err
		}
	}
	return f.end(indent, "case")
}

func (f *plFormatter) forStmt(indent int) error {
	f.consume()
	loop := f.untilWord(f.i, "loop")
	in := f.untilWord(f.i, "in")
	if loop < 0 || in < 0 || in > loop || f.hasComment(f.i, loop) {
		return f.lightHeader(indent, loop)
	}
	var targets []string
	for i := f.i; i < in; i++ {
		switch {
		case f.toks[i].Kind == pg.Token_ASCII_44:
		case f.isName(i):
			targets = append(targets, quoteIdent(identValue(f.tokText(i))))
		default:
			return f.lightHeader(indent, loop)
		}
	}
	head := "FOR " + strings.Join(targets, ", ") + " IN "
	start := in + 1
	if f.word(start) == "reverse" {
		head += "REVERSE "
		start++
	}
	dots := f.scanUntil(start, func(i int) bool { return i >= loop || f.toks[i].Kind == pg.Token_DOT_DOT })
	switch {
	case dots >= 0 && dots < loop:
		by := f.untilWord(dots+1, "by")
		upperEnd := loop
		if by >= 0 && by < loop {
			upperEnd = by
		}
		lower, err1 := f.p.exprText(f.text(start, dots), 0, 0)
		upper, err2 := f.p.exprText(f.text(dots+1, upperEnd), 0, 0)
		if err1 != nil || err2 != nil || len(lower) != 1 || len(upper) != 1 {
			return f.lightHeader(indent, loop)
		}
		head += lower[0] + ".." + upper[0]
		if upperEnd != loop {
			step, err := f.p.exprText(f.text(by+1, loop), 0, 0)
			if err != nil || len(step) != 1 {
				return f.lightHeader(indent, loop)
			}
			head += " BY " + step[0]
		}
		f.emit(indent, head+" LOOP")
	case f.word(start) == "execute":
		return f.lightHeader(indent, loop)
	default:
		query, err := f.p.sqlText(f.text(start, loop), indent+4)
		if err != nil {
			return f.lightHeader(indent, loop)
		}
		f.emit(indent, strings.TrimSuffix(head, " "))
		f.out = append(f.out, query...)
		f.emit(indent, "LOOP")
	}
	f.i = loop
	f.consume()
	return f.loopBody(indent)
}

// lightHeader prints FOR/FOREACH ... LOOP headers it cannot parse.
func (f *plFormatter) lightHeader(indent, loop int) error {
	if loop < 0 {
		return unsupported("plpgsql: FOR without LOOP")
	}
	from := f.i - 1
	f.emitLines(indent, "", f.light(from, loop, indent), " LOOP")
	f.i = loop
	f.consume()
	return f.loopBody(indent)
}

func (f *plFormatter) exitStmt(indent int) error {
	kw := strings.ToUpper(f.word(f.i))
	f.consume()
	semi := f.untilSemicolon(f.i)
	if semi < 0 || f.hasComment(f.i, semi) {
		return unsupported("plpgsql: EXIT")
	}
	line := kw
	if f.i < semi && f.word(f.i) != "when" {
		line += " " + f.tokText(f.i)
		f.consume()
	}
	if f.at("when") {
		f.consume()
		cond, err := f.expr(f.i, semi, indent+len(line)+6, indent)
		if err != nil {
			return err
		}
		f.emitLines(indent, line+" WHEN ", cond, ";")
	} else if f.i == semi {
		f.emit(indent, line+";")
	} else {
		return unsupported("plpgsql: EXIT")
	}
	f.i = semi
	f.consume()
	return nil
}

func (f *plFormatter) returnStmt(indent int) error {
	semi := f.untilSemicolon(f.i + 1)
	if semi < 0 {
		return unsupported("plpgsql: RETURN without semicolon")
	}
	f.consume()
	switch {
	case f.i == semi:
		f.emit(indent, "RETURN;")
	case f.at("next"):
		f.consume()
		e, err := f.expr(f.i, semi, indent+12, indent)
		if err != nil {
			return err
		}
		f.emitLines(indent, "RETURN NEXT ", e, ";")
	case f.at("query"):
		f.consume()
		if f.at("execute") || f.hasComment(f.i, semi) {
			f.i -= 2
			return f.lightStmt(indent)
		}
		query, err := f.p.sqlText(f.text(f.i, semi), indent+4)
		if err != nil {
			f.i -= 2
			return f.lightStmt(indent)
		}
		f.emit(indent, "RETURN QUERY")
		query[len(query)-1] += ";"
		f.out = append(f.out, query...)
	default:
		e, err := f.expr(f.i, semi, indent+7, indent)
		if err != nil {
			return err
		}
		f.emitLines(indent, "RETURN ", e, ";")
	}
	f.i = semi
	f.consume()
	return nil
}

func (f *plFormatter) perform(indent int) error {
	semi := f.untilSemicolon(f.i)
	if semi < 0 {
		return unsupported("plpgsql: PERFORM without semicolon")
	}
	if !f.hasComment(f.i, semi) {
		lines, err := f.p.sqlText("SELECT "+f.text(f.i+1, semi), indent)
		if err == nil && strings.HasPrefix(lines[0], spaces(indent)+"SELECT ") {
			lines[0] = spaces(indent) + "PERFORM " + strings.TrimPrefix(lines[0], spaces(indent)+"SELECT ")
			lines[len(lines)-1] += ";"
			f.out = append(f.out, lines...)
			f.i = semi
			f.consume()
			return nil
		}
	}
	return f.lightStmt(indent)
}

// assignment prints target := expr. It reports false when the statement is
// not an assignment.
func (f *plFormatter) assignment(indent int) (bool, error) {
	semi := f.untilSemicolon(f.i)
	if semi < 0 || !f.isName(f.i) {
		return false, nil
	}
	op := f.scanUntil(f.i, func(i int) bool {
		return i >= semi || f.toks[i].Kind == pg.Token_COLON_EQUALS || f.toks[i].Kind == pg.Token_ASCII_61
	})
	if op < 0 || op >= semi || f.hasComment(f.i, semi) {
		return false, nil
	}
	target, err := f.p.exprText(f.text(f.i, op), 0, 0)
	if err != nil || len(target) != 1 {
		return true, f.lightStmt(indent)
	}
	head := target[0] + " := "
	e, err := f.expr(op+1, semi, indent+width(head), indent)
	if err != nil {
		return true, err
	}
	f.emitLines(indent, head, e, ";")
	f.i = semi
	f.consume()
	return true, nil
}

// sqlStmt prints an embedded SQL statement. A plpgsql INTO clause is cut
// out before formatting and put back on its own river line.
func (f *plFormatter) sqlStmt(indent int) error {
	semi := f.untilSemicolon(f.i)
	if semi < 0 {
		return unsupported("plpgsql: statement without semicolon")
	}
	if f.hasComment(f.i, semi) {
		return f.lightStmt(indent)
	}
	into := -1
	for i := f.scanUntil(f.i, func(i int) bool { return i >= semi || f.word(i) == "into" }); i >= 0 && i < semi; {
		if prev := f.word(i - 1); prev != "insert" && prev != "merge" {
			into = i
			break
		}
		i = f.scanUntil(i+1, func(j int) bool { return j >= semi || f.word(j) == "into" })
	}
	text := f.text(f.i, semi)
	intoLine := ""
	if into >= 0 {
		j := into + 1
		strict := ""
		if f.word(j) == "strict" {
			strict = "STRICT "
			j++
		}
		var targets []string
		for {
			k := j
			for k < semi && (f.isName(k) || f.toks[k].Kind == pg.Token_ASCII_46) && (k <= j || !f.isName(k) || !f.isName(k-1)) {
				k++
			}
			if k == j {
				return f.lightStmt(indent)
			}
			t, err := f.p.exprText(f.text(j, k), 0, 0)
			if err != nil || len(t) != 1 {
				return f.lightStmt(indent)
			}
			targets = append(targets, t[0])
			j = k
			if !f.isKind(j, pg.Token_ASCII_44) {
				break
			}
			j++
		}
		intoLine = riverKW(indent, "INTO") + " " + strict + strings.Join(targets, ", ")
		text = f.text(f.i, into) + " " + f.text(j, semi)
	}
	lines, err := f.p.sqlText(text, indent)
	if err != nil {
		return f.lightStmt(indent)
	}
	if intoLine != "" {
		at := len(lines)
		if f.at("select", "with") {
			main := -1
			for k, l := range lines {
				if strings.HasPrefix(l, spaces(indent)+"SELECT ") {
					main = k
					break
				}
			}
			if main < 0 {
				return f.lightStmt(indent)
			}
			for k := main + 1; k < len(lines); k++ {
				if l := lines[k]; len(l) > indent &&
					strings.TrimSpace(l[indent:min(len(l), indent+riverWidth+1)]) != "" {
					at = k
					break
				}
			}
		}
		lines = append(lines[:at], append([]string{intoLine}, lines[at:]...)...)
	}
	lines[len(lines)-1] += ";"
	f.out = append(f.out, lines...)
	f.i = semi
	f.consume()
	return nil
}

// commentTextOf returns a comment token of src with trailing spaces and \r
// removed from each line.
func (p *printer) commentTextOf(src string, t Token) string {
	lines := strings.Split(src[t.Start:t.End], "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// parseSelect parses "SELECT <text>" and returns its targets.
func parseSelectTargets(text string) ([]*pg.ResTarget, error) {
	tree, err := pgquery.Parse("SELECT " + text)
	if err != nil || len(tree.Stmts) != 1 {
		return nil, unsupported("plpgsql expression")
	}
	s := tree.Stmts[0].Stmt.GetSelectStmt()
	if s == nil || len(s.FromClause) > 0 || s.WhereClause != nil || len(s.GroupClause) > 0 ||
		s.Op != pg.SetOperation_SETOP_NONE || len(s.SortClause) > 0 || s.LimitCount != nil ||
		len(s.DistinctClause) > 0 || s.IntoClause != nil || s.HavingClause != nil || len(s.WindowClause) > 0 {
		return nil, unsupported("plpgsql expression")
	}
	var out []*pg.ResTarget
	for _, t := range s.TargetList {
		rt := t.GetResTarget()
		if rt == nil || rt.Name != "" {
			return nil, unsupported("plpgsql expression")
		}
		out = append(out, rt)
	}
	return out, nil
}

// exprText formats a plpgsql expression.
func (p *printer) exprText(text string, col, indent int) ([]string, error) {
	targets, err := parseSelectTargets(text)
	if err != nil || len(targets) != 1 {
		return nil, unsupported("plpgsql expression")
	}
	return p.layout(targets[0].Val, col, indent, precNone)
}

func (p *printer) exprListText(text string) (string, error) {
	targets, err := parseSelectTargets(text)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(targets))
	for i, t := range targets {
		if parts[i], err = p.expr(t.Val, precNone); err != nil {
			return "", err
		}
	}
	return strings.Join(parts, ", "), nil
}

// typeText formats a type name written in a declaration.
func (p *printer) typeText(text string) (string, error) {
	targets, err := parseSelectTargets("NULL::" + text)
	if err != nil || len(targets) != 1 {
		return "", unsupported("plpgsql type")
	}
	tc := targets[0].Val.GetTypeCast()
	if tc == nil || tc.Arg.GetAConst() == nil || !tc.Arg.GetAConst().Isnull {
		return "", unsupported("plpgsql type")
	}
	return p.typeName(tc.TypeName)
}

// sqlText formats an embedded SQL statement with its river at base.
func (p *printer) sqlText(text string, base int) ([]string, error) {
	tree, err := pgquery.Parse(text)
	if err != nil || len(tree.Stmts) != 1 {
		return nil, unsupported("plpgsql SQL")
	}
	toks, err := Tokenize(text)
	if err != nil {
		return nil, err
	}
	sub := &printer{stmt: Stmt{Node: tree.Stmts[0].Stmt, Src: text, Tokens: toks}, plNames: p.plNames}
	if sub.hasComments(0, len(toks)) {
		return nil, unsupported("comment in plpgsql SQL")
	}
	return sub.stmtLines(sub.stmt.Node, base)
}

// pctType prints name%TYPE and name%ROWTYPE, which are not SQL types.
func (f *plFormatter) pctType(from, to int) (string, error) {
	if to-from < 3 || f.tokText(to-2) != "%" {
		return "", unsupported("plpgsql type")
	}
	suffix := f.word(to - 1)
	if suffix != "type" && suffix != "rowtype" {
		return "", unsupported("plpgsql type")
	}
	var parts []string
	for i := from; i < to-2; i++ {
		switch {
		case (i-from)%2 == 0 && f.isName(i):
			parts = append(parts, identValue(f.tokText(i)))
		case (i-from)%2 == 1 && f.toks[i].Kind == pg.Token_ASCII_46:
		default:
			return "", unsupported("plpgsql type")
		}
	}
	return quoteQualified(parts...) + "%" + strings.ToUpper(suffix), nil
}
