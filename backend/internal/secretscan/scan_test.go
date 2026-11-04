//go:build w7o1

package secretscan_test

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/yashdagar/zomato-prep/backend/internal/secretscan"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func scan(t *testing.T, files fstest.MapFS) []secretscan.Finding {
	t.Helper()
	got, err := secretscan.Scan(files)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestW7O1_FindsAnAwsAccessKeyOnTheRightLine(t *testing.T) {
	got := scan(t, fstest.MapFS{"deploy.sh": file("#!/bin/sh\nset -e\nexport AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n")})
	want := secretscan.Finding{Path: "deploy.sh", Line: 3, Rule: "aws-access-key"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("findings = %+v, want [%+v]", got, want)
	}
}

func TestW7O1_FindsAPrivateKeyHeader(t *testing.T) {
	got := scan(t, fstest.MapFS{"keys/server.pem": file("-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----\n")})
	if len(got) != 1 || got[0].Rule != "private-key" || got[0].Line != 1 || got[0].Path != "keys/server.pem" {
		t.Fatalf("findings = %+v, want one private-key finding on line 1", got)
	}
}

func TestW7O1_FindsAGithubToken(t *testing.T) {
	got := scan(t, fstest.MapFS{"ci.yml": file("env:\n  TOKEN_X: ghp_abcdefghijklmnopqrstuvwxyzABCDEF0123\n")})
	if len(got) != 1 || got[0].Rule != "github-token" || got[0].Line != 2 {
		t.Fatalf("findings = %+v, want one github-token finding on line 2", got)
	}
}

func TestW7O1_FindsGenericAssignmentsOfSecretLookingNames(t *testing.T) {
	src := "password: \"hunter2hunter2\"\nJWT_SECRET=abcdefgh1234\napi_key = 'k9d8s7f6g5h4'\nDB_TOKEN: zz99yy88xx77\n"
	got := scan(t, fstest.MapFS{"config.txt": file(src)})
	if len(got) != 4 {
		t.Fatalf("findings = %+v, want 4", got)
	}
	for i, f := range got {
		if f.Rule != "generic-secret" || f.Line != i+1 {
			t.Fatalf("finding %d = %+v, want generic-secret on line %d", i, f, i+1)
		}
	}
}

func TestW7O1_IgnoresPlaceholdersAndShortValues(t *testing.T) {
	src := "JWT_SECRET=${JWT_SECRET}\npassword: ${DB_PASSWORD}\nPASSWORD=\ntoken: short\nsecret=$SECRET_FROM_ENV\napi_key: <your-key-here>\n"
	if got := scan(t, fstest.MapFS{"compose.yml": file(src)}); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestW7O1_SkipsExampleFiles(t *testing.T) {
	got := scan(t, fstest.MapFS{".env.example": file("JWT_SECRET=local-development-secret\n"), "config.yml.example": file("password: hunter2hunter2\n")})
	if len(got) != 0 {
		t.Fatalf("findings = %+v, example files are templates", got)
	}
}

func TestW7O1_SkipsBinaryFiles(t *testing.T) {
	got := scan(t, fstest.MapFS{"logo.png": file("\x89PNG\x00\x00AKIAIOSFODNN7EXAMPLE")})
	if len(got) != 0 {
		t.Fatalf("findings = %+v, binary files must be skipped", got)
	}
}

func TestW7O1_SkipsGitAndNodeModules(t *testing.T) {
	got := scan(t, fstest.MapFS{
		".git/config":                 file("token = AKIAIOSFODNN7EXAMPLE\n"),
		"app/node_modules/x/index.js": file("const k = 'AKIAIOSFODNN7EXAMPLE'\n"),
		"app/src/index.ts":            file("const k = 'AKIAIOSFODNN7EXAMPLE'\n"),
	})
	if len(got) != 1 || got[0].Path != "app/src/index.ts" {
		t.Fatalf("findings = %+v, want only app/src/index.ts", got)
	}
}

func TestW7O1_ReportsOneFindingPerLineSortedByPathThenLine(t *testing.T) {
	got := scan(t, fstest.MapFS{
		"b.txt": file("x\nAKIAIOSFODNN7EXAMPLE password=hunter2hunter2\n"),
		"a.txt": file("secret=abcdefgh1234\n\nAKIAIOSFODNN7EXAMPLE\n"),
	})
	want := []secretscan.Finding{
		{Path: "a.txt", Line: 1, Rule: "generic-secret"},
		{Path: "a.txt", Line: 3, Rule: "aws-access-key"},
		{Path: "b.txt", Line: 2, Rule: "aws-access-key"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

func TestW7O1_FindingsNeverContainTheSecretValue(t *testing.T) {
	got := scan(t, fstest.MapFS{"x.txt": file("AKIAIOSFODNN7EXAMPLE\npassword=hunter2hunter2\n")})
	out := fmt.Sprintf("%v %+v %#v", got, got, got)
	for _, secret := range []string{"AKIAIOSFODNN7EXAMPLE", "hunter2hunter2"} {
		if contains(out, secret) {
			t.Fatalf("findings leak %q: %s", secret, out)
		}
	}
}

func TestW7O1_CleanTreeHasNoFindings(t *testing.T) {
	got := scan(t, fstest.MapFS{"main.go": file("package main\n\nfunc main() {}\n"), "README.md": file("# hello\n")})
	if len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
