package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kata/pzlauncher/internal/install"
)

type cliFlags struct {
	installPack *string
	steamRoot   *string
	zomboidDir  *string
	zbHome      *string
	outFile     *string
	dryRun      *bool
	noWait      *bool
}

func registerCLI() cliFlags {
	return cliFlags{
		installPack: flag.String("install", "", "install from an extracted pack folder without opening the window"),
		steamRoot:   flag.String("steam-root", "", "Steam folder to install into (default: detected)"),
		zomboidDir:  flag.String("zomboid-dir", "", "Zomboid user folder (default: detected)"),
		zbHome:      flag.String("zombiebuddy-home", "", "ZombieBuddy settings folder (default: ~/.zombie_buddy)"),
		outFile:     flag.String("out", "", "also write the install log to this file"),
		dryRun:      flag.Bool("dry-run", false, "show what the install would change without changing anything"),
		noWait:      flag.Bool("no-wait", false, "don't wait for Workshop downloads"),
	}
}

func runCLI(f cliFlags) int {
	outs := []io.Writer{os.Stdout}
	if *f.outFile != "" {
		file, err := os.Create(*f.outFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer file.Close()
		outs = append([]io.Writer{file}, outs...)
	}
	log := func(s string) {
		for _, w := range outs {
			fmt.Fprintln(w, s)
		}
	}
	r, err := install.Run(context.Background(), install.Options{
		PackDir:         *f.installPack,
		SteamRoot:       *f.steamRoot,
		ZomboidDir:      *f.zomboidDir,
		ZombieBuddyHome: *f.zbHome,
		DryRun:          *f.dryRun,
		NoWait:          *f.noWait,
		Log:             log,
	})
	log(fmt.Sprintf("result: %d changes, %d warnings, failed=%v", len(r.Changes), len(r.Warnings), r.Failed))
	if err != nil {
		log("error: " + err.Error())
		return 1
	}
	return 0
}
