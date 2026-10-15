package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	unformatted = "create table t (id int);\n"
	formatted   = "CREATE TABLE \"t\" (\n    \"id\"    INTEGER\n);\n"
	broken      = "DROP TABLE IS EXISTS t;\n"
)

func setFlags(t *testing.T, w, l, d, c bool) {
	t.Helper()
	*write, *list, *diffs, *check = w, l, d, c
	t.Cleanup(func() { *write, *list, *diffs, *check = false, false, false, false })
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStdinToStdout(t *testing.T) {
	setFlags(t, false, false, false, false)
	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader(unformatted), &out, &errOut)
	if code != 0 || out.String() != formatted || errOut.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestStdinParseError(t *testing.T) {
	setFlags(t, false, false, false, false)
	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader(broken), &out, &errOut)
	if code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "<stdin>:1:12: ") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "a.sql")
	good := filepath.Join(dir, "sub", "b.sql")
	hidden := filepath.Join(dir, ".git", "c.sql")
	other := filepath.Join(dir, "notes.txt")
	writeFile(t, bad, unformatted)
	writeFile(t, good, formatted)
	writeFile(t, hidden, unformatted)
	writeFile(t, other, unformatted)

	setFlags(t, false, false, false, true)
	var out, errOut bytes.Buffer
	if code := run([]string{dir}, nil, &out, &errOut); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
	if out.String() != bad+"\n" {
		t.Fatalf("out=%q, want only %q", out.String(), bad)
	}
	if readFile(t, bad) != unformatted {
		t.Fatal("--check changed a file")
	}

	out.Reset()
	if code := run([]string{good}, nil, &out, &errOut); code != 0 || out.Len() != 0 {
		t.Fatalf("formatted file: code=%d out=%q", code, out.String())
	}
}

func TestWriteListContinuesPastErrors(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.sql")
	b := filepath.Join(dir, "b.sql")
	writeFile(t, a, broken)
	writeFile(t, b, unformatted)

	setFlags(t, true, true, false, false)
	var out, errOut bytes.Buffer
	code := run([]string{dir}, nil, &out, &errOut)
	if code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
	if readFile(t, a) != broken {
		t.Fatal("broken file was changed")
	}
	if readFile(t, b) != formatted {
		t.Fatalf("b.sql = %q", readFile(t, b))
	}
	if out.String() != b+"\n" {
		t.Fatalf("out=%q", out.String())
	}
	if !strings.Contains(errOut.String(), a+":1:12: ") {
		t.Fatalf("err=%q", errOut.String())
	}
}

func TestDiff(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.sql")
	writeFile(t, a, unformatted)

	setFlags(t, false, false, true, false)
	var out, errOut bytes.Buffer
	if code := run([]string{a}, nil, &out, &errOut); code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut.String())
	}
	want := "--- " + a + ".orig\n+++ " + a + "\n@@ -1,1 +1,3 @@\n" +
		"-create table t (id int);\n+CREATE TABLE \"t\" (\n+    \"id\"    INTEGER\n+);\n"
	if out.String() != want {
		t.Fatalf("diff:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteRejectsStdin(t *testing.T) {
	setFlags(t, true, false, false, false)
	var out, errOut bytes.Buffer
	if code := run(nil, strings.NewReader(unformatted), &out, &errOut); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
}
