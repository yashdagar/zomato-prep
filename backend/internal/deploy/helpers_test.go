package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const backendDir = "../.."

func readFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(backendDir, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(backendDir, "..", rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

func parseYAML(t *testing.T, src string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}
	return doc
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func composeServices(t *testing.T) map[string]map[string]any {
	t.Helper()
	doc := parseYAML(t, readFile(t, "compose.prod.yml"))
	out := map[string]map[string]any{}
	for name, v := range asMap(doc["services"]) {
		out[name] = asMap(v)
	}
	if len(out) == 0 {
		t.Fatal("compose.prod.yml defines no services")
	}
	return out
}

func composeService(t *testing.T, name string) map[string]any {
	t.Helper()
	s, ok := composeServices(t)[name]
	if !ok {
		t.Fatalf("compose.prod.yml has no service %q", name)
	}
	return s
}

func dependsOn(svc map[string]any) map[string]string {
	out := map[string]string{}
	switch d := svc["depends_on"].(type) {
	case []any:
		for _, n := range d {
			out[asString(n)] = ""
		}
	case map[string]any:
		for n, v := range d {
			out[n] = asString(asMap(v)["condition"])
		}
	}
	return out
}

func envMap(svc map[string]any) map[string]string {
	out := map[string]string{}
	switch e := svc["environment"].(type) {
	case []any:
		for _, kv := range e {
			k, v, _ := strings.Cut(asString(kv), "=")
			out[k] = v
		}
	case map[string]any:
		for k, v := range e {
			out[k] = strings.TrimSpace(strings.Trim(strings.TrimSpace(toString(v)), "\""))
		}
	}
	return out
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		b, _ := yaml.Marshal(x)
		return strings.TrimSpace(string(b))
	}
}

var dockerfileInstr = regexp.MustCompile(`^\s*([A-Za-z]+)\s+(.*)$`)

type instruction struct{ op, args string }

func dockerfile(t *testing.T, name string) []instruction {
	t.Helper()
	src := readFile(t, name)
	var out []instruction
	var pending string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasSuffix(trimmed, "\\") {
			pending += strings.TrimSuffix(trimmed, "\\") + " "
			continue
		}
		full := pending + trimmed
		pending = ""
		if m := dockerfileInstr.FindStringSubmatch(full); m != nil {
			out = append(out, instruction{strings.ToUpper(m[1]), strings.TrimSpace(m[2])})
		}
	}
	return out
}

func finalStage(ins []instruction) []instruction {
	last := -1
	for i, in := range ins {
		if in.op == "FROM" {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	return ins[last:]
}
