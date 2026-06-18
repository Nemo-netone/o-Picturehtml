//  架构测试
package architecture_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/SATA260/SimulSpeak1"

func TestPBXDoesNotDependOnAPIStoreOrLLM(t *testing.T) {
	assertNoImports(t, []string{"internal/pbx", "cmd/pbx-node"}, []string{
		modulePath + "/internal/api-server",
		modulePath + "/internal/store",
		modulePath + "/internal/ai/llm",
	})
}

func TestAPIServerDoesNotDependOnPBXInternals(t *testing.T) {
	assertNoImports(t, []string{"internal/api-server", "cmd/api-server"}, []string{
		modulePath + "/internal/pbx",
	})
}

func TestProtocolPackagesDoNotDependOnPBXInternals(t *testing.T) {
	assertNoImports(t, []string{"internal/protocol"}, []string{
		modulePath + "/internal/pbx",
	})
}

func assertNoImports(t *testing.T, roots []string, forbidden []string) {
	t.Helper()
	repo := repoRoot(t)
	for _, root := range roots {
		absRoot := filepath.Join(repo, root)
		err := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content := string(data)
			for _, target := range forbidden {
				if strings.Contains(content, `"`+target) || strings.Contains(content, "`"+target) {
					rel, _ := filepath.Rel(repo, path)
					t.Fatalf("%s imports forbidden package prefix %s", rel, target)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

