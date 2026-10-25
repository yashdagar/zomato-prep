package main

import (
	"io"
	"strings"
)

var glyphs = map[rune][5]string{
	'z': {"       ", "███████", "   ███ ", " ███   ", "███████"},
	'o': {"       ", " █████ ", "██   ██", "██   ██", " █████ "},
	'm': {"           ", "██████████ ", "██   ██  ██", "██   ██  ██", "██   ██  ██"},
	'a': {"       ", " █████ ", "     ██", " ██████", "██████▀"},
	't': {" ██    ", "██████ ", " ██    ", " ██    ", "  ████ "},
}

func logo(w io.Writer, s style, sub string) {
	ln(w)
	for r := range 5 {
		var b strings.Builder
		b.WriteString("  ")
		b.WriteString(strings.Repeat(" ", 4-r))
		for _, c := range "zomato" {
			b.WriteString(glyphs[c][r])
			b.WriteString("  ")
		}
		ln(w, s.red(strings.TrimRight(b.String(), " ")))
	}
	if sub != "" {
		ln(w)
		ln(w, "  "+sub)
	}
	ln(w)
}
