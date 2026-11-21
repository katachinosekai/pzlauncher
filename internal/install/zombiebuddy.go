package install

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/katachinosekai/pzlauncher/internal/modlist"
	"github.com/katachinosekai/pzlauncher/internal/zbsetup"
)

type zbState struct {
	planned        bool
	steamToRestart bool
	jarChanged     bool
}

func SteamRunning() bool { return DefaultSystem().Running(steamProcess) }

func ZombieBuddy(ctx context.Context, opts Options) (Report, error) {
	in, err := New(opts)
	if err != nil {
		return Report{Failed: true}, err
	}
	for _, s := range []func(context.Context) error{in.Preflight, in.Locate, in.ZombieBuddy} {
		if err = s(ctx); err != nil {
			break
		}
	}
	return in.Finish(err), err
}

func (in *Installer) restartSteamIfClosed() {
	if !in.zb.steamToRestart {
		return
	}
	in.zb.steamToRestart = false
	if err := in.sys.StartSteam(in.steamRoot); err == nil {
		in.say("Steam started again")
	} else {
		in.say("could not start Steam again; start it yourself")
	}
}

func (in *Installer) zbLibs() string {
	idx := modlist.Scan(modlist.Roots{ZomboidDir: in.zdir, WorkshopDir: in.workshop}, modlist.Version{})
	if m := idx.Get(zbsetup.ModID); m != nil {
		if libs := zbsetup.LibsFromModDir(m.Dir); isFile(filepath.Join(libs, zbsetup.JarName)) {
			return libs
		}
	}
	if libs := zbsetup.WorkshopLibs(in.workshop); isFile(filepath.Join(libs, zbsetup.JarName)) {
		return libs
	}
	return ""
}

func (in *Installer) zbConfigs() []zbsetup.ConfigFile {
	var out []zbsetup.ConfigFile
	for _, f := range in.launchFiles() {
		t, err := readText(f)
		if err != nil {
			in.warn("could not read " + f + ": " + err.Error())
			continue
		}
		out = append(out, zbsetup.ConfigFile{Path: f, Text: t})
	}
	return out
}

func (in *Installer) zbPlan() zbsetup.Plan {
	configs := in.zbConfigs()
	if in.opts.SetupZombieBuddy {
		p := zbsetup.SetupPlan(in.opts.GOOS, in.gameDir, in.zbLibs(), configs)
		zbsetup.ApplyCustomJar(&p, in.gameDir, in.opts.ZombieBuddyJar)
		if in.opts.ZombieBuddyMalloc && in.opts.GOOS != "windows" {
			zbsetup.AddMallocToPlan(&p, configs)
		}
		return p
	}
	return zbsetup.RemovePlan(in.gameDir, configs, in.opts.RemoveZombieBuddyFiles)
}

func (in *Installer) ZombieBuddy(ctx context.Context) error {
	if !in.opts.SetupZombieBuddy && !in.opts.RemoveZombieBuddy {
		return nil
	}
	if in.opts.SetupZombieBuddy {
		in.say("ZombieBuddy (needed for Java mods) -> " + in.gameDir)
	} else {
		in.say("ZombieBuddy: switching it off (only its launch options are removed)")
	}
	plan := in.zbPlan()
	for _, w := range plan.Warnings {
		in.warn(w)
	}
	if plan.Empty() {
		if in.opts.SetupZombieBuddy {
			in.say("  already set up")
		} else {
			in.say("  nothing to remove")
		}
		in.say("")
		return nil
	}
	for _, c := range plan.Copies {
		if err := in.zbCopy(c); err != nil {
			return err
		}
	}
	for _, d := range plan.Deletes {
		if err := in.backupFile(filepath.Base(d), in.gameDir, "ProjectZomboid"); err != nil {
			return err
		}
		if err := in.removeAll(d); err != nil {
			return err
		}
		in.change("  deleted " + d + " (backed up)")
		if filepath.Base(d) == zbsetup.JarName {
			in.zb.jarChanged = true
		}
	}
	if len(plan.Configs) > 0 {
		if err := in.zbLaunchOptions(ctx); err != nil {
			return err
		}
	}
	if in.opts.RemoveZombieBuddy && !in.opts.SetupZombieBuddy && in.opts.GOOS != "windows" {
		if err := in.zbRemoveZBBatchJSON(); err != nil {
			return err
		}
	}
	if in.zb.jarChanged {
		in.say("  " + zbsetup.ReapproveNote)
	}
	in.say("")
	return nil
}

func (in *Installer) zbCopy(c zbsetup.FileCopy) error {
	if c.Name == zbsetup.JarName && c.Replace {
		if in.opts.DryRun {
			in.change("  ZombieBuddy.jar: " + c.FromVersion + " -> " + c.ToVersion + " (from " + c.Src + ")")
			return nil
		}
		r, err := zbsetup.ReplaceJar(zbsetup.JarEnv{GameDir: in.gameDir, GOOS: in.opts.GOOS, Running: func() bool { return in.sys.Running(gameProcesses...) }}, c.Src)
		if err != nil {
			return err
		}
		in.change(fmt.Sprintf("  ZombieBuddy.jar: %s -> %s (old jar kept as %s)", r.OldVersion, r.NewVersion, r.Backup))
		in.zb.jarChanged = true
		return nil
	}
	if c.Replace {
		if err := in.backupFile(c.Name, in.gameDir, "ProjectZomboid"); err != nil {
			return err
		}
	}
	if err := in.copyFile(c.Src, c.Dst); err != nil {
		return err
	}
	in.change("  " + c.String())
	if c.Name == zbsetup.JarName {
		in.zb.jarChanged = true
	}
	return nil
}

func (in *Installer) zbLaunchOptions(ctx context.Context) error {
	return in.writeLaunchOptions(ctx, func() []zbsetup.ConfigEdit {
		configs := in.zbPlan().Configs
		if len(configs) > 0 && in.opts.SetupZombieBuddy && in.opts.DryRun {
			in.zb.planned = true
		}
		return configs
	})
}

// MallocArena adds or removes MALLOC_ARENA_MAX=2 in Steam's launch options
// for Project Zomboid (Settings > Java options). Steam is closed for the edit
// and started again afterwards.
func MallocArena(ctx context.Context, opts Options) (Report, error) {
	in, err := New(opts)
	if err != nil {
		return Report{Failed: true}, err
	}
	for _, s := range []func(context.Context) error{in.Preflight, in.Locate, in.Malloc} {
		if err = s(ctx); err != nil {
			break
		}
	}
	return in.Finish(err), err
}

func (in *Installer) Malloc(ctx context.Context) error {
	on := in.opts.SetMallocArena
	if !on && !in.opts.RemoveMallocArena {
		return nil
	}
	if in.opts.GOOS == "windows" {
		in.say("MALLOC_ARENA_MAX only works on Linux; skipped")
		return nil
	}
	if on {
		in.say("Limit memory pools: adding " + zbsetup.MallocToken + " to Steam's launch options")
	} else {
		in.say("Limit memory pools: removing " + zbsetup.MallocToken + " from Steam's launch options")
	}
	plan := zbsetup.MallocPlan(in.zbConfigs(), on)
	for _, w := range plan.Warnings {
		in.warn(w)
	}
	if len(plan.Configs) == 0 {
		in.say("  already as chosen")
		in.say("")
		return nil
	}
	err := in.writeLaunchOptions(ctx, func() []zbsetup.ConfigEdit {
		return zbsetup.MallocPlan(in.zbConfigs(), on).Configs
	})
	in.say("")
	return err
}

// writeLaunchOptions closes Steam if it runs (Steam rewrites localconfig.vdf
// when it exits), then plans the edits from the files as they are now, backs
// each file up and writes it. Steam is started again in Finish.
func (in *Installer) writeLaunchOptions(ctx context.Context, plan func() []zbsetup.ConfigEdit) error {
	touchSteam := !in.opts.NoWait && !in.opts.DryRun
	if touchSteam && in.steamRunning() {
		in.say("  closing Steam to change the launch options (it is started again afterwards)...")
		closed, err := in.closeSteam(ctx, 45)
		if err != nil {
			return err
		}
		if !closed {
			in.warn("Steam did not close, so the launch options were not changed. " + in.exitSteamHint() + " and try again.")
			return nil
		}
		in.zb.steamToRestart = true
	}
	root := filepath.Clean(in.steamRoot)
	for _, c := range plan() {
		for _, e := range c.Edits {
			before := e.Before
			if e.Inserted {
				before = "(none)"
			}
			in.change(fmt.Sprintf("  Steam launch options (%s):\n    before: %s\n    after:  %s", c.Path, before, e.After))
		}
		if in.opts.DryRun {
			continue
		}
		rel, err := filepath.Rel(root, c.Path)
		if err != nil {
			return err
		}
		if err := in.backupFile(rel, root, "Steam"); err != nil {
			return err
		}
		if err := in.writeFile(c.Path, []byte(c.Text)); err != nil {
			return err
		}
	}
	return nil
}

func (in *Installer) zbRemoveZBBatchJSON() error {
	t, err := readText(in.memoryJSON())
	if err != nil || !HasZBBatchAgent(t) {
		return nil
	}
	nt := RemoveZBBatchAgent(t)
	if !jsonValid(nt) {
		in.warn("could not remove -javaagent:ZBBatch.jar from ProjectZomboid64.json; remove it by hand, ZBBatch needs ZombieBuddy")
		return nil
	}
	if err := in.writeGameJSON(nt); err != nil {
		return err
	}
	in.change("  removed -javaagent:ZBBatch.jar from ProjectZomboid64.json (ZBBatch needs ZombieBuddy)")
	return nil
}
