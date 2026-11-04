package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const root = "../../.."

func read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("%s is missing: %v", rel, err)
	}
	return string(b)
}

func section(t *testing.T, doc, name string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	var out []string
	in := false
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			if in {
				break
			}
			in = strings.Contains(strings.ToLower(l), strings.ToLower(name))
			continue
		}
		if in {
			out = append(out, l)
		}
	}
	if !in && len(out) == 0 {
		t.Fatalf("no ## section whose title contains %q", name)
	}
	return strings.Join(out, "\n")
}

func words(s string) int { return len(strings.Fields(s)) }

func TestW8B5_ReadmeHasTheEngineeringPostSections(t *testing.T) {
	doc := read(t, "README.md")
	for _, name := range []string{"problem", "architecture", "numbers", "hardest bugs", "next"} {
		if words(section(t, doc, name)) < 25 {
			t.Errorf("the %q section needs at least 25 words", name)
		}
	}
}

func TestW8B5_ArchitectureHasAMermaidFlowchartAndASequenceDiagram(t *testing.T) {
	arch := section(t, read(t, "README.md"), "architecture")
	blocks := regexp.MustCompile("(?s)```mermaid\n(.*?)```").FindAllStringSubmatch(arch, -1)
	var flow, seq bool
	for _, b := range blocks {
		first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(b[1]), "\n", 2)[0])
		flow = flow || strings.HasPrefix(first, "flowchart")
		seq = seq || first == "sequenceDiagram"
	}
	if !flow || !seq {
		t.Fatalf("architecture needs one mermaid flowchart and one sequenceDiagram, found flowchart=%v sequenceDiagram=%v", flow, seq)
	}
}

func TestW8B5_NumbersTableStatesHardwareOnEveryRow(t *testing.T) {
	nums := section(t, read(t, "README.md"), "numbers")
	var rows [][]string
	for _, l := range strings.Split(nums, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") || strings.Contains(l, "---") {
			continue
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(l, "|"), "|") {
			cells = append(cells, strings.TrimSpace(c))
		}
		rows = append(rows, cells)
	}
	if len(rows) < 3 {
		t.Fatalf("the numbers table needs a header and at least 2 rows, found %d lines", len(rows))
	}
	col := -1
	for i, c := range rows[0] {
		if strings.EqualFold(c, "hardware") {
			col = i
		}
	}
	if col < 0 {
		t.Fatal("the numbers table needs a Hardware column")
	}
	for _, r := range rows[1:] {
		if len(r) != len(rows[0]) {
			t.Errorf("row %v has %d cells, want %d", r, len(r), len(rows[0]))
			continue
		}
		for _, c := range r {
			if c == "" {
				t.Errorf("row %v has an empty cell", r)
			}
		}
	}
}

func TestW8B5_ThreeHardestBugsAreWrittenAsStories(t *testing.T) {
	bugs := section(t, read(t, "README.md"), "hardest bugs")
	parts := regexp.MustCompile(`(?m)^### .+$`).Split(bugs, -1)
	if len(parts) < 4 {
		t.Fatalf("need three ### subsections under the bugs section, found %d", len(parts)-1)
	}
	for i, p := range parts[1:] {
		if words(p) < 40 {
			t.Errorf("bug %d has %d words, want at least 40", i+1, words(p))
		}
	}
}

func TestW8B5_ReadmeUsesNoEmDashesOrEnDashes(t *testing.T) {
	if doc := read(t, "README.md"); strings.ContainsAny(doc, "–—") {
		t.Fatal("README.md contains an em dash or en dash")
	}
}

func TestW8B5_RelativeLinksInTheReadmePointAtRealFiles(t *testing.T) {
	doc := read(t, "README.md")
	for _, m := range regexp.MustCompile(`\]\(([^)\s]+)\)`).FindAllStringSubmatch(doc, -1) {
		link := m[1]
		if strings.Contains(link, "://") || strings.HasPrefix(link, "#") || strings.HasPrefix(link, "mailto:") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, strings.SplitN(link, "#", 2)[0])); err != nil {
			t.Errorf("README.md links to %s, which does not exist", link)
		}
	}
}

func TestW8B5_ThreeADRsFollowTheContextDecisionConsequencesShape(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(root, "docs", "adr", "*.md"))
	sort.Strings(files)
	if len(files) < 3 {
		t.Fatalf("docs/adr needs at least 3 ADRs, found %d", len(files))
	}
	all := ""
	for _, f := range files {
		b, _ := os.ReadFile(f)
		all += strings.ToLower(filepath.Base(f)) + "\n"
		for _, h := range []string{"## Status", "## Context", "## Decision", "## Consequences"} {
			if !strings.Contains(string(b), h) {
				t.Errorf("%s is missing %q", filepath.Base(f), h)
			}
		}
	}
	for _, topic := range []string{"mysql", "outbox", "websocket"} {
		if !strings.Contains(all, topic) {
			t.Errorf("no ADR file name mentions %q", topic)
		}
	}
}

func TestW8B5_APostmortemCoversTimelineCauseAndFollowUps(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(root, "docs", "postmortem-*.md"))
	if len(files) < 1 {
		t.Fatal("docs/postmortem-<name>.md is missing")
	}
	b, _ := os.ReadFile(files[0])
	for _, h := range []string{"## Summary", "## Timeline", "## Root cause", "## Follow-ups"} {
		if !strings.Contains(string(b), h) {
			t.Errorf("%s is missing %q", filepath.Base(files[0]), h)
		}
	}
}

func TestW8B5_ARetroSaysWhichWeeksSlipped(t *testing.T) {
	doc := read(t, "docs/retro.md")
	for _, h := range []string{"## What slipped", "## Why", "## Next"} {
		if !strings.Contains(doc, h) {
			t.Errorf("docs/retro.md is missing %q", h)
		}
	}
	if len(regexp.MustCompile(`(?i)week ?[1-8]`).FindAllString(doc, -1)) < 2 {
		t.Error("docs/retro.md must name at least two weeks")
	}
}

func fieldsOf(doc string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(doc, "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "-") {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return out
}

func TestW8B6_ReadmeLinksTheDemoVideo(t *testing.T) {
	demo := section(t, read(t, "README.md"), "demo")
	if !regexp.MustCompile(`\[[^\]]+\]\((https://[^)\s]+|docs/[^)\s]+\.(mp4|mov|webm|gif))\)`).MatchString(demo) {
		t.Fatal("the Demo section needs a markdown link to the video (https URL or a file under docs/)")
	}
}

var beat = regexp.MustCompile(`(?m)^- (\d{2}):(\d{2}) (.+)$`)

func TestW8B6_DemoScriptIsTimedAndFitsTwoMinutes(t *testing.T) {
	beats := beat.FindAllStringSubmatch(read(t, "docs/demo-script.md"), -1)
	if len(beats) < 5 {
		t.Fatalf("need at least 5 timed beats like '- 00:15 ...', found %d", len(beats))
	}
	prev := -1
	for _, b := range beats {
		sec := atoi(b[1])*60 + atoi(b[2])
		if sec <= prev {
			t.Errorf("beat at %s:%s is not after the previous one", b[1], b[2])
		}
		if sec > 120 {
			t.Errorf("beat at %s:%s is past 02:00", b[1], b[2])
		}
		prev = sec
	}
}

func TestW8B6_EveryDemoBeatShowsTheCustomerAndTheRiderSideBySide(t *testing.T) {
	for _, b := range beat.FindAllStringSubmatch(read(t, "docs/demo-script.md"), -1) {
		l := strings.ToLower(b[3])
		if !strings.Contains(l, "customer") || !strings.Contains(l, "rider") {
			t.Errorf("beat %s:%s must mention both the customer and the rider: %q", b[1], b[2], b[3])
		}
	}
}

func TestW8B6_PortfolioEntryHasTheRequiredFields(t *testing.T) {
	f := fieldsOf(read(t, "docs/portfolio-entry.md"))
	for _, k := range []string{"title", "summary", "stack", "repo", "demo"} {
		if f[k] == "" {
			t.Errorf("docs/portfolio-entry.md needs a non-empty %q line", k)
		}
	}
	if n := len([]rune(f["summary"])); n > 280 {
		t.Errorf("summary is %d characters, want at most 280", n)
	}
	if !strings.HasPrefix(f["repo"], "https://github.com/") {
		t.Errorf("repo = %q, want a https://github.com/ link", f["repo"])
	}
	if !strings.Contains(strings.ToLower(f["stack"]), "go") {
		t.Errorf("stack = %q, want it to name Go", f["stack"])
	}
}

var budget = regexp.MustCompile(`^(.+) \((\d+) s\)$`)

func walkthrough(t *testing.T) (names []string, secs map[string]int, bodies map[string]string) {
	t.Helper()
	doc := read(t, "docs/walkthrough.md")
	secs, bodies = map[string]int{}, map[string]string{}
	parts := regexp.MustCompile(`(?m)^## (.+)$`).FindAllStringSubmatchIndex(doc, -1)
	for i, p := range parts {
		title := strings.TrimSpace(doc[p[2]:p[3]])
		end := len(doc)
		if i+1 < len(parts) {
			end = parts[i+1][0]
		}
		if m := budget.FindStringSubmatch(title); m != nil {
			names = append(names, strings.ToLower(m[1]))
			secs[strings.ToLower(m[1])] = atoi(m[2])
			bodies[strings.ToLower(m[1])] = doc[p[1]:end]
		}
	}
	return
}

func TestW8B7_WalkthroughHasTheFiveTimedPartsInOrder(t *testing.T) {
	names, _, bodies := walkthrough(t)
	want := []string{"problem", "architecture", "idempotency and outbox", "load test", "rider claim"}
	if len(names) != len(want) {
		t.Fatalf("found timed sections %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("section %d is %q, want %q", i+1, names[i], w)
		}
		if words(bodies[names[i]]) < 20 {
			t.Errorf("section %q needs at least 20 words of script", names[i])
		}
	}
}

func TestW8B7_WalkthroughFitsFiveMinutesAndSpendsTheMostOnIdempotency(t *testing.T) {
	names, secs, _ := walkthrough(t)
	total := 0
	for _, n := range names {
		total += secs[n]
		if n != "idempotency and outbox" && secs[n] >= secs["idempotency and outbox"] {
			t.Errorf("%q gets %d s, the idempotency and outbox story must get the most time", n, secs[n])
		}
	}
	if total > 300 || total < 240 {
		t.Fatalf("budgets add up to %d s, want between 240 and 300", total)
	}
}

func TestW8B7_WalkthroughAnswersWhatBreaksFirstAtTenTimesTraffic(t *testing.T) {
	ans := section(t, read(t, "docs/walkthrough.md"), "10x")
	if words(ans) < 30 || !regexp.MustCompile(`\d`).MatchString(ans) {
		t.Fatal("the 10x section needs at least 30 words and one number")
	}
}

func TestW8B7_TwoTimedRehearsalsAreLoggedUnderFiveMinutes(t *testing.T) {
	runs := regexp.MustCompile(`(?m)^- run \d+: (\d+)m(\d{2})s`).FindAllStringSubmatch(read(t, "docs/rehearsals.md"), -1)
	if len(runs) < 2 {
		t.Fatalf("need at least 2 lines like '- run 1: 4m52s', found %d", len(runs))
	}
	for _, r := range runs {
		if atoi(r[1])*60+atoi(r[2]) > 300 {
			t.Errorf("rehearsal %sm%ss is over 5 minutes", r[1], r[2])
		}
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
