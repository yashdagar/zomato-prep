package deploy_test

import (
	"regexp"
	"strings"
	"testing"
)

const workflowPath = ".github/workflows/backend.yml"

type step struct {
	uses string
	run  string
	with map[string]any
}

type job struct {
	name     string
	needs    []string
	cond     string
	services map[string]any
	env      map[string]string
	perms    map[string]any
	steps    []step
}

func workflowDoc(t *testing.T) map[string]any {
	t.Helper()
	return parseYAML(t, readRepoFile(t, workflowPath))
}

func workflowJobs(t *testing.T) []job {
	t.Helper()
	var jobs []job
	for name, v := range asMap(workflowDoc(t)["jobs"]) {
		m := asMap(v)
		j := job{name: name, cond: asString(m["if"]), services: asMap(m["services"]), perms: asMap(m["permissions"]), env: map[string]string{}}
		switch n := m["needs"].(type) {
		case string:
			j.needs = []string{n}
		case []any:
			for _, x := range n {
				j.needs = append(j.needs, asString(x))
			}
		}
		for k, e := range asMap(m["env"]) {
			j.env[k] = toString(e)
		}
		for _, s := range asList(m["steps"]) {
			sm := asMap(s)
			j.steps = append(j.steps, step{uses: asString(sm["uses"]), run: asString(sm["run"]), with: asMap(sm["with"])})
		}
		jobs = append(jobs, j)
	}
	if len(jobs) == 0 {
		t.Fatalf("%s defines no jobs", workflowPath)
	}
	return jobs
}

func (j job) usesAction(prefix string) bool {
	for _, s := range j.steps {
		if strings.HasPrefix(s.uses, prefix) {
			return true
		}
	}
	return false
}

func (j job) runs(substr string) bool {
	for _, s := range j.steps {
		if strings.Contains(s.run, substr) {
			return true
		}
	}
	return false
}

func testJob(t *testing.T) job {
	t.Helper()
	for _, j := range workflowJobs(t) {
		if len(j.services) > 0 && j.runs("-race") {
			return j
		}
	}
	t.Fatal("no job has service containers and runs go test with -race")
	return job{}
}

func buildJobs(t *testing.T) []job {
	t.Helper()
	var out []job
	for _, j := range workflowJobs(t) {
		if j.usesAction("docker/build-push-action") {
			out = append(out, j)
		}
	}
	if len(out) == 0 {
		t.Fatal("no job uses docker/build-push-action")
	}
	return out
}

func TestW7B2_WorkflowRunsOnPushAndPullRequest(t *testing.T) {
	on := asMap(workflowDoc(t)["on"])
	for _, ev := range []string{"push", "pull_request"} {
		if _, ok := on[ev]; !ok {
			t.Errorf("workflow does not trigger on %s", ev)
		}
	}
}

func TestW7B2_TestJobRunsMysqlAndRedisAsHealthCheckedServices(t *testing.T) {
	j := testJob(t)
	for name, prefix := range map[string]string{"mysql": "mysql:", "redis": "redis:"} {
		svc := asMap(j.services[name])
		if !strings.HasPrefix(asString(svc["image"]), prefix) {
			t.Errorf("service %s: image = %q, want a %s image", name, asString(svc["image"]), prefix)
		}
		if !strings.Contains(asString(svc["options"]), "--health-cmd") {
			t.Errorf("service %s: options must define --health-cmd so steps wait for readiness", name)
		}
		if len(asList(svc["ports"])) == 0 {
			t.Errorf("service %s: map its port so tests can reach 127.0.0.1", name)
		}
	}
}

func TestW7B2_TestJobPointsTestsAtTheServices(t *testing.T) {
	j := testJob(t)
	if !strings.HasPrefix(j.env["TEST_REDIS_ADDR"], "127.0.0.1:") {
		t.Errorf("TEST_REDIS_ADDR = %q, want 127.0.0.1:6379", j.env["TEST_REDIS_ADDR"])
	}
	if !strings.Contains(j.env["TEST_MYSQL_DSN"], "127.0.0.1") {
		t.Errorf("TEST_MYSQL_DSN = %q, want a DSN for 127.0.0.1", j.env["TEST_MYSQL_DSN"])
	}
}

func TestW7B2_TestJobLintsAndRunsRaceTests(t *testing.T) {
	j := testJob(t)
	if !j.runs("golangci-lint") && !j.runs("make lint") && !j.usesAction("golangci/golangci-lint-action") {
		t.Error("test job does not run the linter")
	}
	if !j.runs("go test") || !j.runs("-race") {
		t.Error("test job does not run go test -race")
	}
}

func TestW7B2_ImagesAreBuiltOnlyAfterTestsPassOnMain(t *testing.T) {
	tj := testJob(t)
	for _, b := range buildJobs(t) {
		needs := false
		for _, n := range b.needs {
			if n == tj.name {
				needs = true
			}
		}
		if !needs {
			t.Errorf("job %s builds images but does not need %s", b.name, tj.name)
		}
		if !strings.Contains(b.cond, "refs/heads/main") {
			t.Errorf("job %s: if = %q, want a condition on refs/heads/main", b.name, b.cond)
		}
	}
}

func TestW7B2_BothImagesArePushedToGhcrTaggedWithTheCommitSha(t *testing.T) {
	src := readRepoFile(t, workflowPath)
	for _, img := range []string{"ghcr.io/yashdagar/zomato-api", "ghcr.io/yashdagar/zomato-worker"} {
		if !strings.Contains(src, img) {
			t.Errorf("workflow never mentions %s", img)
		}
	}
	if !strings.Contains(src, "github.sha") {
		t.Error("images are not tagged with github.sha")
	}
	if regexp.MustCompile(`(?m)zomato-(api|worker):latest`).MatchString(src) {
		t.Error("do not tag images with latest")
	}
	pushes := 0
	for _, b := range buildJobs(t) {
		for _, s := range b.steps {
			if strings.HasPrefix(s.uses, "docker/build-push-action") && toString(s.with["push"]) == "true" {
				pushes++
			}
		}
	}
	if pushes < 2 {
		t.Errorf("found %d pushing build-push-action steps, want one per image", pushes)
	}
}

func TestW7B2_BuildStepsUseTheBackendContextAndBothDockerfiles(t *testing.T) {
	var contexts, files []string
	for _, b := range buildJobs(t) {
		for _, s := range b.steps {
			if strings.HasPrefix(s.uses, "docker/build-push-action") {
				contexts = append(contexts, toString(s.with["context"]))
				files = append(files, toString(s.with["file"]))
			}
		}
	}
	for _, c := range contexts {
		if strings.TrimSuffix(c, "/") != "backend" && strings.TrimSuffix(c, "/") != "./backend" {
			t.Errorf("context = %q, want backend", c)
		}
	}
	joined := strings.Join(files, " ")
	for _, f := range []string{"Dockerfile.api", "Dockerfile.worker"} {
		if !strings.Contains(joined, f) {
			t.Errorf("no build step uses %s", f)
		}
	}
}

func TestW7B2_BuildJobCanLogInToGhcrWithTheBuiltInToken(t *testing.T) {
	for _, b := range buildJobs(t) {
		login := false
		for _, s := range b.steps {
			if strings.HasPrefix(s.uses, "docker/login-action") && toString(s.with["registry"]) == "ghcr.io" && strings.Contains(toString(s.with["password"]), "secrets.GITHUB_TOKEN") {
				login = true
			}
		}
		if !login {
			t.Errorf("job %s: add docker/login-action for ghcr.io with secrets.GITHUB_TOKEN", b.name)
		}
		perms := b.perms
		if perms == nil {
			perms = asMap(workflowDoc(t)["permissions"])
		}
		if toString(perms["packages"]) != "write" {
			t.Errorf("job %s: permissions.packages must be write", b.name)
		}
	}
}

func TestW7B2_EveryActionIsPinnedToAVersion(t *testing.T) {
	for _, j := range workflowJobs(t) {
		for _, s := range j.steps {
			if s.uses == "" {
				continue
			}
			_, ref, ok := strings.Cut(s.uses, "@")
			if !ok || ref == "main" || ref == "master" || ref == "" {
				t.Errorf("job %s: %q is not pinned to a version", j.name, s.uses)
			}
		}
	}
}

func TestW7B2_WorkflowHasNoLiteralCredentials(t *testing.T) {
	src := readRepoFile(t, workflowPath)
	for _, line := range strings.Split(src, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "password:") && !strings.Contains(l, "${{") {
			t.Errorf("literal credential in workflow: %s", l)
		}
	}
	if strings.Contains(src, "ghp_") {
		t.Error("workflow contains a personal access token")
	}
}
