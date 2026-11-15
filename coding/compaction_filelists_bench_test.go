package coding

import (
	"fmt"
	"testing"

	"github.com/dat267/pier/ai"
)

// longSessionFileOps builds the file-tracking state of a long session: 5,000
// distinct paths, a third of them edited. D473: ComputeFileLists sorted these
// with an insertion sort, 78 ms per compaction finalize against the
// reference's 2 ms Array.prototype.sort.
func longSessionFileOps() *FileOperations {
	ops := CreateFileOps()
	for i := 0; i < 5000; i++ {
		ops.markRead(fmt.Sprintf("/tmp/dir%02d/file-%05d.txt", i%50, i))
	}
	for i := 0; i < 5000; i += 3 {
		ops.markEdited(fmt.Sprintf("/tmp/dir%02d/file-%05d.txt", i%50, i))
	}
	return ops
}

// BenchmarkCapFileListsLongSession measures the compaction summary's file-list
// finalize on a long session, the workload D379 caps and D473 sorted.
func BenchmarkCapFileListsLongSession(b *testing.B) {
	ops := longSessionFileOps()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reads, modifieds, _, _ := CapFileLists(ops)
		if len(reads) != 100 || len(modifieds) != 100 {
			b.Fatalf("capped lists = %d/%d, want 100/100", len(reads), len(modifieds))
		}
	}
}

var _ = ai.Usage{}
