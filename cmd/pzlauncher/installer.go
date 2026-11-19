package main

import (
	"context"
	"fmt"

	"github.com/kata/pzlauncher/internal/install"
)

type InstallFunc func(ctx context.Context, packDir string, dryRun bool, log func(string)) (summary string, err error)

var runInstall InstallFunc = realInstall

func realInstall(ctx context.Context, packDir string, dryRun bool, log func(string)) (string, error) {
	r, err := install.Run(ctx, install.Options{
		PackDir: packDir,
		DryRun:  dryRun,
		Log:     log,
		OnWait: func(w install.WaitStatus) {
			if !w.Done {
				log(fmt.Sprintf("waiting for %s: %d of %d installed", w.What, w.Installed, w.Total))
			}
		},
	})
	summary := fmt.Sprintf("%d changes, %d warnings", len(r.Changes), len(r.Warnings))
	if r.BackupDir != "" {
		summary += ", backup in " + r.BackupDir
	}
	if r.LogFile != "" {
		summary += ", log " + r.LogFile
	}
	return summary, err
}
