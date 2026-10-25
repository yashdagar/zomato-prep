package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type style struct{ color bool }

func newStyle(f *os.File) style {
	if os.Getenv("NO_COLOR") != "" {
		return style{}
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return style{true}
	}
	return style{isTTY(f)}
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func (s style) wrap(code, t string) string {
	if !s.color {
		return t
	}
	return "\x1b[" + code + "m" + t + "\x1b[0m"
}

func (s style) red(t string) string    { return s.wrap("1;38;2;226;55;68", t) }
func (s style) green(t string) string  { return s.wrap("32", t) }
func (s style) yellow(t string) string { return s.wrap("33", t) }
func (s style) dim(t string) string    { return s.wrap("2", t) }
func (s style) bold(t string) string   { return s.wrap("1", t) }

func (s style) bar(done, total, width int) string {
	if total == 0 {
		return s.dim(strings.Repeat("─", width))
	}
	n := done * width / total
	fill := strings.Repeat("━", n)
	if done == total {
		fill = s.green(fill)
	} else {
		fill = s.red(fill)
	}
	return fill + s.dim(strings.Repeat("─", width-n))
}

func pf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func ln(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }
