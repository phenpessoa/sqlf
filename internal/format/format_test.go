package format

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite want.sql files with the current output")

// TestGolden formats testdata/<case>/input.sql and compares the result with
// want.sql byte for byte. Formatting want.sql must give want.sql again.
func TestGolden(t *testing.T) {
	dirs, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join(dir, "input.sql"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(string(input))
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			wantPath := filepath.Join(dir, "want.sql")
			if *update {
				if err := os.WriteFile(wantPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("output differs from want.sql\n--- got\n%s\n--- want\n%s", got, want)
			}
			again, err := Format(string(want))
			if err != nil {
				t.Fatalf("Format(want): %v", err)
			}
			if again != string(want) {
				t.Errorf("not idempotent\n--- format(want)\n%s\n--- want\n%s", again, want)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	src := "CREATE TABLE t (id INT);\n\nDROP TABLE IS EXISTS t;\n"
	_, err := Format(src)
	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatalf("want *Error, got %v", err)
	}
	if fe.Line != 3 || fe.Col != 12 {
		t.Errorf("position = %d:%d, want 3:12 (%s)", fe.Line, fe.Col, fe.Msg)
	}
}
