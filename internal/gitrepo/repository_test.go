package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_DIR=") && !strings.HasPrefix(entry, "GIT_WORK_TREE=") && !strings.HasPrefix(entry, "GIT_INDEX_FILE=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func fixture(t *testing.T) (*Repository, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.name", "Test User")
	git(t, dir, "config", "user.email", "test@example.test")
	git(t, dir, "config", "commit.gpgsign", "false")
	repo, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo, dir
}

func write(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInitialCommitWorkflow(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "你好 world.txt", "first line\n")
	state, err := repo.Snapshot(ctx)
	if err != nil || state.Branch != "main" || !state.Unborn || len(state.Files) != 1 || state.Files[0].Path != "你好 world.txt" {
		t.Fatalf("initial state: %+v, %v", state, err)
	}
	diff, err := repo.Diff(ctx, "你好 world.txt", false)
	if err != nil || !strings.Contains(diff.Text, "+first line") {
		t.Fatalf("untracked diff: %+v, %v", diff, err)
	}
	if err := repo.Stage(ctx, []string{"你好 world.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Unstage(ctx, []string{"你好 world.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "你好 world.txt")); err != nil {
		t.Fatal("unstaging removed working file:", err)
	}
	if err := repo.Stage(ctx, []string{"你好 world.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "首次提交\n\n正文"); err != nil {
		t.Fatal(err)
	}
	state, err = repo.Snapshot(ctx)
	if err != nil || state.Unborn || len(state.Files) != 0 {
		t.Fatalf("after commit: %+v, %v", state, err)
	}
	commits, err := repo.History(ctx, 0)
	if err != nil || len(commits) != 1 || commits[0].Subject != "首次提交" || commits[0].Author != "Test User" {
		t.Fatalf("history: %+v, %v", commits, err)
	}
	detail, err := repo.CommitDiff(ctx, commits[0].Hash)
	if err != nil || !strings.Contains(detail.Text, "+first line") {
		t.Fatalf("root commit diff: %+v, %v", detail, err)
	}
}

func TestStageUsesLiteralPaths(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	names := []string{":(glob)*", "-leading.txt", "line\nbreak.txt", "tabs\tfile.txt", "spaces file.txt"}
	for _, name := range names {
		write(t, dir, name, name+"\n")
	}
	if err := repo.Stage(ctx, []string{":(glob)*"}); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	staged := 0
	for _, file := range state.Files {
		if file.Index != "?" && file.Index != " " {
			staged++
			if file.Path != ":(glob)*" {
				t.Fatal("wildcard staged another file:", file.Path)
			}
		}
	}
	if staged != 1 {
		t.Fatalf("expected one staged path: %+v", state.Files)
	}
	if err := repo.Stage(ctx, names); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "special paths"); err != nil {
		t.Fatal(err)
	}
}

func TestStagedAndWorkingDiffAreSeparate(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file.txt", "staged\n")
	if err := repo.Stage(ctx, []string{"file.txt"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "file.txt", "working\n")
	staged, err := repo.Diff(ctx, "file.txt", true)
	if err != nil || !strings.Contains(staged.Text, "+staged") || strings.Contains(staged.Text, "+working") {
		t.Fatalf("staged diff: %+v %v", staged, err)
	}
	working, err := repo.Diff(ctx, "file.txt", false)
	if err != nil || !strings.Contains(working.Text, "+working") {
		t.Fatalf("working diff: %+v %v", working, err)
	}
	if err := repo.Commit(ctx, "only index"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", "HEAD:file.txt"); got != "staged" {
		t.Fatalf("commit included unstaged content: %q", got)
	}
}

func TestUnstageRenameRestoresBothIndexPaths(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "old.txt", "content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "mv", "old.txt", "new.txt")
	state, err := repo.Snapshot(ctx)
	if err != nil || len(state.Files) != 1 || state.Files[0].OriginalPath != "old.txt" {
		t.Fatalf("rename state: %+v %v", state, err)
	}
	if err := repo.Unstage(ctx, []string{"new.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("rename remained staged: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestStageModifiedRenamedFile(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "old.txt", "original\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "mv", "old.txt", "new.txt")
	write(t, dir, "new.txt", "updated after rename\n")
	if err := repo.Stage(ctx, []string{"new.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", ":new.txt"); got != "updated after rename" {
		t.Fatalf("renamed content was not staged: %q", got)
	}
	if got := git(t, dir, "diff", "--name-only"); got != "" {
		t.Fatalf("unstaged content remains: %q", got)
	}
}

func TestUnsafePathsAndOptionsAreRejected(t *testing.T) {
	repo, _ := fixture(t)
	ctx := context.Background()
	for _, path := range []string{"../escape", "/tmp/escape", ".git/config", "a/../../escape", ""} {
		if err := repo.Stage(ctx, []string{path}); err == nil {
			t.Errorf("accepted path %q", path)
		}
		if _, err := repo.Diff(ctx, path, false); err == nil {
			t.Errorf("accepted diff path %q", path)
		}
	}
	if err := repo.SwitchBranch(ctx, "--force"); err == nil {
		t.Fatal("accepted switch option")
	}
	if _, err := repo.CommitDiff(ctx, "--output=/tmp/unwanted"); err == nil {
		t.Fatal("accepted commit option")
	}
	if err := repo.Commit(ctx, "   "); err == nil {
		t.Fatal("accepted empty commit message")
	}
}

func TestBranchesAndDetachedHead(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	if err := repo.CreateBranch(ctx, "feature/test"); err != nil {
		t.Fatal(err)
	}
	branches, err := repo.Branches(ctx)
	if err != nil || len(branches) != 2 {
		t.Fatalf("branches: %+v %v", branches, err)
	}
	if err := repo.SwitchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "checkout", "--detach")
	state, err := repo.Snapshot(ctx)
	if err != nil || !state.Detached || state.Head == "" || state.Unborn {
		t.Fatalf("detached state: %+v %v", state, err)
	}
	write(t, dir, "detached.txt", "detached content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "detached commit")
	history, err := repo.History(ctx, 0)
	if err != nil || len(history) == 0 || history[0].Subject != "detached commit" {
		t.Fatalf("detached commit missing from history: %+v %v", history, err)
	}
}

func TestHistoryPastLastPageIsEmpty(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "commit", "--allow-empty", "-m", "only commit")
	commits, err := repo.History(context.Background(), 100)
	if err != nil || len(commits) != 0 {
		t.Fatalf("expected an empty final page: %+v %v", commits, err)
	}
}

func TestOpenSubdirectoryAndRejectBareRepository(t *testing.T) {
	_, dir := fixture(t)
	subdir := filepath.Join(dir, "nested")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(context.Background(), subdir)
	canonical, _ := filepath.EvalSymlinks(dir)
	if err != nil || repo.Path != canonical {
		t.Fatalf("open nested: %+v %v", repo, err)
	}
	bare := t.TempDir()
	git(t, bare, "init", "--bare")
	if _, err := Open(context.Background(), bare); err == nil {
		t.Fatal("accepted bare repository")
	}
}

func TestBinaryAndSymlinkPreview(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "image.bin", "binary\x00content")
	diff, err := repo.Diff(ctx, "image.bin", false)
	if err != nil || !diff.Binary {
		t.Fatalf("binary preview: %+v %v", diff, err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("do not reveal"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	diff, err = repo.Diff(ctx, "link", false)
	if err != nil || strings.Contains(diff.Text, "do not reveal") || !strings.Contains(diff.Text, secret) {
		t.Fatalf("symlink preview: %+v %v", diff, err)
	}
}

func TestLocalRemoteRoundTrip(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "remote", "add", "origin", remote)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "push", "-u", "origin", "main")
	write(t, dir, "file.txt", "next\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "next")
	state, err := repo.Snapshot(ctx)
	if err != nil || state.Ahead != 1 || state.Upstream != "origin/main" {
		t.Fatalf("tracking state: %+v %v", state, err)
	}
	if err := repo.Remote(ctx, "push"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remote(ctx, "fetch"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remote(ctx, "pull"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remote(ctx, "--force"); err == nil {
		t.Fatal("accepted unsupported remote action")
	}
	if got := git(t, remote, "log", "-1", "--format=%s"); got != "next" {
		t.Fatal("push did not update remote:", got)
	}
}

func TestInheritedGitEnvironmentCannotRedirectRepository(t *testing.T) {
	_, dir := fixture(t)
	_, other := fixture(t)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	repo, err := Open(context.Background(), dir)
	canonical, _ := filepath.EvalSymlinks(dir)
	if err != nil || repo.Path != canonical {
		t.Fatalf("inherited environment redirected repository: %+v %v", repo, err)
	}
}

func TestInheritedIndexCannotRedirectStaging(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "new file\n")
	alternate := filepath.Join(t.TempDir(), "alternate-index")
	t.Setenv("GIT_INDEX_FILE", alternate)
	if err := repo.Stage(context.Background(), []string{"file.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "diff", "--cached", "--name-only"); got != "file.txt" {
		t.Fatalf("staging did not update repository index: %q", got)
	}
	if _, err := os.Stat(alternate); !os.IsNotExist(err) {
		t.Fatal("staging wrote an inherited alternate index")
	}
}

func TestDiffDisablesExternalCommands(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "config", "diff.external", "this-program-must-not-run")
	write(t, dir, "file.txt", "changed\n")
	diff, err := repo.Diff(context.Background(), "file.txt", false)
	if err != nil || !strings.Contains(diff.Text, "+changed") {
		t.Fatalf("external diff was used: %+v %v", diff, err)
	}
}

func TestLargeUntrackedPreviewIsBounded(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "large.txt", strings.Repeat("line\n", 60000))
	diff, err := repo.Diff(context.Background(), "large.txt", false)
	if err != nil || !diff.Truncated || len(diff.Text) > 400000 {
		t.Fatalf("unbounded preview: length %d truncated %v err %v", len(diff.Text), diff.Truncated, err)
	}
}

func TestUnbornUnstageKeepsNewerWorkingContent(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "staged version\n")
	if err := repo.Stage(ctx, []string{"file.txt"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "file.txt", "newer working version\n")
	if err := repo.Unstage(ctx, []string{"file.txt"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil || string(content) != "newer working version\n" {
		t.Fatalf("working content changed: %q %v", content, err)
	}
	if got := git(t, dir, "ls-files"); got != "" {
		t.Fatalf("index not cleared: %q", got)
	}
}

func TestPushOnlyUpdatesCurrentUpstream(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "remote", "add", "origin", remote)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "branch", "other")
	git(t, dir, "push", "-u", "origin", "main")
	git(t, dir, "push", "origin", "other")
	original := git(t, remote, "rev-parse", "refs/heads/other")
	git(t, dir, "switch", "other")
	write(t, dir, "file.txt", "other branch\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "other branch")
	git(t, dir, "switch", "main")
	write(t, dir, "file.txt", "main branch\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "main branch")
	git(t, dir, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	if err := repo.Remote(ctx, "push"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, remote, "rev-parse", "refs/heads/other"); got != original {
		t.Fatal("push unexpectedly updated another branch")
	}
	if got := git(t, remote, "log", "-1", "--format=%s", "main"); got != "main branch" {
		t.Fatal("upstream was not updated:", got)
	}
	git(t, dir, "config", "remote.origin.mirror", "true")
	if err := repo.Remote(ctx, "push"); err == nil {
		t.Fatal("accepted mirror push configuration")
	}
}

func TestPushRejectsNonFastForwardDespiteForceConfiguration(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "remote", "add", "origin", remote)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "push", "-u", "origin", "main")
	write(t, dir, "file.txt", "remote history\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "remote history")
	git(t, dir, "push")
	remoteHead := git(t, remote, "rev-parse", "main")
	git(t, dir, "commit", "--amend", "-m", "rewritten local history")
	git(t, dir, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	if err := repo.Remote(ctx, "push"); err == nil {
		t.Fatal("non-fast-forward push unexpectedly succeeded")
	}
	if got := git(t, remote, "rev-parse", "main"); got != remoteHead {
		t.Fatal("remote history was rewritten")
	}
}
