//go:build !windows

package diag

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func totalRAM() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "MemTotal:"); ok {
			fs := strings.Fields(v)
			if len(fs) > 0 {
				kb, err := strconv.ParseUint(fs[0], 10, 64)
				if err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 0
}
