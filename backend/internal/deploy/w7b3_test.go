package deploy_test

import (
	"regexp"
	"strings"
	"testing"
)

func TestW7B3_CaddyfileProxiesTheConfiguredDomainToTheApi(t *testing.T) {
	src := readFile(t, "Caddyfile")
	if !strings.Contains(src, "{$API_DOMAIN}") {
		t.Error("Caddyfile: the site address must come from {$API_DOMAIN}")
	}
	if !strings.Contains(src, "reverse_proxy") || !strings.Contains(src, "api:8080") {
		t.Error("Caddyfile: reverse_proxy to api:8080 is missing")
	}
	if regexp.MustCompile(`(?m)^\s*http://`).MatchString(src) {
		t.Error("Caddyfile: an http:// site address disables automatic HTTPS")
	}
	if strings.Contains(src, "auto_https off") || strings.Contains(src, "tls internal") {
		t.Error("Caddyfile: leave automatic HTTPS on, Caddy picks a local CA for localhost by itself")
	}
}

func TestW7B3_CaddyHealthChecksTheApiThroughReadyz(t *testing.T) {
	src := readFile(t, "Caddyfile")
	if !regexp.MustCompile(`health_uri\s+/readyz`).MatchString(src) {
		t.Error("Caddyfile: add health_uri /readyz inside reverse_proxy")
	}
}

func TestW7B3_CaddyServiceExposesWebPortsAndPersistsCertificates(t *testing.T) {
	caddy := composeService(t, "caddy")
	published := map[string]bool{}
	for _, p := range asList(caddy["ports"]) {
		s := toString(p)
		if m := asMap(p); m != nil {
			s = toString(m["published"]) + ":" + toString(m["target"])
		}
		published[strings.Split(s, ":")[0]] = true
	}
	if !published["80"] || !published["443"] {
		t.Errorf("caddy ports = %v, want 80 and 443 published", caddy["ports"])
	}
	var mountsFile, mountsData bool
	for _, v := range asList(caddy["volumes"]) {
		s := toString(v)
		if strings.Contains(s, "Caddyfile") && strings.Contains(s, "/etc/caddy/Caddyfile") {
			mountsFile = true
		}
		if strings.HasPrefix(s, "caddy_data:") && strings.Contains(s, "/data") {
			mountsData = true
		}
	}
	if !mountsFile {
		t.Error("caddy must mount ./Caddyfile at /etc/caddy/Caddyfile")
	}
	if !mountsData {
		t.Error("caddy must keep certificates in the named volume caddy_data mounted at /data")
	}
	if _, ok := dependsOn(caddy)["api"]; !ok {
		t.Error("caddy must depend on api")
	}
}

func TestW7B3_OnlyCaddyIsPublishedToTheInternet(t *testing.T) {
	for _, name := range []string{"api", "worker", "mysql", "redis", "redpanda"} {
		for _, p := range asList(composeService(t, name)["ports"]) {
			s := toString(p)
			if m := asMap(p); m != nil {
				s = toString(m["host_ip"]) + ":" + toString(m["published"])
			}
			if !strings.HasPrefix(s, "127.0.0.1:") {
				t.Errorf("service %s publishes %q on all interfaces; only caddy may, bind others to 127.0.0.1", name, s)
			}
		}
	}
}

func TestW7B3_MigrationsRunAsAOneShotMigrateServiceBeforeTheApi(t *testing.T) {
	m := composeService(t, "migrate")
	if !strings.HasPrefix(asString(m["image"]), "migrate/migrate") {
		t.Errorf("migrate image = %q, want migrate/migrate", asString(m["image"]))
	}
	if r := asString(m["restart"]); r != "no" && r != "" && r != "false" {
		t.Errorf("migrate restart = %q, a one-shot job must not restart", r)
	}
	cmd := toString(m["command"])
	if !strings.Contains(cmd, "up") || !strings.Contains(cmd, "-path") || !strings.Contains(cmd, "mysql://") {
		t.Errorf("migrate command = %q, want -path, a mysql:// database URL and up", cmd)
	}
	mounted := false
	for _, v := range asList(m["volumes"]) {
		if strings.Contains(toString(v), "db/migrations") {
			mounted = true
		}
	}
	if !mounted {
		t.Error("migrate must mount ./db/migrations")
	}
	if c := dependsOn(m)["mysql"]; c != "service_healthy" {
		t.Errorf("migrate depends_on mysql = %q, want service_healthy", c)
	}
	if c := dependsOn(composeService(t, "api"))["migrate"]; c != "service_completed_successfully" {
		t.Errorf("api depends_on migrate = %q, want service_completed_successfully", c)
	}
}

func TestW7B3_MigrateDatabaseUrlComesFromTheEnvFile(t *testing.T) {
	cmd := toString(composeService(t, "migrate")["command"])
	if !strings.Contains(cmd, "${MYSQL_ROOT_PASSWORD") {
		t.Errorf("migrate command = %q, interpolate ${MYSQL_ROOT_PASSWORD} instead of writing the password", cmd)
	}
}

func TestW7B3_ApiHasAHealthcheckThatNeedsNoShell(t *testing.T) {
	hc := asMap(composeService(t, "api")["healthcheck"])
	test := asList(hc["test"])
	if len(test) == 0 {
		t.Fatal("api has no healthcheck")
	}
	if first := asString(test[0]); first != "CMD" {
		t.Errorf("api healthcheck starts with %q, distroless has no shell so use the CMD form", first)
	}
	if !strings.Contains(toString(test), "healthcheck") {
		t.Errorf("api healthcheck %v should call the binary's own -healthcheck flag", test)
	}
}

func TestW7B3_EnvExampleDefaultsToALocalDomain(t *testing.T) {
	if !regexp.MustCompile(`(?m)^API_DOMAIN=localhost\s*$`).MatchString(readFile(t, ".env.example")) {
		t.Error(".env.example must set API_DOMAIN=localhost so the stack runs without DNS")
	}
}
