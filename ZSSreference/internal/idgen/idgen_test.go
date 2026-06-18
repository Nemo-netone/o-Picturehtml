//  ID生成器：雪花算法+节点ID+通话ID
package idgen_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/idgen"
)

// 作用: 验证 Test I D Gen_ Unique Concurrent 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestIDGen_UniqueConcurrent(t *testing.T) {
	generator := idgen.NewGenerator(idgen.PrefixCall)
	const total = 1000

	seen := make(map[string]struct{}, total)
	var seenMu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < total; i++ {
		wg.Add(1)
		// 作用: 并发生成一个 ID 并校验唯一性。
		go func() {
			defer wg.Done()
			id := generator.Next()
			if !strings.HasPrefix(id, string(idgen.PrefixCall)+"-") {
				t.Errorf("unexpected id prefix: %s", id)
			}
			seenMu.Lock()
			defer seenMu.Unlock()
			if _, ok := seen[id]; ok {
				t.Errorf("duplicate id: %s", id)
			}
			seen[id] = struct{}{}
		}()
	}

	wg.Wait()

	if len(seen) != total {
		t.Fatalf("expected %d unique ids, got %d", total, len(seen))
	}
}

