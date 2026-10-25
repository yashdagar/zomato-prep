package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type step struct {
	week  int
	label string
	tests []*test
}

func (s *step) status() status {
	var n [5]int
	for _, t := range s.tests {
		n[t.status]++
	}
	switch {
	case n[failed] > 0:
		return failed
	case n[broken] > 0:
		return broken
	case n[passed] == len(s.tests):
		return passed
	case n[passed] > 0:
		return -1
	case n[skipped] == len(s.tests):
		return skipped
	}
	return todo
}

func (s *step) pending() bool {
	for _, t := range s.tests {
		if t.status == todo || t.status == failed || t.status == broken {
			return true
		}
	}
	return false
}

func steps(tests []*test) map[int][]*step {
	byWeek := map[int][]*step{}
	index := map[string]*step{}
	for _, t := range tests {
		k := strconv.Itoa(t.week) + t.step
		s := index[k]
		if s == nil {
			s = &step{week: t.week, label: t.step}
			index[k] = s
			byWeek[t.week] = append(byWeek[t.week], s)
		}
		s.tests = append(s.tests, t)
	}
	for _, ss := range byWeek {
		sort.Slice(ss, func(i, j int) bool { return stepOrder(ss[i].label) < stepOrder(ss[j].label) })
	}
	return byWeek
}

func stepOrder(label string) int {
	n, _ := strconv.Atoi(label[1:])
	return int(label[0])*100 + n
}

func render(w io.Writer, s style, tests []*test, errs map[string][]string, verbose bool) (bad bool) {
	byWeek := steps(tests)
	weeks := make([]int, 0, len(byWeek))
	for wk := range byWeek {
		weeks = append(weeks, wk)
	}
	sort.Ints(weeks)

	var next *step
	for _, wk := range weeks {
		ss := byWeek[wk]
		done := 0
		for _, st := range ss {
			if st.status() == passed {
				done++
			} else if next == nil && st.pending() {
				next = st
			}
		}
		head := s.bold(fmt.Sprintf("Week %d", wk))
		pf(w, "  %s  %s  %s\n", head, s.bar(done, len(ss), 24), s.dim(fmt.Sprintf("%d/%d steps", done, len(ss))))
		for _, st := range ss {
			row(w, s, st, verbose)
		}
		ln(w)
	}

	if len(errs) > 0 {
		bad = true
		ln(w, "  "+s.red("Does not compile"))
		pkgs := make([]string, 0, len(errs))
		for p := range errs {
			pkgs = append(pkgs, p)
		}
		sort.Strings(pkgs)
		for _, p := range pkgs {
			ln(w, "    "+s.bold(strings.TrimPrefix(p, module+"/")))
			for i, l := range errs[p] {
				if i == 6 && !verbose {
					ln(w, "      "+s.dim(fmt.Sprintf("... %d more", len(errs[p])-6)))
					break
				}
				ln(w, "      "+l)
			}
		}
		ln(w)
	}

	notes := map[string]int{}
	var order []string
	var n [5]int
	elapsed := 0.0
	for _, t := range tests {
		n[t.status]++
		elapsed += t.elapsed
		if t.status == skipped {
			if notes[t.note] == 0 {
				order = append(order, t.note)
			}
			notes[t.note]++
		}
	}
	if len(order) > 0 {
		ln(w, "  "+s.yellow("Skipped"))
		for _, note := range order {
			pf(w, "    %s  %s\n", s.dim(fmt.Sprintf("%3d", notes[note])), note)
		}
		ln(w)
	}

	parts := []string{s.green(fmt.Sprintf("%d passed", n[passed]))}
	if n[failed] > 0 {
		bad = true
		parts = append(parts, s.red(fmt.Sprintf("%d failed", n[failed])))
	}
	if n[broken] > 0 {
		bad = true
		parts = append(parts, s.red(fmt.Sprintf("%d blocked", n[broken])))
	}
	if n[todo] > 0 {
		parts = append(parts, s.dim(fmt.Sprintf("%d not written", n[todo])))
	}
	if n[skipped] > 0 {
		parts = append(parts, s.yellow(fmt.Sprintf("%d skipped", n[skipped])))
	}
	pf(w, "  %s\n", strings.Join(parts, s.dim(" · ")))
	if next != nil {
		pf(w, "  %s W%d %s %s   %s\n", s.dim("next"), next.week, next.label, title(next), s.dim(fmt.Sprintf("make test W=W%d%s", next.week, next.label)))
	}
	ln(w)
	return bad
}

func title(st *step) string {
	t := st.tests[0].title
	if st.tests[0].kind == "app" {
		t = strings.TrimPrefix(st.tests[0].name, fmt.Sprintf("W%d %s ", st.week, st.label))
		t = strings.TrimSuffix(t, " "+st.tests[0].title)
	}
	if len(t) > 46 {
		t = t[:45] + "…"
	}
	return t
}

func row(w io.Writer, s style, st *step, verbose bool) {
	var mark, right string
	pass := 0
	for _, t := range st.tests {
		if t.status == passed {
			pass++
		}
	}
	kinds := map[string]bool{}
	for _, t := range st.tests {
		kinds[t.kind] = true
	}
	var ks []string
	for _, k := range []string{"unit", "e2e", "app"} {
		if kinds[k] {
			ks = append(ks, k)
		}
	}
	tag := strings.Join(ks, "+")
	switch st.status() {
	case passed:
		mark, right = s.green("✓"), s.dim(fmt.Sprintf("%d/%d", pass, len(st.tests)))
	case failed:
		mark, right = s.red("✗"), s.red(fmt.Sprintf("%d/%d", pass, len(st.tests)))
	case broken:
		mark, right = s.red("!"), s.red("does not compile")
	case skipped:
		mark, right = s.yellow("–"), s.dim("skipped")
	case -1:
		mark, right = s.yellow("◐"), s.yellow(fmt.Sprintf("%d/%d", pass, len(st.tests)))
	default:
		mark, right = s.dim("○"), s.dim("not written yet")
	}
	name := title(st)
	pad := 48 - len([]rune(name))
	if pad < 1 {
		pad = 1
	}
	pf(w, "    %s %s  %s%s%s  %s\n", mark, s.bold(fmt.Sprintf("%-3s", st.label)), name, strings.Repeat(" ", pad), s.dim(fmt.Sprintf("%-9s", tag)), right)
	if st.status() == broken && st.tests[0].kind == "app" {
		for _, l := range st.tests[0].out {
			ln(w, "            "+s.dim(strings.TrimSpace(l)))
		}
		return
	}
	for _, t := range st.tests {
		if t.status != failed {
			continue
		}
		pf(w, "          %s %s\n", s.red("✗"), t.title)
		shown := 0
		for _, l := range t.out {
			if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "FAIL") || strings.HasPrefix(strings.TrimSpace(l), "PASS") {
				continue
			}
			if !verbose && shown == 6 {
				ln(w, "            "+s.dim("... run with -v for the full output"))
				break
			}
			ln(w, "            "+s.dim(strings.TrimSpace(l)))
			shown++
		}
	}
}
