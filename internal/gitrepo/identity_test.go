// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func identityFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, _ := fixture(t)
	git(t, repo.Path, "config", "--unset-all", "user.name")
	git(t, repo.Path, "config", "--unset-all", "user.email")
	return repo, home
}

func saveGlobalIdentity(t *testing.T, name, email string) {
	t.Helper()
	settings, err := GlobalIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveGlobalIdentity(context.Background(), IdentityRequest{Name: name, Email: email, Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityScopesAndInheritance(t *testing.T) {
	repo, _ := identityFixture(t)
	ctx := context.Background()
	saveGlobalIdentity(t, "Global User", "global@example.test")
	settings, err := repo.Identity(ctx)
	if err != nil || settings.Overrides() || settings.Name.Value != "Global User" || settings.Name.Scope != "global" {
		t.Fatalf("未继承全局身份: %+v %v", settings, err)
	}
	if err := repo.SaveIdentity(ctx, IdentityRequest{Name: "Local User", Email: "local@example.test", Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	settings, err = repo.Identity(ctx)
	if err != nil || !settings.Overrides() || settings.Email.Scope != "local" || settings.Email.Value != "local@example.test" {
		t.Fatalf("仓库身份未生效: %+v %v", settings, err)
	}
	global, err := GlobalIdentity(ctx)
	if err != nil || global.Email.Value != "global@example.test" {
		t.Fatalf("仓库保存影响了全局: %+v %v", global, err)
	}
	if err := repo.SaveIdentity(ctx, IdentityRequest{Inherit: true, Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	saveGlobalIdentity(t, "Changed Global", "changed@example.test")
	settings, err = repo.Identity(ctx)
	if err != nil || settings.Overrides() || settings.Name.Value != "Changed Global" {
		t.Fatalf("恢复继承后未随全局配置变化: %+v %v", settings, err)
	}
}

func TestIdentityPreservesIncludesAndOtherConfig(t *testing.T) {
	repo, home := identityFixture(t)
	ctx := context.Background()
	saveGlobalIdentity(t, "Default", "default@example.test")
	include := filepath.Join(home, "work.gitconfig")
	git(t, home, "config", "--file", include, "user.email", "work@example.test")
	git(t, home, "config", "--file", filepath.Join(home, ".gitconfig"),
		"includeIf.gitdir:"+filepath.ToSlash(repo.Path)+"/.path", include)
	localInclude := filepath.Join(home, "local.gitconfig")
	git(t, home, "config", "--file", localInclude, "user.name", "Included Name")
	git(t, repo.Path, "config", "include.path", localInclude)
	git(t, repo.Path, "config", "user.name", "Direct Name")
	git(t, repo.Path, "config", "remote.origin.url", "https://example.test/repo.git")
	settings, err := repo.Identity(ctx)
	if err != nil || settings.Email.Value != "work@example.test" || !strings.Contains(settings.Email.Origin, "work.gitconfig") {
		t.Fatalf("条件配置未生效: %+v %v", settings, err)
	}
	if err := repo.SaveIdentity(ctx, IdentityRequest{Inherit: true, Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	settings, err = repo.Identity(ctx)
	if err != nil || settings.Overrides() || settings.Name.Value != "Included Name" || settings.Email.Value != "work@example.test" {
		t.Fatalf("继承操作破坏了 include: %+v %v", settings, err)
	}
	if got := git(t, repo.Path, "config", "remote.origin.url"); got != "https://example.test/repo.git" {
		t.Fatal("修改身份影响了远程配置")
	}
}

func TestIdentityRejectsStaleInvalidAndLockedWrites(t *testing.T) {
	repo, _ := identityFixture(t)
	ctx := context.Background()
	saveGlobalIdentity(t, "Default", "default@example.test")
	settings, err := repo.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "Bad\nName", "<Bad>"} {
		if err := repo.SaveIdentity(ctx, IdentityRequest{Name: name, Email: "valid@example.test", Token: settings.Token}); err == nil {
			t.Fatal("无效身份被保存")
		}
	}
	git(t, repo.Path, "config", "user.name", "External Change")
	if err := repo.SaveIdentity(ctx, IdentityRequest{Name: "New Name", Email: "new@example.test", Token: settings.Token}); err == nil {
		t.Fatal("过期设置覆盖了外部变更")
	}
	global, err := GlobalIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lock := os.Getenv("GIT_CONFIG_GLOBAL") + ".lock"
	if err := os.WriteFile(lock, []byte("other writer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveGlobalIdentity(ctx, IdentityRequest{Name: "New Name", Email: "new@example.test", Token: global.Token}); err == nil {
		t.Fatal("忽略了 Git 配置锁")
	}
	after, err := GlobalIdentity(ctx)
	if err != nil || after.Token != global.Token {
		t.Fatalf("失败写入改变了配置: %+v %v", after, err)
	}
}

func TestIdentityWorktreeSourceAndLinkedRepository(t *testing.T) {
	repo, _ := identityFixture(t)
	ctx := context.Background()
	saveGlobalIdentity(t, "Default", "default@example.test")
	git(t, repo.Path, "-c", "user.name=Initial", "-c", "user.email=initial@example.test", "commit", "--allow-empty", "-m", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo.Path, "worktree", "add", "-b", "linked", linked)
	git(t, repo.Path, "config", "extensions.worktreeConfig", "true")
	git(t, linked, "config", "--worktree", "user.email", "worktree@example.test")
	linkedRepo, err := Open(ctx, linked)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := linkedRepo.Identity(ctx)
	if err != nil || settings.Email.Scope != "worktree" {
		t.Fatalf("未展示工作树来源: %+v %v", settings, err)
	}
	if err := linkedRepo.SaveIdentity(ctx, IdentityRequest{Name: "Repo User", Email: "repo@example.test", Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	settings, err = linkedRepo.Identity(ctx)
	if err != nil || settings.Email.Value != "worktree@example.test" {
		t.Fatalf("覆盖了工作树配置: %+v %v", settings, err)
	}
	if err := linkedRepo.SaveIdentity(ctx, IdentityRequest{Inherit: true, Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	settings, err = linkedRepo.Identity(ctx)
	if err != nil || settings.Overrides() || settings.Email.Value != "worktree@example.test" || settings.Name.Value != "Default" {
		t.Fatalf("恢复继承破坏了工作树身份: %+v %v", settings, err)
	}
}

func TestGlobalIdentitySelectsXDGWriteTarget(t *testing.T) {
	_, home := identityFixture(t)
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".config", "git", "config")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	git(t, home, "config", "--file", target, "user.name", "XDG Name")
	saveGlobalIdentity(t, "Changed XDG", "xdg@example.test")
	if got := git(t, home, "config", "--file", target, "user.name"); got != "Changed XDG" {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig")); !os.IsNotExist(err) {
		t.Fatal("错误创建了优先级更高的全局文件")
	}
}

func TestIdentityRestoresNameWhenEmailWriteFails(t *testing.T) {
	for _, phase := range []string{"before-write", "after-write"} {
		t.Run(phase, func(t *testing.T) {
			repo, home := identityFixture(t)
			ctx := context.Background()
			git(t, repo.Path, "config", "--add", "user.name", "First Name")
			git(t, repo.Path, "config", "--add", "user.name", "Last Name")
			git(t, repo.Path, "config", "user.email", "original@example.test")
			settings, err := repo.Identity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			gitPath, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(home, "bin")
			if err := os.Mkdir(wrapper, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("IDENTITY_TEST_GIT", gitPath)
			failure := "exit 42"
			if phase == "after-write" {
				failure = "\"$IDENTITY_TEST_GIT\" \"$@\"; exit 42"
			}
			script := "#!/bin/sh\ncase \"$*\" in *'--replace-all user.email new@example.test') " + failure + ";; esac\nexec \"$IDENTITY_TEST_GIT\" \"$@\"\n"
			if err := os.WriteFile(filepath.Join(wrapper, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
			err = repo.SaveIdentity(ctx, IdentityRequest{Name: "New Name", Email: "new@example.test", Token: settings.Token})
			if err == nil {
				t.Fatal("邮箱写入失败却返回成功")
			}
			after, err := repo.Identity(ctx)
			if err != nil || after.Token != settings.Token {
				t.Fatalf("姓名未完整恢复: %+v %v", after, err)
			}
		})
	}
}
