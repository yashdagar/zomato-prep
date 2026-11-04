package deploy_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const stubDump = `#!/bin/sh
echo "$@" >> "$STUB_LOG"
if [ -n "$STUB_FAIL" ]; then echo "mysqldump: connection refused" >&2; exit 2; fi
echo "-- dump of $*"
echo "INSERT INTO orders VALUES (1);"
`

const stubMysql = `#!/bin/sh
echo "ARGS $*" >> "$STUB_LOG"
if [ ! -t 0 ]; then sed 's/^/STDIN /' >> "$STUB_LOG"; fi
case "$*" in
  *"COUNT(*)"*) echo "${STUB_COUNT:-5}" ;;
esac
exit 0
`

type rig struct {
	dir, bin, backups, log string
}

func newRig(t *testing.T) rig {
	t.Helper()
	dir := t.TempDir()
	r := rig{dir: dir, bin: filepath.Join(dir, "bin"), backups: filepath.Join(dir, "backups"), log: filepath.Join(dir, "stub.log")}
	for _, d := range []string{r.bin, r.backups} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"mysqldump": stubDump, "mysql": stubMysql} {
		if err := os.WriteFile(filepath.Join(r.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r rig) run(t *testing.T, script string, env ...string) (string, error) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(backendDir, "scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scripts/%s: %v", script, err)
	}
	cmd := exec.Command("sh", path)
	cmd.Env = append(os.Environ(),
		"PATH="+r.bin+":"+os.Getenv("PATH"),
		"BACKUP_DIR="+r.backups,
		"STUB_LOG="+r.log,
		"MYSQL_ROOT_PASSWORD=test-password",
		"MYSQL_HOST=mysql",
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r rig) dumps(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(r.backups, "zomato-*.sql.gz"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	sort.Strings(names)
	return names
}

func (r rig) logText(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(r.log)
	return string(b)
}

func gunzip(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s is not gzip: %v", path, err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestW7B4_BackupWritesATimestampedGzipOfTheDump(t *testing.T) {
	r := newRig(t)
	if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP=20261104T020000Z"); err != nil {
		t.Fatalf("backup.sh failed: %v\n%s", err, out)
	}
	got := r.dumps(t)
	if len(got) != 1 || got[0] != "zomato-20261104T020000Z.sql.gz" {
		t.Fatalf("backup files = %v, want [zomato-20261104T020000Z.sql.gz]", got)
	}
	if body := gunzip(t, filepath.Join(r.backups, got[0])); !strings.Contains(body, "INSERT INTO orders") {
		t.Fatalf("dump content = %q, want the mysqldump output", body)
	}
}

func TestW7B4_BackupTakesAConsistentSnapshotOfTheZomatoDatabase(t *testing.T) {
	r := newRig(t)
	if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP=20261104T020000Z"); err != nil {
		t.Fatalf("backup.sh failed: %v\n%s", err, out)
	}
	log := r.logText(t)
	for _, want := range []string{"--single-transaction", "--routines", "--triggers", "zomato"} {
		if !strings.Contains(log, want) {
			t.Errorf("mysqldump was called without %s: %s", want, log)
		}
	}
	if strings.Contains(log, "test-password") {
		t.Error("the password must be passed through MYSQL_PWD, not on the mysqldump command line")
	}
}

func TestW7B4_BackupPointsLatestAtTheNewestDump(t *testing.T) {
	r := newRig(t)
	for _, ts := range []string{"20261103T020000Z", "20261104T020000Z"} {
		if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP="+ts); err != nil {
			t.Fatalf("backup.sh failed: %v\n%s", err, out)
		}
	}
	target, err := os.Readlink(filepath.Join(r.backups, "latest.sql.gz"))
	if err != nil {
		t.Fatalf("latest.sql.gz is not a symlink: %v", err)
	}
	if filepath.Base(target) != "zomato-20261104T020000Z.sql.gz" {
		t.Fatalf("latest.sql.gz -> %s, want the newest dump", target)
	}
}

func TestW7B4_BackupKeepsOnlyTheNewestDumps(t *testing.T) {
	r := newRig(t)
	for _, ts := range []string{"20261101T020000Z", "20261102T020000Z", "20261103T020000Z", "20261104T020000Z", "20261105T020000Z"} {
		if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP="+ts, "KEEP=3"); err != nil {
			t.Fatalf("backup.sh failed: %v\n%s", err, out)
		}
	}
	got := strings.Join(r.dumps(t), " ")
	want := "zomato-20261103T020000Z.sql.gz zomato-20261104T020000Z.sql.gz zomato-20261105T020000Z.sql.gz"
	if got != want {
		t.Fatalf("kept = %s, want %s", got, want)
	}
}

func TestW7B4_BackupNeverDeletesFilesItDidNotCreate(t *testing.T) {
	r := newRig(t)
	keep := filepath.Join(r.backups, "notes.txt")
	if err := os.WriteFile(keep, []byte("do not delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ts := range []string{"20261101T020000Z", "20261102T020000Z", "20261103T020000Z"} {
		if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP="+ts, "KEEP=1"); err != nil {
			t.Fatalf("backup.sh failed: %v\n%s", err, out)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("backup.sh deleted a file that is not a dump")
	}
}

func TestW7B4_FailedDumpExitsNonZeroAndLeavesNoPartialFile(t *testing.T) {
	r := newRig(t)
	if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP=20261103T020000Z"); err != nil {
		t.Fatalf("first backup failed: %v\n%s", err, out)
	}
	if _, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP=20261104T020000Z", "STUB_FAIL=1"); err == nil {
		t.Fatal("backup.sh exited 0 although mysqldump failed")
	}
	got := r.dumps(t)
	if len(got) != 1 || got[0] != "zomato-20261103T020000Z.sql.gz" {
		t.Fatalf("backup files after a failed dump = %v, want only the earlier good dump", got)
	}
	target, _ := os.Readlink(filepath.Join(r.backups, "latest.sql.gz"))
	if filepath.Base(target) != "zomato-20261103T020000Z.sql.gz" {
		t.Fatalf("latest.sql.gz -> %q, a failed run must not move it", target)
	}
}

func prepareLatest(t *testing.T, r rig) {
	t.Helper()
	if out, err := r.run(t, "backup.sh", "BACKUP_TIMESTAMP=20261104T020000Z"); err != nil {
		t.Fatalf("backup.sh failed: %v\n%s", err, out)
	}
	if err := os.WriteFile(r.log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestW7B4_RestoreTestLoadsTheDumpIntoAFreshDatabase(t *testing.T) {
	r := newRig(t)
	prepareLatest(t, r)
	if out, err := r.run(t, "restore-test.sh"); err != nil {
		t.Fatalf("restore-test.sh failed: %v\n%s", err, out)
	}
	log := r.logText(t)
	if !strings.Contains(log, "CREATE DATABASE") || !strings.Contains(log, "restore_test") {
		t.Errorf("restore-test.sh must CREATE DATABASE restore_test: %s", log)
	}
	if !strings.Contains(log, "STDIN INSERT INTO orders") {
		t.Errorf("the dump was never piped into mysql: %s", log)
	}
	if !strings.Contains(log, "COUNT(*)") {
		t.Errorf("restore-test.sh must run a sanity query such as SELECT COUNT(*) FROM orders: %s", log)
	}
}

func TestW7B4_RestoreTestNeverTouchesTheLiveDatabase(t *testing.T) {
	r := newRig(t)
	prepareLatest(t, r)
	if out, err := r.run(t, "restore-test.sh"); err != nil {
		t.Fatalf("restore-test.sh failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(r.logText(t), "\n") {
		if strings.HasPrefix(line, "ARGS") && strings.Contains(line, "zomato") && !strings.Contains(line, "restore_test") {
			t.Errorf("restore-test.sh addressed the live database: %s", line)
		}
		if strings.Contains(line, "DROP DATABASE") && !strings.Contains(line, "restore_test") {
			t.Errorf("restore-test.sh drops something other than restore_test: %s", line)
		}
	}
}

func TestW7B4_RestoreTestCleansUpAfterItself(t *testing.T) {
	r := newRig(t)
	prepareLatest(t, r)
	if out, err := r.run(t, "restore-test.sh"); err != nil {
		t.Fatalf("restore-test.sh failed: %v\n%s", err, out)
	}
	if !strings.Contains(r.logText(t), "DROP DATABASE") {
		t.Error("restore-test.sh should drop restore_test when it is done")
	}
}

func TestW7B4_RestoreTestFailsWhenTheRestoredTableIsEmpty(t *testing.T) {
	r := newRig(t)
	prepareLatest(t, r)
	if _, err := r.run(t, "restore-test.sh", "STUB_COUNT=0"); err == nil {
		t.Fatal("restore-test.sh exited 0 although the restored orders table is empty")
	}
}

func TestW7B4_RestoreTestFailsWithoutAnyBackup(t *testing.T) {
	r := newRig(t)
	if _, err := r.run(t, "restore-test.sh"); err == nil {
		t.Fatal("restore-test.sh exited 0 although there is no latest.sql.gz")
	}
}

func TestW7B4_ComposeRunsTheBackupOnAScheduleWithItsOwnVolume(t *testing.T) {
	b := composeService(t, "backup")
	if r := asString(b["restart"]); r != "unless-stopped" && r != "always" {
		t.Errorf("backup restart = %q, want unless-stopped", r)
	}
	if c := dependsOn(b)["mysql"]; c != "service_healthy" {
		t.Errorf("backup depends_on mysql = %q, want service_healthy", c)
	}
	var data, scripts bool
	for _, v := range asList(b["volumes"]) {
		s := toString(v)
		if strings.HasPrefix(s, "backups:") && strings.Contains(s, "/backups") {
			data = true
		}
		if strings.Contains(s, "scripts") {
			scripts = true
		}
	}
	if !data {
		t.Error("backup must write to the named volume backups mounted at /backups")
	}
	if !scripts {
		t.Error("backup must mount ./scripts so it can run backup.sh")
	}
	_, cmdIsList := b["command"].([]any)
	if !cmdIsList && len(asList(b["entrypoint"])) < 3 {
		t.Error("backup: a multi-line shell script must be one list item (command: - | ...) or part of an entrypoint list, a plain string is split into words by Compose")
	}
	cmd := toString(b["command"]) + toString(b["entrypoint"])
	if !strings.Contains(cmd, "backup.sh") || !strings.Contains(cmd, "sleep") {
		t.Errorf("backup command = %q, want a loop that sleeps and runs backup.sh", cmd)
	}
	if _, ok := asMap(parseYAML(t, readFile(t, "compose.prod.yml"))["volumes"])["backups"]; !ok {
		t.Error("declare the backups volume under top-level volumes")
	}
}
