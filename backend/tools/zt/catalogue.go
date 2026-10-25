package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const module = "github.com/yashdagar/zomato-prep/backend"

type status int

const (
	todo status = iota
	skipped
	passed
	failed
	broken
)

type test struct {
	kind    string
	pkg     string
	name    string
	week    int
	step    string
	title   string
	status  status
	elapsed float64
	note    string
	out     []string
}

var (
	goTest   = regexp.MustCompile(`(?m)^func (TestW(\d+)([A-Z]\d+)_(\w+))\(t \*testing\.T\)`)
	jestDesc = regexp.MustCompile(`describe\('W(\d+) ([A-Z]\d+) ([^']+)'`)
	jestTest = regexp.MustCompile(`(?m)^\s+test\('([^']+)'`)
)

func scanGo(dir, kind string, grpc bool) []*test {
	var out []*test
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		tagged := strings.HasPrefix(string(src), "//go:build grpc")
		pkg := module + "/" + filepath.ToSlash(filepath.Dir(path))
		for _, m := range goTest.FindAllStringSubmatch(string(src), -1) {
			week, _ := strconv.Atoi(m[2])
			t := &test{kind: kind, pkg: pkg, name: m[1], week: week, step: m[3], title: words(m[4])}
			if tagged && !grpc {
				t.status = skipped
				t.note = "set TEST_GRPC_ADDR=localhost:9090 to run (after buf generate)"
			}
			out = append(out, t)
		}
		return nil
	})
	return out
}

func scanJest(dir string) []*test {
	var out []*test
	files, _ := filepath.Glob(filepath.Join(dir, "lib", "*.test.ts"))
	sort.Strings(files)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		d := jestDesc.FindStringSubmatch(string(src))
		if d == nil {
			continue
		}
		week, _ := strconv.Atoi(d[1])
		for _, m := range jestTest.FindAllStringSubmatch(string(src), -1) {
			out = append(out, &test{
				kind:  "app",
				pkg:   filepath.Base(f),
				name:  "W" + d[1] + " " + d[2] + " " + d[3] + " " + m[1],
				week:  week,
				step:  d[2],
				title: m[1],
			})
		}
	}
	return out
}

func words(camel string) string {
	var b strings.Builder
	r := []rune(strings.ReplaceAll(camel, "_", " "))
	for i, c := range r {
		if i > 0 && unicode.IsDigit(c) && unicode.IsLetter(r[i-1]) {
			b.WriteRune(' ')
		}
		if i > 0 && unicode.IsUpper(c) && r[i-1] != ' ' {
			prevLower := unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1])
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if prevLower || (nextLower && unicode.IsUpper(r[i-1])) {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(c)
	}
	s := strings.Fields(b.String())
	for i, w := range s {
		if i > 0 && len(w) > 1 && unicode.IsUpper(rune(w[0])) && unicode.IsLower(rune(w[1])) {
			s[i] = strings.ToLower(w)
		} else if i > 0 && len(w) == 1 && w != "A" {
			s[i] = strings.ToLower(w)
		} else if i > 0 && w == "A" {
			s[i] = "a"
		}
	}
	return strings.Join(s, " ")
}
