package deploy_test

import (
	"regexp"
	"strings"
	"testing"
)

var binaries = []string{"api", "worker"}

func TestW7B1_DockerfilesAreMultiStageWithGoBuilderAndDistrolessRuntime(t *testing.T) {
	for _, bin := range binaries {
		ins := dockerfile(t, "Dockerfile."+bin)
		var froms []string
		for _, in := range ins {
			if in.op == "FROM" {
				froms = append(froms, in.args)
			}
		}
		if len(froms) < 2 {
			t.Fatalf("Dockerfile.%s has %d FROM lines, want a build stage and a runtime stage", bin, len(froms))
		}
		if !strings.HasPrefix(froms[0], "golang:") {
			t.Errorf("Dockerfile.%s: build stage is %q, want a golang: image", bin, froms[0])
		}
		if !strings.HasPrefix(froms[len(froms)-1], "gcr.io/distroless/static") {
			t.Errorf("Dockerfile.%s: runtime stage is %q, want gcr.io/distroless/static", bin, froms[len(froms)-1])
		}
	}
}

func TestW7B1_BinariesAreBuiltStaticallyFromTheRightPackage(t *testing.T) {
	for _, bin := range binaries {
		built := false
		for _, in := range dockerfile(t, "Dockerfile."+bin) {
			if in.op == "RUN" && strings.Contains(in.args, "CGO_ENABLED=0") && strings.Contains(in.args, "go build") && strings.Contains(in.args, "./cmd/"+bin) {
				built = true
			}
		}
		if !built {
			t.Errorf("Dockerfile.%s: no RUN line with CGO_ENABLED=0 and go build ./cmd/%s", bin, bin)
		}
	}
}

func TestW7B1_RuntimeStageRunsAsNonRoot(t *testing.T) {
	for _, bin := range binaries {
		user := ""
		for _, in := range finalStage(dockerfile(t, "Dockerfile."+bin)) {
			if in.op == "USER" {
				user = in.args
			}
		}
		switch user {
		case "":
			t.Errorf("Dockerfile.%s: runtime stage has no USER line", bin)
		case "root", "0", "0:0", "root:root":
			t.Errorf("Dockerfile.%s: runtime stage runs as %q", bin, user)
		}
	}
}

func TestW7B1_RuntimeStageCopiesTheBinaryAndRunsNoShellCommands(t *testing.T) {
	for _, bin := range binaries {
		copied, entrypointExec := false, false
		for _, in := range finalStage(dockerfile(t, "Dockerfile."+bin)) {
			switch in.op {
			case "RUN":
				t.Errorf("Dockerfile.%s: runtime stage has RUN %q, distroless has no shell", bin, in.args)
			case "COPY":
				if strings.Contains(in.args, "--from=") {
					copied = true
				}
			case "ENTRYPOINT":
				entrypointExec = strings.HasPrefix(in.args, "[")
			}
		}
		if !copied {
			t.Errorf("Dockerfile.%s: runtime stage does not COPY --from the build stage", bin)
		}
		if !entrypointExec {
			t.Errorf("Dockerfile.%s: ENTRYPOINT must use the exec form, for example [\"/api\"]", bin)
		}
	}
}

func TestW7B1_ModuleDownloadIsCachedBeforeTheSourceIsCopied(t *testing.T) {
	for _, bin := range binaries {
		download, copyAll := -1, -1
		for i, in := range dockerfile(t, "Dockerfile."+bin) {
			if in.op == "RUN" && strings.Contains(in.args, "go mod download") && download < 0 {
				download = i
			}
			if in.op == "COPY" && regexp.MustCompile(`^\.\s+\S+$`).MatchString(strings.TrimSpace(in.args)) && copyAll < 0 {
				copyAll = i
			}
		}
		if download < 0 || copyAll < 0 || download > copyAll {
			t.Errorf("Dockerfile.%s: run go mod download before COPY . . so the dependency layer is cached", bin)
		}
	}
}

func TestW7B1_NoSecretsAreBakedIntoTheImages(t *testing.T) {
	secret := regexp.MustCompile(`(?i)(PASSWORD|SECRET|TOKEN|API_?KEY|DSN)`)
	for _, bin := range binaries {
		for _, in := range dockerfile(t, "Dockerfile."+bin) {
			if (in.op == "ENV" || in.op == "ARG") && secret.MatchString(in.args) {
				t.Errorf("Dockerfile.%s: %s %s puts a secret into an image layer", bin, in.op, in.args)
			}
		}
	}
}

func TestW7B1_DockerignoreKeepsSecretsAndHistoryOutOfTheBuildContext(t *testing.T) {
	lines := map[string]bool{}
	for _, l := range strings.Split(readFile(t, ".dockerignore"), "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	for _, want := range []string{".env", ".git"} {
		if !lines[want] && !lines[want+"/"] {
			t.Errorf(".dockerignore does not list %s", want)
		}
	}
}

func TestW7B1_EnvExampleListsEveryVariableComposeNeedsAndRealEnvIsIgnored(t *testing.T) {
	example := readFile(t, ".env.example")
	have := map[string]bool{}
	for _, l := range strings.Split(example, "\n") {
		if k, _, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			have[k] = true
		}
	}
	for _, want := range []string{"MYSQL_ROOT_PASSWORD", "JWT_SECRET", "API_DOMAIN"} {
		if !have[want] {
			t.Errorf(".env.example does not define %s", want)
		}
	}
	for _, m := range regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)`).FindAllStringSubmatch(readFile(t, "compose.prod.yml"), -1) {
		if !have[m[1]] {
			t.Errorf("compose.prod.yml uses ${%s} but .env.example does not define it", m[1])
		}
	}
	ignored := false
	for _, l := range strings.Split(readRepoFile(t, ".gitignore"), "\n") {
		switch strings.TrimSpace(l) {
		case ".env", "/.env", "backend/.env", "**/.env":
			ignored = true
		case ".env*", "*.env*", ".env.*":
			t.Error(".gitignore must not ignore .env.example")
		}
	}
	if !ignored {
		t.Error(".gitignore does not ignore .env")
	}
}

func TestW7B1_ComposeDefinesEveryProductionService(t *testing.T) {
	services := composeServices(t)
	for _, name := range []string{"mysql", "redis", "redpanda", "api", "worker", "caddy"} {
		if _, ok := services[name]; !ok {
			t.Errorf("compose.prod.yml has no %s service", name)
		}
	}
	for bin, image := range map[string]string{"api": "ghcr.io/yashdagar/zomato-api", "worker": "ghcr.io/yashdagar/zomato-worker"} {
		svc := services[bin]
		if got := asString(svc["image"]); !strings.HasPrefix(got, image+":") {
			t.Errorf("service %s: image = %q, want %s:<tag>", bin, got, image)
		}
		build := asMap(svc["build"])
		if asString(build["dockerfile"]) != "Dockerfile."+bin {
			t.Errorf("service %s: build.dockerfile = %q, want Dockerfile.%s", bin, asString(build["dockerfile"]), bin)
		}
		if ctx := asString(build["context"]); ctx != "." && ctx != "./" {
			t.Errorf("service %s: build.context = %q, want . (the backend directory)", bin, ctx)
		}
	}
	for name, prefix := range map[string]string{"mysql": "mysql:8.4", "redis": "redis:7", "redpanda": "redpandadata/redpanda", "caddy": "caddy:2"} {
		if got := asString(services[name]["image"]); !strings.HasPrefix(got, prefix) {
			t.Errorf("service %s: image = %q, want %s", name, got, prefix)
		}
	}
}

func TestW7B1_ServicesRestartUnlessStopped(t *testing.T) {
	for _, name := range []string{"mysql", "redis", "redpanda", "api", "worker", "caddy"} {
		if r := asString(composeService(t, name)["restart"]); r != "unless-stopped" && r != "always" {
			t.Errorf("service %s: restart = %q, want unless-stopped", name, r)
		}
	}
}

func TestW7B1_StatefulServicesUseNamedVolumes(t *testing.T) {
	doc := parseYAML(t, readFile(t, "compose.prod.yml"))
	declared := asMap(doc["volumes"])
	for name, target := range map[string]string{"mysql": "/var/lib/mysql", "redpanda": "/var/lib/redpanda/data"} {
		found := false
		for _, v := range asList(composeService(t, name)["volumes"]) {
			src, dst, _ := strings.Cut(asString(v), ":")
			if m := asMap(v); m != nil {
				src, dst = asString(m["source"]), asString(m["target"])
			}
			if strings.HasPrefix(dst, target) {
				if _, ok := declared[src]; ok {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("service %s: mount a named volume declared under top-level volumes at %s", name, target)
		}
	}
}

func TestW7B1_ApiAndWorkerWaitForAHealthyMysql(t *testing.T) {
	if len(asList(asMap(composeService(t, "mysql")["healthcheck"])["test"])) == 0 {
		t.Error("service mysql has no healthcheck")
	}
	for _, name := range []string{"api", "worker"} {
		if c := dependsOn(composeService(t, name))["mysql"]; c != "service_healthy" {
			t.Errorf("service %s: depends_on mysql condition = %q, want service_healthy", name, c)
		}
	}
}

func TestW7B1_PasswordsAndSecretsAreInterpolatedNeverLiteral(t *testing.T) {
	secret := regexp.MustCompile(`(?i)(PASSWORD|SECRET)`)
	for name, svc := range composeServices(t) {
		for k, v := range envMap(svc) {
			if secret.MatchString(k) && !strings.Contains(v, "${") {
				t.Errorf("service %s: %s = %q is a literal, interpolate it from .env", name, k, v)
			}
		}
	}
	for _, name := range []string{"api", "worker"} {
		if _, ok := composeService(t, name)["image"]; !ok {
			if _, ok := composeService(t, name)["build"]; !ok {
				t.Errorf("service %s needs an image or a build section", name)
			}
		}
	}
}
