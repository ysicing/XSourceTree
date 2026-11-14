// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/internal/workspace"

	"github.com/egoist/mygo/ui"
)

func BenchmarkNativeChangedFilesFrame(benchmark *testing.B) {
	for _, count := range []int{100, 5000} {
		benchmark.Run(fmt.Sprint(count), func(benchmark *testing.B) {
			manager, err := workspace.New(filepath.Join(benchmark.TempDir(), "recent.json"))
			if err != nil {
				benchmark.Fatal(err)
			}
			app := newNativeApp(&RepositoryService{manager: manager}, "")
			benchmark.Cleanup(app.cancel)
			app.screen, app.writable = "changes", true
			app.repository = &repositoryView{snapshot: gitrepo.Snapshot{
				Path: "/benchmark", Name: "Benchmark", Branch: "main", Head: "1234567890123456789012345678901234567890",
			}}
			for index := 0; index < count; index++ {
				app.repository.snapshot.Files = append(app.repository.snapshot.Files, gitrepo.File{
					Path: fmt.Sprintf("packages/module-%03d/src/file-%05d.go", index%100, index), Index: " ", Working: "M",
				})
			}
			tester := ui.NewTester(app.view, 1320, 860)
			benchmark.ReportAllocs()
			benchmark.ResetTimer()
			for benchmark.Loop() {
				tester.Frame()
			}
		})
	}
}
