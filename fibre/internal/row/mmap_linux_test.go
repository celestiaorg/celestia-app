package row

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"unsafe"
)

// TestMmapAllocHugePageEligible checks that mmap'd slabs are advised for
// transparent huge pages.
func TestMmapAllocHugePageEligible(t *testing.T) {
	mode, err := os.ReadFile("/sys/kernel/mm/transparent_hugepage/enabled")
	if err != nil || strings.Contains(string(mode), "[never]") {
		t.Skip("transparent huge pages unavailable")
	}
	data, err := mmapAlloc(8 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(data)

	f, err := os.Open("/proc/self/smaps")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	addr := uintptr(unsafe.Pointer(&data[0]))
	inRegion := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		var lo, hi uintptr
		if n, _ := fmt.Sscanf(line, "%x-%x ", &lo, &hi); n == 2 {
			inRegion = lo <= addr && addr < hi
			continue
		}
		if inRegion && strings.HasPrefix(line, "THPeligible:") {
			if strings.TrimSpace(strings.TrimPrefix(line, "THPeligible:")) != "1" {
				t.Fatalf("slab not huge-page eligible: %q", line)
			}
			return
		}
	}
	t.Skip("THPeligible not reported")
}
