package install

import "context"

type System interface {
	Running(names ...string) bool
	OpenURL(url string) error
	CloseSteam(ctx context.Context, steamRoot string) error
	StartSteam(steamRoot string) error
}

func DefaultSystem() System { return osSystem{} }

var gameProcesses = []string{"ProjectZomboid64", "ProjectZomboid32"}

const steamProcess = "steam"
