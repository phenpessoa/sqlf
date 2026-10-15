package safety

import "testing"

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		in, out string
		ok      bool
	}{
		{"same", "SELECT 1;", "SELECT 1;", true},
		{"whitespace and case", "select  1 ;", "SELECT 1;", true},
		{"quoting folds", "create table t (id int);", `CREATE TABLE "t" ("id" INTEGER);`, true},
		{"not equal", "select 1 where a != b;", "SELECT 1 WHERE a <> b;", true},
		{"type spelling", "create table t (a int4, b timestamp with time zone);",
			`CREATE TABLE "t" ("a" INTEGER, "b" TIMESTAMPTZ);`, true},
		{"constraint order", "create table t (id int not null primary key);",
			`CREATE TABLE "t" ("id" INTEGER PRIMARY KEY NOT NULL);`, true},
		{"function option order", "create function f() returns int language sql stable as 'select 1';",
			"CREATE FUNCTION f() RETURNS INT AS 'select 1' LANGUAGE sql STABLE;", true},

		{"changed value", "SELECT 1;", "SELECT 2;", false},
		{"changed case of quoted name", `SELECT "A";`, `SELECT "a";`, false},
		{"lost statement", "SELECT 1; SELECT 2;", "SELECT 1;", false},
		{"lost comment", "SELECT 1; -- keep", "SELECT 1;", false},
		{"changed comment", "-- a\nSELECT 1;", "-- b\nSELECT 1;", false},
		{"broken output", "SELECT 1;", "SELECT FROM WHERE;", false},
		{"bpchar is not CHAR", "create table t (a bpchar);", "CREATE TABLE t (a CHAR);", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Check(tt.in, tt.out)
			if tt.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("want an error, got nil")
			}
		})
	}
}
