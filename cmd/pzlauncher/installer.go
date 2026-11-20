package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/update"
)

type InstallFunc func(ctx context.Context, packDir string, dryRun bool, log func(string)) (summary string, err error)

var runInstall InstallFunc = realInstall

func realInstall(ctx context.Context, packDir string, dryRun bool, log func(string)) (string, error) {
	r, err := install.Run(ctx, install.Options{
		PackDir:          packDir,
		DryRun:           dryRun,
		Log:              log,
		ConfirmApprovals: confirmApprovals,
		BackupRoot:       installDataDir("install-backups"),
		LogDir:           installDataDir("install-logs"),
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

func installDataDir(sub string) string {
	d, err := update.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, sub)
}

var confirmApprovals func([]install.Approval) bool

func (u *ui) askApprovals(list []install.Approval) bool {
	ch := make(chan bool, 1)
	fyne.Do(func() {
		var b strings.Builder
		b.WriteString("The pack wants to pre-approve these Java mod jars in ZombieBuddy, so ZombieBuddy doesn't ask about them at the next game start:\n\n")
		for _, a := range list {
			h := a.JarHash
			if len(h) > 16 {
				h = h[:16] + "..."
			}
			fmt.Fprintf(&b, "%s  (jar %s)\n", a.ID, h)
		}
		b.WriteString("\nJava mods run with full access to your PC. Approve only if you trust whoever sent you this pack.")
		lbl := widget.NewLabel(b.String())
		lbl.Wrapping = fyne.TextWrapWord
		sc := container.NewVScroll(lbl)
		sc.SetMinSize(fyne.NewSize(560, 260))
		dialog.ShowCustomConfirm("Approve Java mods?", "Approve", "Skip", sc, func(ok bool) { ch <- ok }, u.win)
	})
	return <-ch
}
