package gamecfg

import "github.com/kata/pzlauncher/internal/install"

var gameNames = []string{"ProjectZomboid64", "ProjectZomboid32"}

func GameRunning() bool { return install.DefaultSystem().Running(gameNames...) }
