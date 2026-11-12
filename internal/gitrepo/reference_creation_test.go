// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReferenceCreationTargetsAndNamespaces(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "tag", "--no-sign", "release")
	git(t, dir, "tag", "-a", "--no-sign", "-m", "release", "annotated")
	git(t, dir, "tag", "-a", "--no-sign", "-m", "nested", "nested", "annotated")
	git(t, dir, "commit", "--allow-empty", "-m", "next")
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "branch", "release")
	for _, reference := range []string{"refs/tags/release", "refs/tags/annotated", "refs/tags/nested", base} {
		target, err := repo.ResolveCreationTarget(ctx, reference)
		if err != nil || target != base {
			t.Fatalf("resolve %s: %s %v", reference, target, err)
		}
	}
	target, err := repo.ResolveCreationTarget(ctx, "refs/heads/release")
	if err != nil || target != head {
		t.Fatalf("namesake branch target: %s %v", target, err)
	}
	write(t, dir, "dirty.txt", "keep me\n")
	before := git(t, dir, "status", "--porcelain=v1")
	if err := repo.CreateBranchAt(ctx, "hotfix", base); err != nil {
		t.Fatal(err)
	}
	if git(t, dir, "rev-parse", "HEAD") != head || git(t, dir, "rev-parse", "hotfix") != base ||
		git(t, dir, "status", "--porcelain=v1") != before {
		t.Fatal("create-only changed HEAD, target or working files")
	}
	if err := repo.CreateTag(ctx, "hotfix", head); err != nil {
		t.Fatal("branch and tag namespaces should be independent:", err)
	}
	if err := repo.CreateBranchAt(ctx, "hotfix", head); err == nil {
		t.Fatal("overwrote an existing branch")
	}
	if git(t, dir, "rev-parse", "refs/heads/hotfix") != base {
		t.Fatal("duplicate creation moved branch")
	}
}

func TestReferenceNameValidationAndRecheck(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "branch", "feature/login")
	git(t, dir, "tag", "--no-sign", "release/v1")
	for _, item := range []struct {
		name string
		tag  bool
	}{
		{"feature/login", false}, {"feature", false}, {"feature/login/child", false},
		{"release", true}, {"release/v1", true}, {"release/v1/child", true},
		{"", false}, {"bad name", false}, {"HEAD", false}, {"-force", true},
		{"bad..name", true}, {"@{-1}", false}, {"end.lock", false},
	} {
		if err := repo.CheckReferenceName(ctx, item.name, item.tag); err == nil {
			t.Errorf("accepted unavailable name %q tag=%v", item.name, item.tag)
		}
	}
	for _, item := range []struct {
		name string
		tag  bool
	}{{"release/v1", false}, {"feature/login", true}, {"功能/修复", false}, {"v1.2.3", true}} {
		if err := repo.CheckReferenceName(ctx, item.name, item.tag); err != nil {
			t.Errorf("rejected valid name %q: %v", item.name, err)
		}
	}
	if err := repo.CheckReferenceName(ctx, "raced", false); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "branch", "raced")
	if err := repo.CreateBranchAt(ctx, "raced", hash); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("creation did not recheck a stale availability result: %v", err)
	}
	if err := repo.CheckReferenceName(ctx, "tag-raced", true); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "tag", "--no-sign", "tag-raced")
	if err := repo.CreateTag(ctx, "tag-raced", hash); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("tag creation did not recheck: %v", err)
	}
}

func TestReferenceCreationRejectsInvalidTargetsAndCancellation(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")
	git(t, dir, "tag", "--no-sign", "tree", tree)
	for _, reference := range []string{"HEAD", "--all", "refs/heads/main~1", "refs/tags/missing", "refs/tags/tree", tree} {
		if _, err := repo.ResolveCreationTarget(ctx, reference); err == nil {
			t.Errorf("accepted target %q", reference)
		}
	}
	if err := repo.CreateBranchAt(ctx, "tree-branch", tree); err == nil {
		t.Fatal("created branch from tree")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	repo.mu.Lock()
	if err := repo.CheckReferenceName(cancelled, "cancelled", false); !errors.Is(err, context.Canceled) {
		t.Errorf("name check: %v", err)
	}
	if err := repo.CreateBranchAt(cancelled, "cancelled", hash); !errors.Is(err, context.Canceled) {
		t.Errorf("branch creation: %v", err)
	}
	if _, err := repo.ResolveCreationTarget(cancelled, hash); !errors.Is(err, context.Canceled) {
		t.Errorf("target resolution: %v", err)
	}
	repo.mu.Unlock()
}

func TestCreatedBranchSwitchFailureAndRetry(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	write(t, dir, "file.txt", "next\n")
	git(t, dir, "commit", "-am", "next")
	head := git(t, dir, "rev-parse", "HEAD")
	write(t, dir, "file.txt", "unsaved\n")
	if err := repo.CreateBranchAt(ctx, "hotfix", base); err != nil {
		t.Fatal(err)
	}
	if err := repo.SwitchCreatedBranch(ctx, "hotfix", base); err == nil {
		t.Fatal("switched over dirty work")
	}
	if git(t, dir, "rev-parse", "HEAD") != head || git(t, dir, "rev-parse", "hotfix") != base {
		t.Fatal("switch failure lost created branch or changed HEAD")
	}
	write(t, dir, "file.txt", "next\n")
	if err := repo.SwitchCreatedBranch(ctx, "hotfix", base); err != nil {
		t.Fatal("retry switch:", err)
	}
	git(t, dir, "update-ref", "refs/heads/hotfix", head)
	if err := repo.SwitchCreatedBranch(ctx, "hotfix", base); err == nil {
		t.Fatal("retry accepted externally moved branch")
	}
}
