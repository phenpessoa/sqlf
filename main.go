// sqlf formats PostgreSQL .sql files in one fixed style.
//
// Usage:
//
//	sqlf [flags] [path ...]
//
// With no paths, sqlf formats standard input to standard output. A
// directory is walked recursively for *.sql files, skipping directories
// whose names start with a dot. sqlf only works with PostgreSQL SQL syntax.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/phenpessoa/sqlf/internal/diff"
	"github.com/phenpessoa/sqlf/internal/format"
)

var (
	write = flag.Bool("w", false, "write result to the file instead of stdout")
	list  = flag.Bool("l", false, "list files whose formatting differs")
	diffs = flag.Bool("d", false, "print diffs instead of the formatted file")
	check = flag.Bool("check", false, "list files whose formatting differs and exit 1 if any")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sqlf [flags] [path ...]\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	os.Exit(run(flag.Args(), os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	failed := false
	if len(args) == 0 {
		if *write {
			_, _ = fmt.Fprintln(stderr, "sqlf: cannot use -w with standard input")
			return 1
		}
		src, err := io.ReadAll(stdin)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "sqlf: %v\n", err)
			return 1
		}
		if !process("<stdin>", src, stdout, stderr) {
			failed = true
		}
		return exitCode(failed)
	}

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "sqlf: %v\n", err)
			failed = true
			continue
		}
		if !info.IsDir() {
			if !processFile(arg, stdout, stderr) {
				failed = true
			}
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != arg && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".sql") && !processFile(path, stdout, stderr) {
				failed = true
			}
			return nil
		})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "sqlf: %v\n", err)
			failed = true
		}
	}
	return exitCode(failed)
}

func exitCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

func processFile(path string, stdout, stderr io.Writer) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "sqlf: %v\n", err)
		return false
	}
	return process(path, src, stdout, stderr)
}

// process formats one input and reports whether it succeeded. Under
// -check, a file that needs formatting counts as a failure.
func process(name string, src []byte, stdout, stderr io.Writer) bool {
	out, err := format.Format(string(src))
	if err != nil {
		var fe *format.Error
		if errors.As(err, &fe) && fe.Line > 0 {
			_, _ = fmt.Fprintf(stderr, "%s:%d:%d: %s\n", name, fe.Line, fe.Col, fe.Msg)
		} else {
			_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
		}
		return false
	}
	res := []byte(out)
	changed := !bytes.Equal(src, res)
	ok := true

	if changed && (*list || *check) {
		_, _ = fmt.Fprintln(stdout, name)
	}
	if changed && *check {
		ok = false
	}
	if changed && *write {
		info, err := os.Stat(name)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "sqlf: %v\n", err)
			return false
		}
		if err := os.WriteFile(name, res, info.Mode().Perm()); err != nil {
			_, _ = fmt.Fprintf(stderr, "sqlf: %v\n", err)
			return false
		}
	}
	if changed && *diffs {
		_, _ = fmt.Fprint(stdout, diff.Unified(name+".orig", name, string(src), out))
	}
	if !*list && !*write && !*diffs && !*check {
		_, _ = stdout.Write(res)
	}
	return ok
}
