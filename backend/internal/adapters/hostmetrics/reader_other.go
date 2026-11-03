//go:build !linux

package hostmetrics

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"runtime"
	"time"
)

type Reader struct{}

func New() *Reader { return &Reader{} }
func (*Reader) Read() platform.SystemSample {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return platform.SystemSample{At: time.Now().UTC(), CPUCount: runtime.NumCPU(), Heap: mem.HeapAlloc, Goroutines: runtime.NumGoroutine(), Issues: []string{"当前平台未提供主机指标"}}
}
