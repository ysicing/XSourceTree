// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeFileFilters(t *testing.T) {
	app := &nativeApp{repository: &repositoryView{snapshot: gitrepo.Snapshot{Files: []gitrepo.File{
		{Path: "src/mixed.go", Index: "A", Working: "M"},
		{Path: "src/new.go", Index: "?", Working: "?"},
		{Path: "src/conflict.go", Index: "U", Working: "U", Conflict: true},
		{Path: "docs/old.md", Index: " ", Working: "D"},
	}}}}
	for _, test := range []struct {
		filter, query string
		staged        bool
		want          []string
	}{
		{"", "", false, []string{"src/mixed.go", "src/new.go", "src/conflict.go", "docs/old.md"}},
		{"tracked", "", false, []string{"src/mixed.go", "src/conflict.go", "docs/old.md"}},
		{"untracked", "", false, []string{"src/new.go"}},
		{"conflicts", "", false, []string{"src/conflict.go"}},
		{"tracked", "SRC/", false, []string{"src/mixed.go", "src/conflict.go"}},
		{"tracked", "", true, []string{"src/mixed.go"}},
		{"untracked", "", true, nil},
		{"conflicts", "", true, nil},
	} {
		app.fileFilter, app.fileQuery = test.filter, test.query
		var got []string
		for _, file := range app.visibleFiles(test.staged) {
			got = append(got, file.Path)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("filter=%s query=%s staged=%t: got %v, want %v", test.filter, test.query, test.staged, got, test.want)
		}
	}
}

func TestNativeFileSortAndTree(t *testing.T) {
	files := []gitrepo.File{
		{Path: "b/main.go", Index: "A", Working: "M"},
		{Path: "a/z.go", Index: "M", Working: "D"},
		{Path: "a/main.go", Index: "D", Working: "A"},
	}
	for _, test := range []struct {
		order  string
		staged bool
		want   []string
	}{
		{"path", false, []string{"a/main.go", "a/z.go", "b/main.go"}},
		{"name", false, []string{"a/main.go", "b/main.go", "a/z.go"}},
		{"status", false, []string{"a/main.go", "a/z.go", "b/main.go"}},
		{"status", true, []string{"b/main.go", "a/main.go", "a/z.go"}},
	} {
		for _, descending := range []bool{false, true} {
			sorted := slices.Clone(files)
			slices.SortFunc(sorted, func(first, second gitrepo.File) int {
				return compareNativeFiles(first, second, test.order, descending, test.staged)
			})
			want := slices.Clone(test.want)
			if descending {
				slices.Reverse(want)
			}
			for index, file := range sorted {
				if file.Path != want[index] {
					t.Fatalf("排序 %s descending=%t staged=%t: %v", test.order, descending, test.staged, sorted)
				}
			}
		}
	}
	files = append(files, gitrepo.File{Path: "a", Index: "D", Working: " "})
	nodes := nativeFileNodes(files, "name", false, true)
	if len(nodes) != 3 || nodes[0].path != "a" || nodes[0].file != nil || nodes[2].file == nil {
		t.Fatal("同名目录与已删除文件发生冲突")
	}
	if len(nodes[0].children) != 2 || nodes[0].children[0].file.Path != "a/main.go" ||
		nodes[1].children[0].file.Path != "b/main.go" {
		t.Fatal("目录树未保留同名文件的完整路径")
	}
}

func TestNativeFilePreferencesMigration(t *testing.T) {
	app, _, _ := nativeFixture(t)
	for _, test := range []struct{ data, want string }{
		{`{"byName":true}`, "name"},
		{`{"byName":false}`, "path"},
		{`{"byName":true,"fileSort":"status","fileDescending":true,"fileTree":true}`, "status"},
		{`{"fileSort":"unknown"}`, "path"},
	} {
		stateFile := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(stateFile, []byte(test.data), 0600); err != nil {
			t.Fatal(err)
		}
		restored := newNativeApp(app.service, stateFile)
		t.Cleanup(restored.cancel)
		if restored.prefs.FileSort != test.want {
			t.Fatalf("偏好迁移失败: %s -> %s", test.data, restored.prefs.FileSort)
		}
		restored.prefs.FileTree, restored.prefs.FileDescending = true, true
		if err := restored.savePreferences(); err != nil {
			t.Fatal(err)
		}
		reopened := newNativeApp(app.service, stateFile)
		t.Cleanup(reopened.cancel)
		if reopened.prefs.FileSort != test.want || !reopened.prefs.FileTree || !reopened.prefs.FileDescending {
			t.Fatal("未持久化文件显示偏好")
		}
	}
}

func TestNativeStatusFilteredBulkStaging(t *testing.T) {
	for _, all := range []bool{false, true} {
		app, path, finish := nativeFixture(t)
		writeNativeFile(t, path, "hello.txt", "tracked change\n")
		writeNativeFile(t, path, "new.txt", "untracked change\n")
		app.refresh()
		finish()
		app.selectedFiles["false:hello.txt"], app.selectedFiles["false:new.txt"] = true, true
		app.fileFilter = "untracked"
		app.stageFiles(false, all)
		finish()
		if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "new.txt" {
			t.Fatalf("状态筛选暂存了隐藏文件: all=%t %s", all, got)
		}
		app.fileFilter, app.fileQuery = "tracked", "hello"
		nativeGit(t, path, "add", "hello.txt")
		app.refresh()
		finish()
		app.selectedFiles["true:hello.txt"], app.selectedFiles["true:new.txt"] = true, true
		app.stageFiles(true, all)
		finish()
		if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "new.txt" {
			t.Fatalf("取消暂存影响了筛选外的文件: all=%t %s", all, got)
		}
	}
}

func TestNativeFileViewKeepsContext(t *testing.T) {
	app, path, finish := nativeFixture(t)
	for _, name := range []string{"src/api/main.go", "src/ui/main.go", "docs/guide.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(path, name)), 0700); err != nil {
			t.Fatal(err)
		}
		writeNativeFile(t, path, name, "original\n")
	}
	nativeGit(t, path, "add", "src")
	nativeGit(t, path, "commit", "-m", "tree fixtures")
	writeNativeFile(t, path, "src/api/main.go", "staged\n")
	nativeGit(t, path, "add", "src/api/main.go")
	writeNativeFile(t, path, "src/api/main.go", "working\n")
	writeNativeFile(t, path, "src/ui/main.go", "working\n")
	app.refresh()
	finish()
	app.selectFile("src/api/main.go", false)
	finish()
	diff := app.diff
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("选择 未暂存文件 src/api/main.go"); err != nil {
		t.Fatal(err)
	}
	choose := func(button, item string) {
		t.Helper()
		if err := tester.Click(button); err != nil {
			t.Fatal(err)
		}
		if err := tester.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
	}
	choose("文件视图", "目录树")
	choose("文件排序", "按文件状态排序")
	choose("文件排序", "倒序")
	if app.selectedFile != "src/api/main.go" || !reflect.DeepEqual(app.diff, diff) || !app.selectedFiles["false:src/api/main.go"] {
		t.Fatal("视图切换丢失文件、Diff 或勾选状态")
	}
	if err := tester.Click("src/ui/main.go"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.selectedFile != "src/ui/main.go" || app.staged || app.diff.Text == "" {
		t.Fatal("点击目录树文件未更新所选文件和 Diff")
	}
	app.selectFile("src/api/main.go", false)
	finish()
	tester.Frame()
	if err := tester.Click("未暂存目录 src"); err != nil {
		t.Fatal(err)
	}
	if directory, _ := tester.Access("未暂存目录 src"); directory.Expanded {
		t.Fatal("目录未折叠")
	}
	if directory, _ := tester.Access("已暂存目录 src"); !directory.Expanded {
		t.Fatal("折叠未暂存目录影响了已暂存目录")
	}
	tester.Key(0, ui.KeyRight)
	if directory, _ := tester.Access("未暂存目录 src"); !directory.Expanded {
		t.Fatal("方向键未展开目录")
	}
	choose("文件筛选", "未跟踪")
	if len(app.checkedFiles(false)) != 0 || !app.selectedFiles["false:src/api/main.go"] {
		t.Fatal("隐藏勾选项应保留选择但不参与批量操作")
	}
	if _, found := tester.Find("暂存筛选结果 (1)"); !found {
		t.Fatal("批量按钮未说明筛选范围")
	}
	if err := tester.Click("清除筛选"); err != nil {
		t.Fatal(err)
	}
	if len(app.checkedFiles(false)) != 1 || !reflect.DeepEqual(app.diff, diff) {
		t.Fatal("清除筛选未恢复勾选或改变了 Diff")
	}
	for _, size := range []struct {
		name          string
		width, height int
		panel         float32
	}{{"wide", 1320, 860, 420}, {"compact", 1024, 680, 260}} {
		app.prefs.Files = size.panel
		tester.SetSize(size.width, size.height)
		for _, dark := range []bool{false, true} {
			tester.SetDark(dark)
			for _, label := range []string{"文件筛选", "文件排序", "文件视图", "已暂存文件操作", "未暂存文件操作"} {
				info, found := tester.Access(label)
				if !found || info.Offscreen || info.Bounds.W <= 0 || info.Bounds.H <= 0 {
					t.Fatalf("%s: 控件不可见: %s", size.name, label)
				}
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				theme := "light"
				if dark {
					theme = "dark"
				}
				saveNativeScreenshot(t, tester, directory, "file-tree-"+size.name+"-"+theme+".png")
			}
		}
	}
	choose("文件筛选", "已跟踪变更")
	choose("文件排序", "按文件名排序")
	if info, found := tester.Access("文件视图"); !found || info.Offscreen || info.Bounds.W <= 0 {
		t.Fatal("最窄面板的文件名倒序与已跟踪筛选挤出了视图入口")
	}
	choose("文件筛选", "冲突")
	if _, found := tester.Find("无匹配文件"); !found || len(app.visibleFiles(false)) != 0 {
		t.Fatal("冲突筛选空状态不正确")
	}
	if err := tester.Click("清除筛选"); err != nil {
		t.Fatal(err)
	}
	choose("文件排序", "按文件状态排序")
	choose("文件视图", "平面列表")
	if !reflect.DeepEqual(app.diff, diff) || !app.selectedFiles["false:src/api/main.go"] {
		t.Fatal("返回平面列表丢失审阅状态")
	}
	app.fileFilter, app.fileQuery = "conflicts", "old-query"
	other, otherPath, _ := nativeFixture(t)
	other.cancel()
	app.openRepository(otherPath)
	finish()
	if app.filesFiltered() || len(app.fileCollapsed) != 0 || app.prefs.FileSort != "status" || !app.prefs.FileDescending {
		t.Fatal("切换仓库应清空临时筛选并保留排序偏好")
	}
}
