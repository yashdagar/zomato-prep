package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

func nextStep(all []*test, week int, label string) *step {
	byWeek := steps(all)
	weeks := make([]int, 0, len(byWeek))
	for wk := range byWeek {
		weeks = append(weeks, wk)
	}
	sort.Ints(weeks)
	var order []*step
	for _, wk := range weeks {
		for _, st := range byWeek[wk] {
			if !strings.HasPrefix(st.label, "O") {
				order = append(order, st)
			}
		}
	}
	for i, st := range order {
		if st.week == week && st.label == label && i+1 < len(order) {
			return order[i+1]
		}
	}
	return nil
}

func advise(w io.Writer, s style, tests, all []*test, week int, label string) {
	var n [5]int
	for _, t := range tests {
		n[t.status]++
	}
	if n[passed] == 0 || n[failed]+n[broken]+n[todo] > 0 {
		return
	}
	pf(w, "  %s\n", s.green(fmt.Sprintf("W%d%s passed: %d of %d tests", week, label, n[passed], len(tests))))
	if n[skipped] > 0 {
		pf(w, "  %s\n", s.yellow(fmt.Sprintf("%d tests were skipped, see Skipped above; run them before you move on", n[skipped])))
	}
	next := nextStep(all, week, label)
	if next == nil {
		pf(w, "  %s\n\n", "That was the last step of the course.")
		return
	}
	id := fmt.Sprintf("W%d%s", next.week, next.label)
	ln(w)
	if next.week != week {
		pf(w, "  %s\n", s.bold(fmt.Sprintf("Week %d is done. Week %d starts with %s.", week, next.week, id)))
	}
	pf(w, "  %s %s  %s\n", s.bold("Next"), s.bold(id), title(next))
	pf(w, "    %s   %s\n\n", fmt.Sprintf("%-20s", "make test W="+id), s.dim("read its ticket on the course site, then run this until it passes"))
}
