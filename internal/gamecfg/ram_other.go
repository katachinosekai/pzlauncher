//go:build !linux && !windows

package gamecfg

func TotalRAMMB() int { return 0 }

func FreeRAMMB() int { return 0 }
