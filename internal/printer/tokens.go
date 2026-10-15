package printer

import (
	"sort"

	pgquery "github.com/wasilibs/go-pgquery"
)

// Tokenize scans src with the PostgreSQL scanner and adds line numbers.
func Tokenize(src string) ([]Token, error) {
	res, err := pgquery.Scan(src)
	if err != nil {
		return nil, err
	}
	lineStarts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineOf := func(off int) int {
		return sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > off }) - 1
	}
	out := make([]Token, len(res.Tokens))
	for i, t := range res.Tokens {
		start, end := int(t.Start), int(t.End)
		out[i] = Token{
			Start:       start,
			End:         end,
			Kind:        t.Token,
			KeywordKind: t.KeywordKind,
			Line:        lineOf(start),
			EndLine:     lineOf(max(end-1, start)),
		}
	}
	return out, nil
}
