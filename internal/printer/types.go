package printer

import (
	"fmt"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"

	"github.com/phenpessoa/sqlf/internal/builtins"
)

// canonicalTypes maps internal pg_catalog type names to the spelling the
// style uses. The parser stores SQL-standard spellings (INTEGER, BOOLEAN,
// TIMESTAMP WITH TIME ZONE...) under these internal names.
var canonicalTypes = map[string]string{
	"int2":        "SMALLINT",
	"int4":        "INTEGER",
	"int8":        "BIGINT",
	"bool":        "BOOLEAN",
	"float4":      "REAL",
	"float8":      "DOUBLE PRECISION",
	"numeric":     "NUMERIC",
	"varchar":     "VARCHAR",
	"bpchar":      "CHAR",
	"timestamp":   "TIMESTAMP",
	"timestamptz": "TIMESTAMPTZ",
	"time":        "TIME",
	"timetz":      "TIMETZ",
	"interval":    "INTERVAL",
	"bit":         "BIT",
	"varbit":      "VARBIT",
}

// unqualifiedCanonical lists the internal names that print in canonical
// form even when written without the pg_catalog schema. Other names (such
// as bpchar, where CHAR would add a length of 1) keep their own spelling.
var unqualifiedCanonical = map[string]bool{
	"int2":        true,
	"int4":        true,
	"int8":        true,
	"bool":        true,
	"float4":      true,
	"float8":      true,
	"timestamptz": true,
	"timetz":      true,
}

func (p *printer) typeName(t *pg.TypeName) (string, error) {
	if t == nil {
		return "", unsupported("missing type")
	}
	parts, err := stringList(t.Names)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if t.Setof {
		b.WriteString("SETOF ")
	}
	if t.PctType {
		b.WriteString(quoteQualified(parts...))
		b.WriteString("%TYPE")
		return b.String(), nil
	}

	qualified := len(parts) == 2 && parts[0] == "pg_catalog"
	base := parts[len(parts)-1]
	switch {
	case qualified && base == "bpchar" && len(t.Typmods) == 0:
		// CHAR alone means CHAR(1); only the explicit name keeps "no length".
		b.WriteString(quoteQualified(parts...))
	case qualified && canonicalTypes[base] != "":
		b.WriteString(canonicalTypes[base])
	case len(parts) == 1 && unqualifiedCanonical[base]:
		b.WriteString(canonicalTypes[base])
	case len(parts) == 1 && builtins.IsType(base) && bareWordOK(base):
		b.WriteString(strings.ToUpper(base))
	default:
		b.WriteString(quoteQualified(parts...))
	}

	if len(t.Typmods) > 0 {
		if base == "interval" {
			return "", unsupported("interval typmods")
		}
		mods := make([]string, 0, len(t.Typmods))
		for _, m := range t.Typmods {
			s, err := p.expr(m, 0)
			if err != nil {
				return "", err
			}
			mods = append(mods, s)
		}
		b.WriteString("(" + strings.Join(mods, ", ") + ")")
	}
	for _, bound := range t.ArrayBounds {
		i := bound.GetInteger()
		if i == nil {
			return "", unsupported("array bound")
		}
		if i.Ival < 0 {
			b.WriteString("[]")
		} else {
			fmt.Fprintf(&b, "[%d]", i.Ival)
		}
	}
	return b.String(), nil
}
