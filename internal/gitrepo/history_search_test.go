// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSearchHistoryBeyondLoadedPage(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "tracked.txt", "old content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--author=Archive Person <archive@example.test>", "-m", "old [literal].* subject\n\nHidden BODY needle")
	oldest := git(t, dir, "rev-parse", "HEAD")
	blob := git(t, dir, "rev-parse", "HEAD:tracked.txt")
	var history strings.Builder
	for index := 1; index <= 310; index++ {
		message := fmt.Sprintf("recent %03d", index)
		fmt.Fprintf(&history, "commit refs/heads/main\nmark :%d\ncommitter Test User <test@example.test> %d +0000\ndata %d\n%s\n",
			index, 1800000000+index, len(message), message)
		if index == 1 {
			fmt.Fprintf(&history, "from %s\n", oldest)
		} else {
			fmt.Fprintf(&history, "from :%d\n", index-1)
		}
		fmt.Fprintf(&history, "deleteall\nM 100644 %s tracked.txt\n\n", blob)
	}
	command := exec.Command("git", "-C", dir, "fast-import", "--quiet")
	command.Env = append(gitEnvironment(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	command.Stdin = strings.NewReader(history.String())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("import history: %v\n%s", err, output)
	}
	loaded, err := repo.History(ctx, 0)
	if err != nil || len(loaded) != 100 {
		t.Fatalf("loaded history: %d, %v", len(loaded), err)
	}
	for _, commit := range loaded {
		if commit.Hash == oldest {
			t.Fatal("test commit unexpectedly present in loaded history")
		}
	}
	location, err := repo.LocateHistory(ctx, oldest, "", "", HistoryFilter{})
	if err != nil || location.Commit.Hash != oldest || location.Offset != 300 ||
		len(location.Commits) != 11 || location.Commits[10].Hash != oldest {
		t.Fatalf("定位旧提交未返回目标所在页: %+v, %v", location, err)
	}
	location, err = repo.LocateHistory(ctx, loaded[5].Hash, "", "", HistoryFilter{})
	if err != nil || location.Offset != 0 || len(location.Commits) != 100 ||
		location.Commits[5].Hash != loaded[5].Hash {
		t.Fatalf("定位首屏提交异常: %+v, %v", location, err)
	}
	location, err = repo.LocateHistory(ctx, oldest, "recent", "", HistoryFilter{})
	if err != nil || len(location.Commits) != 0 || location.Commit.Subject != "old [literal].* subject" {
		t.Fatalf("筛选排除目标时未保留详情: %+v, %v", location, err)
	}
	location, err = repo.LocateHistory(ctx, oldest, "hidden BODY", "tracked.txt", HistoryFilter{})
	if err != nil || len(location.Commits) != 1 || location.Offset != 0 || location.Commits[0].Hash != oldest {
		t.Fatalf("定位未遵循正文与路径筛选: %+v, %v", location, err)
	}
	calls := filepath.Join(t.TempDir(), "log-calls")
	t.Setenv("XSOURCE_LOG_CALLS", calls)
	installHistoryGitWrapper(t, `for argument do
  if [ "$argument" = log ]; then
    printf 'log\n' >> "$XSOURCE_LOG_CALLS"
    for option do
      case "$option" in
        --skip=*|-n|--max-count=*) printf 'repeated traversal is forbidden\n' >&2; exit 91 ;;
      esac
    done
  fi
done
exec "$XSOURCE_REAL_GIT" "$@"
`)
	for _, query := range []string{"[LITERAL].*", "hidden body NEEDLE", "ARCHIVE PERSON", "ARCHIVE@EXAMPLE.TEST", strings.ToUpper(oldest[:12])} {
		t.Run(query, func(t *testing.T) {
			before := historyLogCallCount(t, calls)
			commits, err := repo.SearchHistory(ctx, query, "", 0, HistoryFilter{})
			if err != nil || len(commits) != 1 || commits[0].Hash != oldest {
				t.Fatalf("search %q: %+v, %v", query, commits, err)
			}
			if count := historyLogCallCount(t, calls) - before; count != 1 {
				t.Fatalf("search started %d log processes, want one", count)
			}
		})
	}
	if commits, err := repo.SearchHistory(ctx, "old literalZZ", "", 0, HistoryFilter{}); err != nil || len(commits) != 0 {
		t.Fatalf("literal search: %+v, %v", commits, err)
	}
	first, err := repo.SearchHistory(ctx, "recent", "", 0, HistoryFilter{})
	if err != nil || len(first) != 100 {
		t.Fatalf("first matched page: %d, %v", len(first), err)
	}
	second, err := repo.SearchHistory(ctx, "recent", "", 100, HistoryFilter{})
	if err != nil || len(second) != 100 || first[0].Hash == second[0].Hash {
		t.Fatalf("second matched page: %d, %v", len(second), err)
	}
	last, err := repo.SearchHistory(ctx, "recent", "", 300, HistoryFilter{})
	if err != nil || len(last) != 10 {
		t.Fatalf("last matched page: %d, %v", len(last), err)
	}
	past, err := repo.SearchHistory(ctx, "recent", "", 310, HistoryFilter{})
	if err != nil || len(past) != 0 {
		t.Fatalf("past matched pages: %+v, %v", past, err)
	}
}

func TestSearchHistoryAcrossBranchesAndLiteralPaths(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	literal := ":(glob)*"
	write(t, dir, literal, "literal\n")
	write(t, dir, "other.txt", "other\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-b", "archive")
	write(t, dir, literal, "archive\n")
	git(t, dir, "--literal-pathspecs", "add", "--", literal)
	git(t, dir, "commit", "-m", "Branch NEEDLE")
	branch := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "main")
	write(t, dir, "other.txt", "main\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "unrelated")
	for _, query := range []string{"", "needle"} {
		commits, err := repo.SearchHistory(ctx, query, literal, 0, HistoryFilter{})
		if err != nil {
			t.Fatal(err)
		}
		expected := []string{branch}
		if query == "" {
			expected = append(expected, base)
		}
		var hashes []string
		for _, commit := range commits {
			hashes = append(hashes, commit.Hash)
		}
		if !reflect.DeepEqual(hashes, expected) {
			t.Fatalf("literal path %q, query %q: %v, want %v", literal, query, hashes, expected)
		}
	}
	for _, path := range []string{"-leading.txt", "line\nbreak.txt", "tabs\tfile.txt", "你好 file.txt", "wild[1]*.txt"} {
		write(t, dir, path, path+"\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "special path")
		expected := git(t, dir, "rev-parse", "HEAD")
		commits, err := repo.SearchHistory(ctx, "", path, 0, HistoryFilter{})
		if err != nil || len(commits) != 1 || commits[0].Hash != expected {
			t.Fatalf("path %q: %+v, %v", path, commits, err)
		}
	}
	for _, skip := range []int{0, 100} {
		history, err := repo.History(ctx, skip)
		if err != nil {
			t.Fatal(err)
		}
		search, err := repo.SearchHistory(ctx, "", "", skip, HistoryFilter{})
		if err != nil || !reflect.DeepEqual(history, search) {
			t.Fatalf("empty search differs from History: %+v, %v", search, err)
		}
	}
	git(t, dir, "checkout", "--orphan", "unborn")
	commits, err := repo.SearchHistory(ctx, "needle", "", 0, HistoryFilter{})
	if err != nil || len(commits) != 1 || commits[0].Hash != branch {
		t.Fatalf("other branches with unborn HEAD: %+v, %v", commits, err)
	}
}

func TestCommitFilesAndDiffRootAndRename(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	original := "old\tname\n.txt"
	renamed := ":(glob)*"
	write(t, dir, original, "first\nsecond\nthird\n")
	write(t, dir, "other.txt", "unrelated\n")
	write(t, dir, "binary.bin", "\x00binary\xff\n")
	write(t, dir, "-leading.txt", "leading\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "root")
	root := git(t, dir, "rev-parse", "HEAD")
	files, err := repo.CommitFiles(ctx, root)
	if err != nil || len(files) != 4 {
		t.Fatalf("root files: %+v, %v", files, err)
	}
	for _, file := range files {
		if file.Status != "A" || file.OriginalPath != "" {
			t.Fatalf("root status: %+v", file)
		}
	}
	diff, err := repo.CommitFileDiff(ctx, root, original)
	if err != nil || !strings.Contains(diff.Text, "+first") || strings.Contains(diff.Text, "+unrelated") {
		t.Fatalf("root file diff: %+v, %v", diff, err)
	}
	binary, err := repo.CommitFileDiff(ctx, root, "binary.bin")
	if err != nil || !binary.Binary || !strings.Contains(binary.Text, "Binary files ") {
		t.Fatalf("binary diff: %+v, %v", binary, err)
	}
	leading, err := repo.CommitFileDiff(ctx, root, "-leading.txt")
	if err != nil || !strings.Contains(leading.Text, "+leading") {
		t.Fatalf("leading dash path: %+v, %v", leading, err)
	}
	git(t, dir, "mv", "--", original, renamed)
	write(t, dir, "other.txt", "changed elsewhere\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "rename")
	hash := git(t, dir, "rev-parse", "HEAD")
	files, err = repo.CommitFiles(ctx, hash)
	if err != nil || len(files) != 2 {
		t.Fatalf("rename files: %+v, %v", files, err)
	}
	var found bool
	for _, file := range files {
		if file.Path == renamed {
			found = true
			if file.Status != "R" || file.OriginalPath != original {
				t.Fatalf("rename record: %+v", file)
			}
		}
	}
	if !found {
		t.Fatalf("missing renamed file: %+v", files)
	}
	for _, path := range []string{renamed, original} {
		diff, err := repo.CommitFileDiff(ctx, hash, path)
		if err != nil || !strings.Contains(diff.Text, "rename from ") || !strings.Contains(diff.Text, "rename to ") || strings.Contains(diff.Text, "changed elsewhere") {
			t.Fatalf("rename patch for %q: %+v, %v", path, diff, err)
		}
	}
	if _, err := repo.CommitFileDiff(ctx, hash, "binary.bin"); err == nil {
		t.Fatal("unchanged path accepted")
	}
	git(t, dir, "--literal-pathspecs", "rm", "--", renamed)
	git(t, dir, "commit", "-m", "delete renamed file")
	deleted := git(t, dir, "rev-parse", "HEAD")
	files, err = repo.CommitFiles(ctx, deleted)
	if err != nil || len(files) != 1 || files[0].Path != renamed || files[0].Status != "D" {
		t.Fatalf("deleted file: %+v, %v", files, err)
	}
	diff, err = repo.CommitFileDiff(ctx, deleted, renamed)
	if err != nil || !strings.Contains(diff.Text, "-first") {
		t.Fatalf("deleted file patch: %+v, %v", diff, err)
	}
}

func TestCommitFilesAndDiffUseFirstParent(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "base.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "side")
	write(t, dir, "side.txt", "side\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "side")
	git(t, dir, "checkout", "main")
	write(t, dir, "main.txt", "main\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "main")
	git(t, dir, "merge", "--no-ff", "side", "-m", "merge")
	hash := git(t, dir, "rev-parse", "HEAD")
	files, err := repo.CommitFiles(ctx, hash)
	if err != nil || len(files) != 1 || files[0].Path != "side.txt" || files[0].Status != "A" {
		t.Fatalf("merge first-parent files: %+v, %v", files, err)
	}
	diff, err := repo.CommitFileDiff(ctx, hash, "side.txt")
	if err != nil || !strings.Contains(diff.Text, "+side") || strings.Contains(diff.Text, "+main") {
		t.Fatalf("merge first-parent diff: %+v, %v", diff, err)
	}
	all, err := repo.CommitDiff(ctx, hash)
	if err != nil || !strings.Contains(all.Text, "diff --git a/side.txt b/side.txt") ||
		!strings.Contains(all.Text, "+side") || strings.Contains(all.Text, "+main") ||
		strings.Count(all.Text, "diff --git ") != 1 {
		t.Fatalf("merge first-parent full diff: %+v, %v", all, err)
	}
	git(t, dir, "commit", "--allow-empty", "-m", "empty")
	empty := git(t, dir, "rev-parse", "HEAD")
	files, err = repo.CommitFiles(ctx, empty)
	if err != nil || files == nil || len(files) != 0 {
		t.Fatalf("empty commit files: %+v, %v", files, err)
	}
}

func TestHistorySearchRejectsInvalidParameters(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	for _, skip := range []int{-1, int(1 << 31)} {
		if _, err := repo.SearchHistory(ctx, "", "", skip, HistoryFilter{}); err == nil {
			t.Fatalf("accepted invalid skip: %d", skip)
		}
	}
	if _, err := repo.SearchHistory(ctx, "bad\x00query", "", 0, HistoryFilter{}); err == nil {
		t.Fatal("accepted NUL in query")
	}
	for _, path := range []string{"", "../escape", "/tmp/escape", ".git/config", "a/../../escape", "bad\x00path"} {
		if path != "" {
			if _, err := repo.SearchHistory(ctx, "", path, 0, HistoryFilter{}); err == nil {
				t.Fatalf("accepted invalid search path: %q", path)
			}
		}
		if _, err := repo.CommitFileDiff(ctx, strings.Repeat("a", 40), path); err == nil {
			t.Fatalf("accepted invalid diff path: %q", path)
		}
	}
	for _, hash := range []string{"", "HEAD", "abc1234", "--output=unwanted", strings.Repeat("g", 40), strings.Repeat("a", 39), strings.Repeat("a", 40), strings.Repeat("a", 64)} {
		if _, err := repo.CommitFiles(ctx, hash); err == nil {
			t.Fatalf("accepted invalid or missing commit: %q", hash)
		}
		if _, err := repo.CommitFileDiff(ctx, hash, "file.txt"); err == nil {
			t.Fatalf("accepted invalid diff commit: %q", hash)
		}
	}
	write(t, dir, "file.txt", "blob\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	for _, object := range []string{"HEAD:file.txt", "HEAD^{tree}"} {
		hash := git(t, dir, "rev-parse", object)
		if _, err := repo.CommitFiles(ctx, hash); err == nil {
			t.Fatalf("accepted non-commit object: %s", object)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "unwanted")); !os.IsNotExist(err) {
		t.Fatalf("option injection created output: %v", err)
	}
}

func TestHistorySearchEmptyRepositoryAndCancellation(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	for _, parameters := range []struct{ query, path string }{{}, {query: "missing"}, {path: "missing.txt"}} {
		commits, err := repo.SearchHistory(ctx, parameters.query, parameters.path, 0, HistoryFilter{})
		if err != nil || commits == nil || len(commits) != 0 {
			t.Fatalf("empty search: %+v, %v", commits, err)
		}
	}
	write(t, dir, "file.txt", "content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	expired, expire := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer expire()
	for _, check := range []struct {
		name string
		ctx  context.Context
		want error
	}{{"canceled", canceled, context.Canceled}, {"expired", expired, context.DeadlineExceeded}} {
		t.Run(check.name, func(t *testing.T) {
			if _, err := repo.SearchHistory(check.ctx, "base", "", 0, HistoryFilter{}); !errors.Is(err, check.want) {
				t.Fatalf("search context: %v", err)
			}
			if _, err := repo.CommitFiles(check.ctx, hash); !errors.Is(err, check.want) {
				t.Fatalf("files context: %v", err)
			}
			if _, err := repo.CommitFileDiff(check.ctx, hash, "file.txt"); !errors.Is(err, check.want) {
				t.Fatalf("diff context: %v", err)
			}
		})
	}
}

func TestHistorySearchCancellationWhileRepositoryBusy(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	for _, operation := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"empty search", func(ctx context.Context) error {
			_, err := repo.SearchHistory(ctx, "", "", 0, HistoryFilter{})
			return err
		}},
		{"path search", func(ctx context.Context) error {
			_, err := repo.SearchHistory(ctx, "", "file.txt", 0, HistoryFilter{})
			return err
		}},
		{"search", func(ctx context.Context) error {
			_, err := repo.SearchHistory(ctx, "base", "", 0, HistoryFilter{})
			return err
		}},
		{"files", func(ctx context.Context) error {
			_, err := repo.CommitFiles(ctx, hash)
			return err
		}},
		{"diff", func(ctx context.Context) error {
			_, err := repo.CommitFileDiff(ctx, hash, "file.txt")
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			repo.mu.Lock()
			result := make(chan error, 1)
			go func() { result <- operation.run(ctx) }()
			select {
			case err := <-result:
				repo.mu.Unlock()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("waiting for repository lock: %v", err)
				}
			case <-time.After(time.Second):
				repo.mu.Unlock()
				<-result
				t.Fatal("operation ignored deadline while waiting for repository lock")
			}
		})
	}
}

func installHistoryGitWrapper(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Git process wrapper requires a POSIX shell")
	}
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XSOURCE_REAL_GIT", actual)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func historyLogCallCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(data), "log\n")
}

func historyStreamRecord(index int, subject, body string) string {
	return strings.Join([]string{
		fmt.Sprintf("%040x", index), "", subject, "Archive User", "2026-11-10T00:00:00Z", "", body, "archive@example.test",
	}, "\x00") + "\x00"
}

func TestHistoryStreamParsesFragmentedFieldsAndStopsAtPage(t *testing.T) {
	stops := 0
	writer := newHistoryLogWriter(context.Background(), "BODY needle", 1, func() { stops++ })
	data := historyStreamRecord(1, "not a match", "other") +
		historyStreamRecord(2, "skip matched", "body NEEDLE") +
		historyStreamRecord(3, "selected", "BODY needle")
	for index := range len(data) {
		if _, err := writer.Write([]byte(data[index : index+1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	if len(writer.commits) != 1 || writer.commits[0].Subject != "selected" || stops != 0 {
		t.Fatalf("fragmented stream: %+v, stops %d", writer.commits, stops)
	}
	writer = newHistoryLogWriter(context.Background(), "", 0, func() { stops++ })
	var page strings.Builder
	for index := 1; index <= 101; index++ {
		page.WriteString(historyStreamRecord(index, "page", "page"))
	}
	page.WriteString("ignored incomplete tail")
	if _, err := io.WriteString(writer, page.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "more ignored data"); err != nil {
		t.Fatal(err)
	}
	if err := writer.finish(); err != nil || !writer.stopped || len(writer.commits) != 100 || stops != 1 {
		t.Fatalf("bounded page: %d commits, stops %d, %v", len(writer.commits), stops, err)
	}
}

func TestHistoryStreamBoundsRecordsWithoutBoundingWholeHistory(t *testing.T) {
	writer := newHistoryLogWriter(context.Background(), "missing needle", 0, func() {})
	body := strings.Repeat("x", outputLimit/2)
	for index := 1; index <= 12; index++ {
		if _, err := io.WriteString(writer, historyStreamRecord(index, "large body", body)); err != nil {
			t.Fatalf("bounded record %d: %v", index, err)
		}
	}
	if err := writer.finish(); err != nil || len(writer.commits) != 0 {
		t.Fatalf("large total stream: %+v, %v", writer.commits, err)
	}
	for _, data := range []string{
		strings.Repeat("x", outputLimit+1),
		strings.Repeat("x", outputLimit/2) + "\x00" + strings.Repeat("y", outputLimit/2),
	} {
		stopped := false
		writer := newHistoryLogWriter(context.Background(), "", 0, func() { stopped = true })
		if _, err := io.WriteString(writer, data); err == nil || !stopped {
			t.Fatal("oversized field or record was not rejected and canceled")
		}
		if err := writer.finish(); err == nil {
			t.Fatal("oversized stream reported success")
		}
	}
	for _, data := range []string{"unfinished", "one\x00two\x00"} {
		writer := newHistoryLogWriter(context.Background(), "", 0, func() {})
		if _, err := io.WriteString(writer, data); err != nil {
			t.Fatal(err)
		}
		if err := writer.finish(); err == nil {
			t.Fatal("incomplete record was accepted")
		}
	}
}

func TestHistoryStreamCallerCancellationWinsOverFullPage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := false
	writer := newHistoryLogWriter(ctx, "", 0, func() { stopped = true })
	for index := 1; index <= 100; index++ {
		if _, err := io.WriteString(writer, historyStreamRecord(index, "page", "body")); err != nil {
			t.Fatal(err)
		}
	}
	if !stopped || !writer.stopped {
		t.Fatal("full page did not stop the child context")
	}
	cancel()
	if err := writer.finish(); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation was hidden by a full page: %v", err)
	}
}

func TestHistoryStreamCommandErrorsAndUserCancellation(t *testing.T) {
	t.Run("command failure", func(t *testing.T) {
		repo, _ := fixture(t)
		installHistoryGitWrapper(t, "printf 'history failure\\n' >&2\nexit 73\n")
		_, err := repo.SearchHistory(context.Background(), "query", "", 0, HistoryFilter{})
		if !exitCode(err, 73) || !strings.Contains(err.Error(), "history failure") {
			t.Fatalf("stream command error: %v", err)
		}
	})
	t.Run("cancellation during log", func(t *testing.T) {
		repo, _ := fixture(t)
		started := filepath.Join(t.TempDir(), "started")
		t.Setenv("XSOURCE_STARTED", started)
		installHistoryGitWrapper(t, "printf started > \"$XSOURCE_STARTED\"\nsleep 10\n")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := repo.SearchHistory(ctx, "query", "", 0, HistoryFilter{})
			done <- err
		}()
		for {
			if _, err := os.Stat(started); err == nil {
				break
			}
			select {
			case err := <-done:
				t.Fatalf("log did not start: %v", err)
			case <-time.After(10 * time.Millisecond):
			}
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("running log cancellation: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("running log did not respond to cancellation")
		}
	})
	t.Run("bounded stderr", func(t *testing.T) {
		repo, _ := fixture(t)
		installHistoryGitWrapper(t, "head -c 4194305 /dev/zero >&2\n")
		_, err := repo.SearchHistory(context.Background(), "query", "", 0, HistoryFilter{})
		if err == nil || !strings.Contains(err.Error(), "4 MiB") {
			t.Fatalf("oversized stderr was accepted: %v", err)
		}
	})
}

func TestHistoryCommandsDisableSignatureVerification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signature marker requires a POSIX shell")
	}
	repo, dir := fixture(t)
	write(t, dir, "signed.txt", "signed content\n")
	git(t, dir, "add", ".")
	tree := git(t, dir, "write-tree")
	data := "tree " + tree + "\nauthor Signed User <signed@example.test> 1800000000 +0000\n" +
		"committer Signed User <signed@example.test> 1800000000 +0000\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n fake signature\n -----END PGP SIGNATURE-----\n\nsigned root\n"
	command := exec.Command("git", "-C", dir, "hash-object", "-t", "commit", "-w", "--stdin")
	command.Env = gitEnvironment()
	command.Stdin = strings.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("write signed fixture: %v\n%s", err, output)
	}
	hash := strings.TrimSpace(string(output))
	git(t, dir, "update-ref", "refs/heads/main", hash)
	marker := filepath.Join(t.TempDir(), "gpg-called")
	t.Setenv("XSOURCE_GPG_MARKER", marker)
	gpg := filepath.Join(t.TempDir(), "gpg")
	if err := os.WriteFile(gpg, []byte("#!/bin/sh\nprintf called > \"$XSOURCE_GPG_MARKER\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "config", "gpg.program", gpg)
	git(t, dir, "config", "gpg.format", "openpgp")
	git(t, dir, "config", "log.showSignature", "true")
	git(t, dir, "log", "-1", "--format=%H", hash)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("signature verification positive control: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	commits, err := repo.History(context.Background(), 0)
	if err != nil || len(commits) != 1 || commits[0].Hash != hash {
		t.Fatalf("signed initial history: %+v, %v", commits, err)
	}
	for _, query := range []string{"", "SIGNED"} {
		commits, err := repo.SearchHistory(context.Background(), query, "", 0, HistoryFilter{})
		if err != nil || len(commits) != 1 || commits[0].Hash != hash {
			t.Fatalf("signed history: %+v, %v", commits, err)
		}
	}
	diff, err := repo.CommitFileDiff(context.Background(), hash, "signed.txt")
	if err != nil || !strings.Contains(diff.Text, "+signed content") {
		t.Fatalf("signed file diff: %+v, %v", diff, err)
	}
	all, err := repo.CommitDiff(context.Background(), hash)
	if err != nil || !strings.Contains(all.Text, "diff --git a/signed.txt b/signed.txt") ||
		!strings.Contains(all.Text, "+signed content") {
		t.Fatalf("signed full diff: %+v, %v", all, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("signature verifier was invoked: %v", err)
	}
}

func TestCommitFileDiffExcludesDirectoryDescendants(t *testing.T) {
	for _, path := range []string{"entry", "entry with space", "中文\tquote\"back\\line\nentry"} {
		t.Run(path, func(t *testing.T) {
			repo, dir := fixture(t)
			write(t, dir, path, "removed file content\n")
			git(t, dir, "add", ".")
			git(t, dir, "commit", "-m", "file")
			if err := os.Remove(filepath.Join(dir, path)); err != nil {
				t.Fatal(err)
			}
			write(t, dir, path+"/child.txt", "descendant addition\n")
			write(t, dir, path+"/binary.bin", "\x00descendant binary\xff")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-m", "file to directory")
			hash := git(t, dir, "rev-parse", "HEAD")
			git(t, dir, "config", "core.quotePath", "false")
			git(t, dir, "config", "diff.noprefix", "true")
			git(t, dir, "config", "diff.mnemonicPrefix", "true")
			git(t, dir, "config", "color.diff", "always")
			diff, err := repo.CommitFileDiff(context.Background(), hash, path)
			if err != nil || !strings.Contains(diff.Text, "-removed file content") ||
				strings.Contains(diff.Text, "descendant") || diff.Binary || strings.Count(diff.Text, "diff --git ") != 1 {
				t.Fatalf("file to directory diff: %+v, %v", diff, err)
			}
			if err := os.RemoveAll(filepath.Join(dir, path)); err != nil {
				t.Fatal(err)
			}
			write(t, dir, path, "replacement file content\n")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-m", "directory to file")
			hash = git(t, dir, "rev-parse", "HEAD")
			diff, err = repo.CommitFileDiff(context.Background(), hash, path)
			if err != nil || !strings.Contains(diff.Text, "+replacement file content") ||
				strings.Contains(diff.Text, "descendant") || diff.Binary || strings.Count(diff.Text, "diff --git ") != 1 {
				t.Fatalf("directory to file diff: %+v, %v", diff, err)
			}
		})
	}
}

func TestCommitFileDiffRenameWithReusedSource(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			repo, dir := fixture(t)
			original := "old\t\"中文\nfile"
			renamed := "new \"\t中文\nfile"
			write(t, dir, original, "first\nsecond\nthird\n")
			git(t, dir, "add", ".")
			git(t, dir, "commit", "-m", "base")
			git(t, dir, "mv", "--", original, renamed)
			reused := original
			if directory {
				reused += "/child.txt"
			}
			write(t, dir, reused, "unrelated replacement\n")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-m", "rename and reuse")
			hash := git(t, dir, "rev-parse", "HEAD")
			expected := []CommitFile{
				{Path: renamed, Status: "A"},
				{Path: reused, Status: "M"},
			}
			if directory {
				expected[0].Status = "R"
				expected[0].OriginalPath = original
				expected[1].Status = "A"
			}
			files, err := repo.CommitFiles(context.Background(), hash)
			if err != nil || !reflect.DeepEqual(files, expected) {
				t.Fatalf("reused source file statuses: %+v, %v; want %+v", files, err, expected)
			}
			diff, err := repo.CommitFileDiff(context.Background(), hash, renamed)
			if err != nil || strings.Contains(diff.Text, "unrelated replacement") ||
				strings.Count(diff.Text, "diff --git ") != 1 {
				t.Fatalf("rename with reused source: %+v, %v", diff, err)
			}
			if directory {
				if !strings.Contains(diff.Text, "rename from ") || !strings.Contains(diff.Text, "rename to ") {
					t.Fatalf("directory replacement must retain rename patch: %+v", diff)
				}
			} else if !strings.Contains(diff.Text, "new file mode ") || !strings.Contains(diff.Text, "+first") ||
				!strings.Contains(diff.Text, "+second") || !strings.Contains(diff.Text, "+third") ||
				strings.Contains(diff.Text, "rename from ") {
				t.Fatalf("file replacement must add destination independently: %+v", diff)
			}
			diff, err = repo.CommitFileDiff(context.Background(), hash, reused)
			if err != nil || !strings.Contains(diff.Text, "+unrelated replacement") ||
				strings.Contains(diff.Text, "rename from ") || strings.Contains(diff.Text, "+first") ||
				strings.Count(diff.Text, "diff --git ") != 1 {
				t.Fatalf("replacement file diff: %+v, %v", diff, err)
			}
		})
	}
}

func TestCommitFileDiffPreservesBothSidesOfTypeChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix symlink permissions")
	}
	repo, dir := fixture(t)
	write(t, dir, "entry", "original content\n")
	write(t, dir, "other.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	if err := os.Remove(filepath.Join(dir, "entry")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(dir, "entry")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "other.txt", "unrelated modification\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "regular file to symlink")
	hash := git(t, dir, "rev-parse", "HEAD")
	diff, err := repo.CommitFileDiff(context.Background(), hash, "entry")
	if err != nil || !strings.Contains(diff.Text, "-original content") ||
		!strings.Contains(diff.Text, "+target.txt") || strings.Contains(diff.Text, "unrelated modification") {
		t.Fatalf("type change patch: %+v, %v", diff, err)
	}
}
