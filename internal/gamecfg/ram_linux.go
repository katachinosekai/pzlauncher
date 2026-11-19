package gamecfg

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func TotalRAMMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	return parseMeminfo(bufio.NewScanner(f))
}

func parseMeminfo(sc *bufio.Scanner) int {
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == "MemTotal:" {
			kb, err := strconv.Atoi(fs[1])
			if err != nil {
				return 0
			}
			return kb / 1024
		}
	}
	return 0
}
