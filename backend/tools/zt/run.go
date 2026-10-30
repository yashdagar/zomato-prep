package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type event struct {
	Action     string
	Package    string
	Test       string
	Output     string
	ImportPath string
	Elapsed    float64
}

type pkgState struct {
	buildFailed bool
	buildOut    []string
	failed      bool
	out         []string
}

var (
	compileErr = regexp.MustCompile(`^(\S+\.go):(\d+):\d+: (.*)$`)
	logLine    = regexp.MustCompile(`^\s*(\S+_test\.go:\d+): (.*)$`)
)

func runGo(args []string, byKey map[string]*test) (map[string]*pkgState, error) {
	cmd := exec.Command("go", append([]string{"test", "-json"}, args...)...)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pkgs := map[string]*pkgState{}
	get := func(p string) *pkgState {
		p, _, _ = strings.Cut(p, " ")
		if pkgs[p] == nil {
			pkgs[p] = &pkgState{}
		}
		return pkgs[p]
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch {
		case e.ImportPath != "":
			ps := get(e.ImportPath)
			if e.Action == "build-output" {
				ps.buildOut = append(ps.buildOut, strings.TrimRight(e.Output, "\n"))
			}
			if e.Action == "build-fail" {
				ps.buildFailed = true
			}
		case e.Test == "":
			ps := get(e.Package)
			if e.Action == "output" {
				ps.out = append(ps.out, strings.TrimRight(e.Output, "\n"))
				if strings.Contains(e.Output, "[build failed]") || strings.Contains(e.Output, "[setup failed]") {
					ps.buildFailed = true
				}
			}
			if e.Action == "fail" {
				ps.failed = true
			}
		default:
			top, _, _ := strings.Cut(e.Test, "/")
			t := byKey[e.Package+"|"+top]
			if t == nil {
				continue
			}
			switch e.Action {
			case "output":
				line := strings.TrimRight(e.Output, "\n")
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "=== ") || strings.HasPrefix(trimmed, "--- ") {
					continue
				}
				if _, sub, ok := strings.Cut(e.Test, "/"); ok && logLine.MatchString(line) {
					line = strings.ReplaceAll(sub, "_", " ") + " › " + trimmed
				}
				t.out = append(t.out, line)
			case "pass", "fail", "skip":
				if e.Test != top {
					continue
				}
				t.elapsed = e.Elapsed
				switch e.Action {
				case "pass":
					t.status = passed
				case "fail":
					t.status = failed
				case "skip":
					t.status = skipped
					t.note = skipNote(t.out)
				}
			}
		}
	}
	_ = cmd.Wait()
	return pkgs, nil
}

func skipNote(out []string) string {
	for _, l := range out {
		if m := logLine.FindStringSubmatch(l); m != nil {
			return m[2]
		}
	}
	return "skipped"
}

func settle(tests []*test, pkgs map[string]*pkgState, ran map[string]bool) map[string][]string {
	errs := map[string][]string{}
	for _, t := range tests {
		if t.kind == "app" || !ran[t.kind] || t.status != todo {
			continue
		}
		ps := pkgs[t.pkg]
		if ps == nil {
			continue
		}
		if ps.buildFailed {
			lines := compileErrors(ps.buildOut)
			if missingOnly(lines) {
				t.note = "not written yet"
			} else {
				t.status = broken
				errs[t.pkg] = lines
			}
			continue
		}
		if ps.failed {
			t.status = failed
			t.out = append(t.out, ps.out...)
		}
	}
	return errs
}

func compileErrors(out []string) []string {
	var lines []string
	for _, l := range out {
		l = strings.TrimSpace(l)
		if m := compileErr.FindStringSubmatch(l); m != nil {
			lines = append(lines, filepath.Base(m[1])+":"+m[2]+": "+m[3])
		} else if strings.HasPrefix(l, "no non-test Go files") || strings.Contains(l, "is not in std") || strings.Contains(l, "no required module") {
			lines = append(lines, l)
		}
	}
	return lines
}

func missingOnly(lines []string) bool {
	if len(lines) == 0 {
		return true
	}
	for _, l := range lines {
		_, msg, _ := strings.Cut(l, ": ")
		if !strings.HasPrefix(msg, "undefined: ") && !strings.HasPrefix(l, "no non-test Go files") && msg != "too many errors" {
			return false
		}
	}
	return true
}

type jestReport struct {
	TestResults []struct {
		Name             string `json:"name"`
		Status           string `json:"status"`
		Message          string `json:"message"`
		AssertionResults []struct {
			FullName        string   `json:"fullName"`
			Status          string   `json:"status"`
			Duration        float64  `json:"duration"`
			FailureMessages []string `json:"failureMessages"`
		} `json:"assertionResults"`
	} `json:"testResults"`
}

func runJest(dir, filter string, tests []*test) {
	byName := map[string]*test{}
	byFile := map[string][]*test{}
	for _, t := range tests {
		byName[t.name] = t
		byFile[t.pkg] = append(byFile[t.pkg], t)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		for _, t := range tests {
			t.status = skipped
			t.note = "run npm install in app-tests to run the app tests"
		}
		return
	}
	args := []string{"jest", "--json", "--silent", "--passWithNoTests"}
	if filter != "" {
		args = append(args, "-t", filter)
	}
	cmd := exec.Command("npx", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	_ = cmd.Run()
	var r jestReport
	if json.Unmarshal(stdout.Bytes(), &r) != nil {
		for _, t := range tests {
			t.status = broken
			t.out = []string{"jest did not produce a report; run npx jest in app-tests to see why"}
		}
		return
	}
	for _, f := range r.TestResults {
		file := filepath.Base(f.Name)
		if len(f.AssertionResults) == 0 && f.Status == "failed" {
			for _, t := range byFile[file] {
				if strings.Contains(f.Message, "Cannot find module '@app/") || strings.Contains(f.Message, "Could not locate module @app/") {
					t.note = "not written yet"
				} else {
					t.status = broken
					t.out = firstLines(f.Message, 3)
				}
			}
			continue
		}
		for _, a := range f.AssertionResults {
			t := byName[a.FullName]
			if t == nil {
				continue
			}
			t.elapsed = a.Duration / 1000
			switch a.Status {
			case "passed":
				t.status = passed
			case "failed":
				t.status = failed
				t.out = firstLines(strings.Join(a.FailureMessages, "\n"), 8)
			default:
				t.status = skipped
				t.note = "skipped"
			}
		}
	}
}

func firstLines(s string, n int) []string {
	var out []string
	for _, l := range strings.Split(stripANSI(s), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "●") || t == "Require stack:" {
			continue
		}
		out = append(out, l)
		if len(out) == n {
			break
		}
	}
	return out
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }
