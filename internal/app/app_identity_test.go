// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ysicing/51code/internal/workspace"

	"github.com/egoist/mygo"
)

func TestRenamedApplicationKeepsLegacyData(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		legacy string
	}{
		{name: "51Code", legacy: "XSourceTree"},
		{name: "51Code Dev", legacy: "XSourceTree Dev"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			t.Setenv("APPDATA", home)
			config, err := os.UserConfigDir()
			if err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(config, scenario.legacy)
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"recent.json":       `[{"Name":"kept","Path":"/legacy/repository"}]`,
				"native-state.json": `{"appearance":"深色","drafts":{"/legacy/repository":"未提交草稿"}}`,
				"locale":            "zh-Hant",
			}
			for name, content := range files {
				writeNativeFile(t, directory, name, content)
			}
			application := mygo.App
			previousName := application.Name()
			t.Cleanup(func() {
				application.SetName(previousName)
				application.SetPath(mygo.PathUserData, "")
			})
			application.SetName(scenario.name)
			if err := preserveLegacyApplicationIdentity(application); err != nil {
				t.Fatal(err)
			}
			if application.Name() != scenario.legacy {
				t.Fatalf("单实例锁身份未沿用旧名称: %s", application.Name())
			}
			application.SetName(scenario.name)
			actual, err := application.Path(mygo.PathUserData)
			if err != nil || actual != directory {
				t.Fatalf("数据目录不兼容: %q, %v", actual, err)
			}
			manager, err := workspace.New(filepath.Join(actual, "recent.json"))
			if err != nil {
				t.Fatal(err)
			}
			if recent := manager.Recent(); len(recent) != 1 || recent[0].Path != "/legacy/repository" {
				t.Fatalf("旧仓库列表丢失: %+v", recent)
			}
			app := newNativeApp(&RepositoryService{manager: manager}, filepath.Join(actual, "native-state.json"))
			defer app.cancel()
			if app.prefs.Appearance != appearanceDark || app.prefs.Drafts["/legacy/repository"] != "未提交草稿" {
				t.Fatalf("旧设置或草稿丢失: %+v", app.prefs)
			}
			for name, content := range files {
				data, err := os.ReadFile(filepath.Join(directory, name))
				if err != nil || string(data) != content {
					t.Fatalf("改名覆盖了旧文件 %s: %v", name, err)
				}
			}
		})
	}
}

func TestRenamedApplicationKeepsIsolatedDataOverride(t *testing.T) {
	application := mygo.App
	previousName := application.Name()
	t.Cleanup(func() {
		application.SetName(previousName)
		application.SetPath(mygo.PathUserData, "")
	})
	application.SetName("51Code Dev")
	directory := t.TempDir()
	application.SetPath(mygo.PathUserData, directory)
	if err := preserveLegacyApplicationIdentity(application); err != nil {
		t.Fatal(err)
	}
	application.SetName("51Code Dev")
	actual, err := application.Path(mygo.PathUserData)
	if err != nil || actual != directory {
		t.Fatalf("自动化数据隔离被覆盖: %q, %v", actual, err)
	}
}

func TestRenamedApplicationRejectsUnavailableLegacyDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	writeNativeFile(t, config, "XSourceTree", "not a directory")
	application := mygo.App
	previousName := application.Name()
	t.Cleanup(func() {
		application.SetName(previousName)
		application.SetPath(mygo.PathUserData, "")
	})
	application.SetName("51Code")
	if err := preserveLegacyApplicationIdentity(application); err == nil {
		t.Fatal("旧目录不可用时不应静默切换到空配置")
	}
}
