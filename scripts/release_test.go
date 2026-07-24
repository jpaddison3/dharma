package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The release guard decides whether the artifact colleagues install can be
// published under this commit's tag. It was hand-verified twice and wrong both
// times (first it only looked at the local clone, which never has the tag;
// then it only matched annotated tags, missing the lightweight ones git and gh
// create by default), so it gets fixtures.
func TestCheckTagAgainstRemote(t *testing.T) {
	for _, tc := range []struct {
		name string
		// tag: "", "light", or "annot"; at: "head" or "old"
		tag, at   string
		wantBlock bool
	}{
		{name: "no tag anywhere", tag: ""},
		{name: "lightweight tag at HEAD", tag: "light", at: "head"},
		{name: "annotated tag at HEAD", tag: "annot", at: "head"},
		{name: "lightweight tag elsewhere", tag: "light", at: "old", wantBlock: true},
		{name: "annotated tag elsewhere", tag: "annot", at: "old", wantBlock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, head := newRepoWithRemoteTag(t, tc.tag, tc.at)
			out, err := runReleaseTagGuard(t, repo, head)
			if blocked := err != nil; blocked != tc.wantBlock {
				t.Fatalf("blocked = %v, want %v\n%s", blocked, tc.wantBlock, out)
			}
			if tc.wantBlock && !strings.Contains(out, "already exists") {
				t.Errorf("block message should name the stale tag:\n%s", out)
			}
		})
	}
}

// An unreachable origin must stop the release rather than read as "no tag":
// this guard exists to keep a build from being published under someone else's
// commit, so it has to fail closed.
func TestCheckTagFailsClosedOnUnreachableRemote(t *testing.T) {
	repo, head := newRepoWithRemoteTag(t, "", "")
	git(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "does-not-exist.git"))
	if out, err := runReleaseTagGuard(t, repo, head); err == nil {
		t.Errorf("release proceeded with an unreachable origin:\n%s", out)
	}
}

// runReleaseTagGuard extracts the guard from release.sh and runs it alone.
func runReleaseTagGuard(t *testing.T, repo, head string) (string, error) {
	t.Helper()
	release := absPath(t, "release.sh")
	guard, err := os.ReadFile(release)
	if err != nil {
		t.Fatal(err)
	}
	src := string(guard)
	start := strings.Index(src, "check_tag() {")
	end := strings.Index(src, `check_tag "$(printf`)
	if start < 0 || end < 0 {
		t.Fatal("release.sh no longer has the check_tag block this test extracts — update the extraction")
	}
	// Take the guard through its last check_tag call.
	end = strings.Index(src[end:], "\n") + end
	script := "set -euo pipefail\nVERSION=1.0.0\nREPO_ROOT=" + repo + "\nCOMMIT=" + head + "\n" + src[start:end]

	cmd := exec.Command("bash", "-c", script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// newRepoWithRemoteTag builds a repo whose origin carries the given tag kind
// (none / lightweight / annotated) at HEAD or at an older commit, with the tag
// deleted locally — the state of a clone that never fetched it.
func newRepoWithRemoteTag(t *testing.T, kind, at string) (repo, head string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "-q", "--bare", remote)
	repo = t.TempDir()
	git(t, "", "init", "-q", "-b", "main", repo)
	git(t, repo, "config", "user.email", "t@example.com")
	git(t, repo, "config", "user.name", "t")
	git(t, repo, "remote", "add", "origin", remote)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "one")
	old := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "two")
	head = strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))

	target := head
	if at == "old" {
		target = old
	}
	switch kind {
	case "light":
		git(t, repo, "tag", "v1.0.0", target)
	case "annot":
		git(t, repo, "tag", "-a", "v1.0.0", "-m", "x", target)
	}
	git(t, repo, "push", "-q", "origin", "main", "--tags")
	if kind != "" {
		git(t, repo, "tag", "-d", "v1.0.0")
	}
	return repo, head
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func absPath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
