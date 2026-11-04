//go:build w7o3

package release_test

import (
	"strings"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/release"
)

const compose = `services:
  api:
    image: ghcr.io/yashdagar/zomato-api:a31f9c2
    restart: unless-stopped
  api-docs:
    image: ghcr.io/yashdagar/zomato-api-docs:0000000
  worker:
    image: ghcr.io/yashdagar/zomato-worker:${WORKER_TAG:-local}
    restart: unless-stopped
  mysql:
    image: mysql:8.4
`

func TestW7O3_ParseImageSplitsRegistryRepoAndTag(t *testing.T) {
	got, err := release.ParseImage("ghcr.io/yashdagar/zomato-api:a31f9c2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Registry != "ghcr.io" || got.Repo != "yashdagar/zomato-api" || got.Tag != "a31f9c2" {
		t.Fatalf("got %+v", got)
	}
	if got.String() != "ghcr.io/yashdagar/zomato-api:a31f9c2" {
		t.Fatalf("String() = %q", got.String())
	}
}

func TestW7O3_ParseImageRejectsRefsThatCannotBeRolledBackTo(t *testing.T) {
	for _, ref := range []string{
		"ghcr.io/yashdagar/zomato-api",
		"ghcr.io/yashdagar/zomato-api:latest",
		"ghcr.io/yashdagar/zomato-api:v1",
		"ghcr.io/yashdagar/zomato-api:A31F9C2",
		"ghcr.io/yashdagar/zomato-api:a31f9c",
		"docker.io/yashdagar/zomato-api:a31f9c2",
		"ghcr.io/zomato-api:a31f9c2",
		"",
	} {
		if _, err := release.ParseImage(ref); err == nil {
			t.Errorf("ParseImage(%q) succeeded, want an error", ref)
		}
	}
}

func TestW7O3_ParseImageAcceptsFullLengthShaTags(t *testing.T) {
	sha := strings.Repeat("ab12", 10)
	if _, err := release.ParseImage("ghcr.io/yashdagar/zomato-api:" + sha); err != nil {
		t.Fatalf("a 40 character sha was rejected: %v", err)
	}
	if _, err := release.ParseImage("ghcr.io/yashdagar/zomato-api:" + sha + "0"); err == nil {
		t.Fatal("a 41 character tag was accepted")
	}
}

func TestW7O3_PinRewritesOnlyTheNamedServicesImageLine(t *testing.T) {
	got, err := release.Pin(compose, "api", "b42e8d1")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(compose, "zomato-api:a31f9c2", "zomato-api:b42e8d1", 1)
	if got != want {
		t.Fatalf("Pin changed more than one line:\n%s", got)
	}
}

func TestW7O3_PinReplacesAnInterpolatedTag(t *testing.T) {
	got, err := release.Pin(compose, "worker", "c53f9a2")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(compose, "${WORKER_TAG:-local}", "c53f9a2", 1)
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestW7O3_PinDoesNotConfuseServicesWithACommonPrefix(t *testing.T) {
	got, err := release.Pin(compose, "api-docs", "1111111")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "zomato-api:a31f9c2") || !strings.Contains(got, "zomato-api-docs:1111111") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestW7O3_PinRejectsUnknownServicesAndImagesOutsideOurRegistry(t *testing.T) {
	if _, err := release.Pin(compose, "caddy", "b42e8d1"); err == nil {
		t.Error("Pin of a missing service succeeded")
	}
	if _, err := release.Pin(compose, "mysql", "b42e8d1"); err == nil {
		t.Error("Pin of a mysql image succeeded, only ghcr.io/yashdagar images may be pinned")
	}
}

func TestW7O3_PinRejectsTagsThatAreNotCommitShas(t *testing.T) {
	for _, tag := range []string{"latest", "v2", "B42E8D1", "b42e8d", ""} {
		if _, err := release.Pin(compose, "api", tag); err == nil {
			t.Errorf("Pin accepted tag %q", tag)
		}
	}
}

func TestW7O3_RollbackReturnsThePreviousDeployedTag(t *testing.T) {
	history := []string{"a31f9c2", "b42e8d1", "c53f9a2"}
	for current, want := range map[string]string{"c53f9a2": "b42e8d1", "b42e8d1": "a31f9c2"} {
		got, err := release.Rollback(history, current)
		if err != nil || got != want {
			t.Errorf("Rollback(%s) = %q, %v, want %q", current, got, err, want)
		}
	}
}

func TestW7O3_RollbackSkipsRedeploysOfTheSameTag(t *testing.T) {
	got, err := release.Rollback([]string{"a31f9c2", "b42e8d1", "b42e8d1", "c53f9a2"}, "c53f9a2")
	if err != nil || got != "b42e8d1" {
		t.Fatalf("Rollback = %q, %v, want b42e8d1", got, err)
	}
	got, err = release.Rollback([]string{"a31f9c2", "b42e8d1", "b42e8d1"}, "b42e8d1")
	if err != nil || got != "a31f9c2" {
		t.Fatalf("Rollback after a redeploy = %q, %v, want a31f9c2", got, err)
	}
}

func TestW7O3_RollbackFailsWithoutAPreviousRelease(t *testing.T) {
	if _, err := release.Rollback([]string{"a31f9c2"}, "a31f9c2"); err == nil {
		t.Error("rolling back the first release succeeded")
	}
	if _, err := release.Rollback([]string{"a31f9c2", "b42e8d1"}, "ffffff0"); err == nil {
		t.Error("rolling back an unknown current tag succeeded")
	}
	if _, err := release.Rollback(nil, "a31f9c2"); err == nil {
		t.Error("rolling back with an empty history succeeded")
	}
}

func TestW7O3_RollbackDoesNotMutateTheHistory(t *testing.T) {
	history := []string{"a31f9c2", "b42e8d1", "c53f9a2"}
	if _, err := release.Rollback(history, "c53f9a2"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(history, ",") != "a31f9c2,b42e8d1,c53f9a2" {
		t.Fatalf("history changed: %v", history)
	}
}
