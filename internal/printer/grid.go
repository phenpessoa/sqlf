package printer

import (
	"strings"
	"unicode/utf8"
)

// gridRow is one line of an aligned grid.
type gridRow struct {
	cells   []string
	suffix  string   // printed right after the last non-empty cell ("," or "")
	comment string   // trailing comment, aligned as the last slot
	leading []string // full-line comments printed above the row
}

// tabStop returns the first multiple of 4 at or after col.
func tabStop(col int) int {
	return (col + 3) / 4 * 4
}

func width(s string) int {
	return utf8.RuneCountInString(s)
}

// renderGrid lays out rows with the tab-stop rule: every slot starts on an
// absolute column that is a multiple of 4, at least 4 spaces after the
// longest cell of the previous slot. Slots that no row uses are dropped.
func renderGrid(origin int, rows []gridRow) []string {
	ncols := 0
	for _, r := range rows {
		ncols = max(ncols, len(r.cells))
	}
	used := make([]bool, ncols)
	for _, r := range rows {
		for i, c := range r.cells {
			if c != "" {
				used[i] = true
			}
		}
	}
	var keep []int
	for i, u := range used {
		if u {
			keep = append(keep, i)
		}
	}

	starts := make([]int, len(keep))
	col := origin
	for k, i := range keep {
		starts[k] = col
		w := 0
		for _, r := range rows {
			if i < len(r.cells) {
				w = max(w, width(r.cells[i]))
			}
		}
		col = tabStop(col + w + 4)
	}

	lines := make([]string, len(rows))
	longest := 0
	for ri, r := range rows {
		var b strings.Builder
		at := 0
		for k, i := range keep {
			if i >= len(r.cells) || r.cells[i] == "" {
				continue
			}
			pad := max(starts[k]-at, 0)
			if at > 0 || starts[k] > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
			b.WriteString(r.cells[i])
			at = starts[k] + width(r.cells[i])
		}
		b.WriteString(r.suffix)
		lines[ri] = strings.TrimRight(b.String(), " ")
		longest = max(longest, width(lines[ri]))
	}

	commentCol := tabStop(longest + 4)
	var out []string
	for ri, r := range rows {
		for _, c := range r.leading {
			out = append(out, strings.Repeat(" ", origin)+c)
		}
		line := lines[ri]
		if r.comment != "" {
			line += strings.Repeat(" ", commentCol-width(line)) + r.comment
		}
		out = append(out, line)
	}
	return out
}
