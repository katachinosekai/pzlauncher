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

func FreeRAMMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	return parseMeminfoKey(bufio.NewScanner(f), "MemAvailable:")
}

func parseMeminfo(sc *bufio.Scanner) int { return parseMeminfoKey(sc, "MemTotal:") }

func parseMeminfoKey(sc *bufio.Scanner, key string) int {
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == key {
			kb, err := strconv.Atoi(fs[1])
			if err != nil {
				return 0
			}
			return kb / 1024
		}
	}
	return 0
}
