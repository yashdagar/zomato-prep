package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var filterArg = regexp.MustCompile(`^W?(\d+)(?:-?([A-Z]\d+))?$`)

func main() {
	args := os.Args[1:]
	cmd := "test"
	if len(args) > 0 && (args[0] == "test" || args[0] == "banner") {
		cmd, args = args[0], args[1:]
	}
	s := newStyle(os.Stdout)
	if cmd == "banner" {
		name := "api"
		if len(args) > 0 {
			name = args[0]
		}
		logo(os.Stdout, s, s.bold(name)+s.dim("  ·  "+branch()+"  ·  "+runtime.Version()+"  ·  "+time.Now().Format("15:04:05")))
		return
	}
	verbose := false
	filter := ""
	for _, a := range args {
		switch {
		case a == "-v":
			verbose = true
		case strings.TrimSpace(a) != "":
			filter = a
		}
	}
	os.Exit(runTests(s, filter, verbose))
}

func runTests(s style, filter string, verbose bool) int {
	week, label := 0, ""
	if filter != "" {
		m := filterArg.FindStringSubmatch(strings.ToUpper(strings.ReplaceAll(filter, " ", "")))
		if m == nil {
			pf(os.Stderr, "zt: %q is not a step or week, try W1, W1B2 or w1-b2\n", filter)
			return 2
		}
		week, _ = strconv.Atoi(m[1])
		label = m[2]
	}

	grpc := os.Getenv("TEST_GRPC_ADDR") != ""
	all := append(append(scanGo("internal", "unit", grpc), scanGo("e2e", "e2e", grpc)...), scanJest("../app-tests")...)
	var tests []*test
	for _, t := range all {
		if (week == 0 || t.week == week) && (label == "" || t.step == label) {
			tests = append(tests, t)
		}
	}
	scope := "all weeks"
	if week > 0 {
		scope = fmt.Sprintf("W%d%s", week, label)
	}
	logo(os.Stdout, s, s.bold("tests")+s.dim("  ·  "+scope+"  ·  "+branch()+"  ·  "+runtime.Version()))
	if len(tests) == 0 {
		fmt.Println("  No tests match " + filter)
		fmt.Println()
		return 2
	}

	byKey := map[string]*test{}
	has := map[string]bool{}
	var jest []*test
	for _, t := range tests {
		byKey[t.pkg+"|"+t.name] = t
		has[t.kind] = true
		if t.kind == "app" {
			jest = append(jest, t)
		}
	}
	run := ""
	switch {
	case label != "":
		run = fmt.Sprintf("^TestW%d%s_", week, label)
	case week > 0:
		run = fmt.Sprintf("^TestW%d[A-Z]", week)
	}
	withRun := func(base ...string) []string {
		if run != "" {
			base = append(base, "-run", run)
		}
		return base
	}

	start := time.Now()
	spin := spinner(s)
	pkgs := map[string]*pkgState{}
	ran := map[string]bool{}
	merge := func(m map[string]*pkgState) {
		for k, v := range m {
			pkgs[k] = v
		}
	}
	if has["unit"] {
		spin("unit tests")
		m, err := runGo(append(withRun("-race", "-count=1"), "./internal/..."), byKey)
		if err == nil {
			merge(m)
			ran["unit"] = true
		}
	}
	if has["e2e"] {
		spin("end-to-end tests")
		a := withRun("-count=1")
		if grpc {
			a = append(a, "-tags", "grpc")
		}
		m, err := runGo(append(a, "./e2e/..."), byKey)
		if err == nil {
			merge(m)
			ran["e2e"] = true
		}
	}
	if len(jest) > 0 {
		spin("app tests")
		jf := ""
		if week > 0 {
			jf = fmt.Sprintf("W%d %s", week, label)
		}
		runJest("../app-tests", strings.TrimSpace(jf), jest)
	}
	spin("")

	errs := settle(tests, pkgs, ran)
	bad := render(os.Stdout, s, tests, errs, verbose)
	fmt.Println("  " + s.dim(fmt.Sprintf("%.1fs", time.Since(start).Seconds())))
	fmt.Println()
	if bad {
		return 1
	}
	return 0
}

func spinner(s style) func(string) {
	if !isTTY(os.Stderr) {
		return func(string) {}
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	stop := make(chan struct{})
	done := make(chan struct{})
	label := make(chan string, 1)
	go func() {
		defer close(done)
		cur := ""
		for i := 0; ; i++ {
			select {
			case <-stop:
				pf(os.Stderr, "\r\x1b[2K")
				return
			case cur = <-label:
			case <-time.After(80 * time.Millisecond):
			}
			pf(os.Stderr, "\r\x1b[2K  %s %s", s.red(frames[i%len(frames)]), s.dim("running "+cur))
		}
	}()
	return func(l string) {
		if l == "" {
			close(stop)
			<-done
			return
		}
		label <- l
	}
}

func branch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "no git"
	}
	return strings.TrimSpace(string(out))
}
