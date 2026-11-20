package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/katachinosekai/pzlauncher/internal/paths"
)

func (in *Installer) Preflight(ctx context.Context) error {
	if in.sys.Running(gameProcesses...) {
		return fatal("Project Zomboid is running; quit it first")
	}
	return nil
}

func (in *Installer) Locate(ctx context.Context) error {
	root := in.opts.SteamRoot
	if root == "" {
		r, err := paths.SteamRoot()
		if err != nil {
			return fatal("could not find the Steam folder; set the Steam folder by hand")
		}
		root = r
	}
	in.steamRoot = root
	in.say("Steam folder: " + root)
	for _, lib := range paths.SteamLibraries(root) {
		in.say("  Steam library: " + lib)
		if isFile(filepath.Join(lib, "steamapps", "appmanifest_"+AppID+".acf")) {
			in.library = lib
			break
		}
	}
	if in.library == "" {
		return fatal("could not find Project Zomboid in any Steam library (set the Steam folder by hand)")
	}
	ws, err := paths.WorkshopDir(root)
	if err != nil {
		return fatal("%v", err)
	}
	in.workshop = ws
	gd, err := paths.GameDir(root)
	if err != nil {
		return fatal("%v", err)
	}
	in.gameDir = gd
	if !isDir(in.workshop) {
		in.say("Workshop folder not there yet (no mods downloaded): " + in.workshop)
	}
	in.zdir = in.opts.ZomboidDir
	if in.zdir == "" {
		z, err := paths.Zomboid()
		if err != nil {
			return fatal("%v", err)
		}
		in.zdir = z
	}
	in.zbHome = in.opts.ZombieBuddyHome
	if in.zbHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fatal("%v", err)
		}
		in.zbHome = filepath.Join(home, ".zombie_buddy")
	}
	in.say("Project Zomboid library: " + in.library)
	in.say("Game folder:             " + in.gameDir)
	in.say("Workshop folder:         " + in.workshop)
	in.say("Zomboid folder:          " + in.zdir)
	in.say("")
	return nil
}

func (in *Installer) vpPaths() (dir, info, jar string) {
	dir = filepath.Join(in.workshop, ViewpointID)
	info = filepath.Join(dir, "mods", "Viewpoint", "common", "mod.info")
	jar = filepath.Join(dir, "mods", "Viewpoint", "42", "media", "java", "client", "Viewpoint.jar")
	return
}

func (in *Installer) vpNightly() string {
	_, info, jar := in.vpPaths()
	if t, err := readText(info); err == nil {
		if v, found := NightlyFromModInfo(t); found {
			return v
		}
	}
	if isFile(jar) && strings.EqualFold(sha256File(jar), VpNightlySha) {
		return "nightly jar"
	}
	return ""
}

func (in *Installer) Viewpoint(ctx context.Context) error {
	v := in.vpNightly()
	if v == "" {
		return nil
	}
	dir, _, jar := in.vpPaths()
	in.say("Viewpoint: nightly build found in the Workshop folder (" + v + "); replacing it with the Workshop release")
	if in.opts.DryRun {
		in.say("  (dry run: Steam not closed, nothing removed)")
		in.say("")
		return nil
	}
	if in.steamRunning() {
		in.say("  closing Steam...")
		closed, err := in.closeSteam(ctx, 30)
		if err != nil {
			return err
		}
		if !closed {
			return fatal("Steam did not close. %s and run the installer again.", in.exitSteamHint())
		}
	}
	acf := filepath.Join(in.library, "steamapps", "workshop", "appworkshop_"+AppID+".acf")
	if isFile(acf) {
		if err := copyFileRaw(acf, acf+".pzfix-"+in.opts.Now().Format("20060102-150405")); err != nil {
			return err
		}
		t, err := readText(acf)
		if err != nil {
			return err
		}
		nt, n := RemoveAcfItem(t, ViewpointID)
		if err := in.writeFile(acf, []byte(nt)); err != nil {
			return err
		}
		in.change(fmt.Sprintf("  removed Viewpoint from Steam's Workshop list (%d entries)", n))
	}
	if err := in.removeAll(dir); err != nil {
		return err
	}
	in.change("  removed the nightly Viewpoint folder")
	in.say("  starting Steam; it downloads Viewpoint again (keep Steam running, do NOT start Project Zomboid)")
	if err := in.sys.OpenURL("steam://url/CommunityFilePage/" + ViewpointID); err != nil {
		in.say("  (could not start Steam; start it yourself)")
	}
	if in.opts.NoWait {
		in.say("  (not waiting)")
		in.say("")
		return nil
	}
	in.say("Waiting until Viewpoint is downloaded.")
	last := time.Now()
	for {
		if isFile(jar) && in.vpNightly() == "" {
			break
		}
		in.notify(WaitStatus{What: "viewpoint", Total: 1, Missing: []string{ViewpointID}})
		if time.Since(last) >= time.Minute {
			in.say("  still waiting for Viewpoint...")
			last = time.Now()
		}
		if err := in.sleep(ctx, in.opts.PollInterval); err != nil {
			return err
		}
	}
	in.notify(WaitStatus{What: "viewpoint", Installed: 1, Total: 1, Done: true})
	in.say("  Viewpoint release downloaded")
	in.say("")
	return nil
}

func first(ids []string, n int) []string {
	if len(ids) > n {
		return ids[:n]
	}
	return ids
}

func (in *Installer) Collection(ctx context.Context) error {
	in.say("Workshop collection: checking collection " + CollectionID + "...")
	items, err := CollectionItems(ctx, in.opts.HTTPClient, in.opts.CollectionAPI, CollectionID)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		in.warn("could not read the collection; continuing with what is installed")
		items = nil
	}
	if len(items) > 0 {
		missing := MissingItems(in.workshop, items)
		total := len(items)
		inst := total - len(missing)
		in.say(fmt.Sprintf("Workshop collection: %d items, %d installed, %d missing", total, inst, len(missing)))
		if len(missing) == 0 {
			in.say(fmt.Sprintf("Workshop items downloaded: %d / %d - all installed, continuing", inst, total))
			in.notify(WaitStatus{What: "collection", Installed: inst, Total: total, Done: true})
		} else if in.opts.NoWait || in.opts.DryRun {
			in.say("  missing (first few): " + strings.Join(first(missing, 8), " "))
			in.say("  (not opening Steam, not waiting)")
			in.notify(WaitStatus{What: "collection", Installed: inst, Total: total, Missing: missing})
		} else if err := in.WaitCollection(ctx, items); err != nil {
			return err
		}
	}
	in.say("")
	if !isDir(in.workshop) {
		return fatal("no Workshop folder at %s (subscribe to the collection in Steam first: https://steamcommunity.com/sharedfiles/filedetails/?id=%s)", in.workshop, CollectionID)
	}
	return nil
}

func (in *Installer) WaitCollection(ctx context.Context, items []string) error {
	steamURL := "steam://url/CommunityFilePage/" + CollectionID
	webURL := "https://steamcommunity.com/sharedfiles/filedetails/?id=" + CollectionID
	total := len(items)
	if !in.steamRunning() {
		in.say("  Steam is not running; starting it. If the collection page does not appear, open the link below.")
	}
	in.say("Opening the collection in Steam: " + steamURL)
	if err := in.sys.OpenURL(steamURL); err != nil {
		in.say("  (could not open Steam)")
	}
	in.say("  Collection link: " + webURL)
	in.say("In Steam, click 'Subscribe to all' on the collection page. Keep Steam running, do NOT start Project Zomboid.")
	in.say("Waiting until everything is downloaded.")
	in.say("")
	lastCount := -1
	var lastList time.Time
	for {
		missing := MissingItems(in.workshop, items)
		inst := total - len(missing)
		if len(missing) == 0 {
			in.say(fmt.Sprintf("Workshop items downloaded: %d / %d", inst, total))
			in.notify(WaitStatus{What: "collection", Installed: inst, Total: total, Done: true})
			return nil
		}
		if inst != lastCount {
			in.say(fmt.Sprintf("Workshop items downloaded: %d / %d", inst, total))
			lastCount = inst
			lastList = time.Time{}
		}
		if time.Since(lastList) >= 2*time.Minute {
			in.say("  still missing (first few): " + strings.Join(first(missing, 8), " "))
			lastList = time.Now()
		}
		in.notify(WaitStatus{What: "collection", Installed: inst, Total: total, Missing: missing})
		if err := in.sleep(ctx, in.opts.PollInterval); err != nil {
			return err
		}
	}
}

func (in *Installer) OldMods(ctx context.Context) error {
	zmods := filepath.Join(in.zdir, "mods")
	in.say("Old local copies of our mods (they now come from the Workshop collection) -> " + zmods)
	removed := 0
	for _, id := range ModIDs {
		d := filepath.Join(zmods, id)
		if !isDir(d) {
			continue
		}
		if err := in.backupTree(d, zmods, "Zomboid-mods"); err != nil {
			return err
		}
		if err := in.removeAll(d); err != nil {
			return err
		}
		in.change("  " + id + " : removed (backed up); the Workshop copy is used instead")
		removed++
	}
	if removed == 0 {
		in.say("  none found")
	}
	in.say("")
	return nil
}

func (in *Installer) Settings(ctx context.Context) error {
	src := filepath.Join(in.opts.PackDir, "settings")
	if !isDir(src) {
		return nil
	}
	in.say("Controls and mod settings -> " + in.zdir)
	files, err := listFiles(src)
	if err != nil {
		return err
	}
	copied, same := 0, 0
	for _, rel := range files {
		dst := filepath.Join(in.zdir, rel)
		if isFile(dst) && (sameFile(filepath.Join(src, rel), dst) || in.sameAfterModList(filepath.Join(src, rel), dst, rel)) {
			same++
			continue
		}
		if isFile(dst) {
			if err := in.backupFile(rel, in.zdir, "Zomboid"); err != nil {
				return err
			}
		}
		if err := in.copyFile(filepath.Join(src, rel), dst); err != nil {
			return err
		}
		copied++
	}
	msg := fmt.Sprintf("  %d file(s) copied, %d already up to date", copied, same)
	if copied > 0 {
		in.change(msg)
	} else {
		in.say(msg)
	}
	in.say("")
	return nil
}

func (in *Installer) Presets(ctx context.Context) error {
	src := filepath.Join(in.opts.PackDir, "presets")
	if !isDir(src) {
		return nil
	}
	dstDir := filepath.Join(in.zdir, "Sandbox Presets")
	in.say("World presets -> " + dstDir)
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		dst := filepath.Join(dstDir, e.Name())
		if isFile(dst) {
			in.say("  " + e.Name() + ": already in Sandbox Presets, left as is")
			continue
		}
		if err := in.copyFile(filepath.Join(src, e.Name()), dst); err != nil {
			return err
		}
		in.change("  " + e.Name() + ": added")
	}
	in.say("")
	return nil
}

func (in *Installer) localModSources() []string {
	var out []string
	for _, d := range []string{filepath.Join(in.opts.PackDir, "payload", "localmods"), filepath.Join(in.opts.PackDir, "localmods")} {
		if isDir(d) {
			out = append(out, d)
		}
	}
	return out
}

func (in *Installer) LocalMods(ctx context.Context) error {
	srcs := in.localModSources()
	if len(srcs) == 0 {
		return nil
	}
	zmods := filepath.Join(in.zdir, "mods")
	in.say("Local mods (not on the Workshop) -> " + zmods)
	for _, src := range srcs {
		ents, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			id := e.Name()
			from := filepath.Join(src, id)
			dst := filepath.Join(zmods, id)
			if f := testTrigger(from); f != "" {
				in.warn("local mod " + id + " skipped: " + f + " contains the test-only AutoLua trigger, which must never ship to players")
				continue
			}
			if sameTree(from, dst) {
				in.say("  " + id + ": up to date")
				continue
			}
			if isDir(dst) {
				if err := in.backupTree(dst, zmods, "Zomboid-mods"); err != nil {
					return err
				}
				if err := in.removeAll(dst); err != nil {
					return err
				}
				in.change("  " + id + ": updated (old copy backed up)")
			} else {
				in.change("  " + id + ": installed")
			}
			if err := in.copyTree(from, dst); err != nil {
				return err
			}
		}
	}
	in.say("")
	return nil
}

func (in *Installer) localModEnableIDs() []string {
	var ids []string
	for _, src := range in.localModSources() {
		t, err := readText(filepath.Join(src, "enable.txt"))
		if err != nil {
			continue
		}
		lines, _ := splitLines(t)
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "#") {
				ids = append(ids, l)
			}
		}
	}
	return ids
}

func (in *Installer) VersionMax(ctx context.Context) error {
	in.say("Mods marked for 42.21 that work on 42.22 (3D Construction):")
	for _, rel := range VersionMaxFiles {
		rel = filepath.FromSlash(rel)
		p := filepath.Join(in.workshop, rel)
		if !isFile(p) {
			in.say("  " + rel + ": not installed")
			continue
		}
		t, err := readText(p)
		if err != nil {
			return err
		}
		nt, changed := BumpVersionMax(t)
		if !changed {
			in.say("  " + rel + ": already allowed on 42.22")
			continue
		}
		if err := in.backupFile(rel, in.workshop, ""); err != nil {
			return err
		}
		if err := in.writeFile(p, []byte(nt)); err != nil {
			return err
		}
		in.change("  " + rel + ": versionMax 42.21 -> 42.22")
	}
	in.say("")
	return nil
}

func (in *Installer) Approvals(ctx context.Context) error {
	src := filepath.Join(in.opts.PackDir, "payload", "approvals.txt")
	if !isFile(src) {
		return nil
	}
	file := filepath.Join(in.zbHome, "mod_approvals.json")
	in.say("ZombieBuddy approvals for our Java mods -> " + file)
	t, err := readText(src)
	if err != nil {
		return err
	}
	var existing []byte
	if isFile(file) {
		existing, err = os.ReadFile(file)
		if err != nil {
			return err
		}
	}
	want := ParseApprovals(t)
	out, added, err := MergeApprovals(existing, want)
	if err != nil {
		return err
	}
	if added == 0 {
		in.say("  already approved")
		in.say("")
		return nil
	}
	if in.opts.ConfirmApprovals != nil && !in.opts.DryRun {
		var fresh []Approval
		for _, a := range want {
			if _, n, err := MergeApprovals(existing, []Approval{a}); err == nil && n > 0 {
				fresh = append(fresh, a)
			}
		}
		if !in.opts.ConfirmApprovals(fresh) {
			in.warn("ZombieBuddy approvals not added (you declined); ZombieBuddy asks about these jars at the next game start")
			in.say("")
			return nil
		}
	}
	if existing != nil {
		if err := in.backupFile("mod_approvals.json", in.zbHome, ".zombie_buddy"); err != nil {
			return err
		}
	}
	if err := in.writeFile(file, out); err != nil {
		return err
	}
	in.change(fmt.Sprintf("  %d approval(s) added", added))
	in.say("")
	return nil
}

func (in *Installer) memoryJSON() string { return filepath.Join(in.gameDir, "ProjectZomboid64.json") }

func (in *Installer) writeGameJSON(text string) error {
	if err := in.backupFile("ProjectZomboid64.json", in.gameDir, "ProjectZomboid"); err != nil {
		return err
	}
	return in.writeFile(in.memoryJSON(), []byte(text))
}

func (in *Installer) Memory(ctx context.Context) error {
	mj := in.memoryJSON()
	if !isFile(mj) {
		in.warn("no ProjectZomboid64.json at " + mj + "; game memory not changed")
		in.say("")
		return nil
	}
	in.say("Game memory -> " + mj)
	t, err := readText(mj)
	if err != nil {
		return err
	}
	r := SetXmx(t)
	switch {
	case r.NoArray:
		in.warn("no vmArgs array in ProjectZomboid64.json; game memory not changed")
	case !r.Changed:
		in.say("  already 6 GB")
	default:
		old := r.Old
		if old == "" {
			old = "(none)"
		}
		if err := in.writeGameJSON(r.Text); err != nil {
			return err
		}
		in.change("  " + old + " -> " + XmxTarget)
	}
	ft, added := AddJVMFlags(r.Text)
	if len(added) == 0 {
		in.say("  memory-saving Java options already on")
	} else if !jsonValid(ft) {
		in.warn("could not add the memory-saving Java options to ProjectZomboid64.json; left unchanged")
	} else {
		if err := in.writeGameJSON(ft); err != nil {
			return err
		}
		in.change("  memory-saving Java options switched on: " + strings.Join(added, " ") + " (about 3 GB less memory in play)")
	}
	in.say("")
	return nil
}

func (in *Installer) launchFiles() []string { return LaunchFiles(in.steamRoot) }

func LaunchFiles(steamRoot string) []string {
	ents, err := os.ReadDir(filepath.Join(steamRoot, "userdata"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(steamRoot, "userdata", e.Name(), "config", "localconfig.vdf")
		if isFile(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func (in *Installer) ZBBatch(ctx context.Context) error {
	agentSrc := filepath.Join(in.workshop, ZBBatchItem, "mods", "ZBBatch", "agent")
	jar := filepath.Join(agentSrc, "ZBBatch.jar")
	if !isFile(jar) || !isDir(in.gameDir) {
		in.warn("ZBBatch is not downloaded yet (Workshop item " + ZBBatchItem + " in the collection); skipped. Run the installer again once Steam has downloaded it.")
		in.say("")
		return nil
	}
	in.say("ZBBatch (faster ZombieBuddy start) -> " + in.gameDir)
	changed, err := in.copyIfChanged(jar, in.gameDir, "ZBBatch.jar", "ProjectZomboid")
	if err != nil {
		return err
	}
	if changed {
		in.change("  ZBBatch.jar: installed")
	} else {
		in.say("  ZBBatch.jar: up to date")
	}
	props := filepath.Join(agentSrc, "zbbatch.properties")
	if packProps := filepath.Join(in.opts.PackDir, "zbbatch", "zbbatch.properties"); isFile(packProps) {
		props = packProps
	}
	if isFile(props) {
		changed, err := in.copyIfChanged(props, in.zbHome, "zbbatch.properties", ".zombie_buddy")
		if err != nil {
			return err
		}
		if changed {
			in.change("  zbbatch.properties: installed")
		} else {
			in.say("  zbbatch.properties: up to date")
		}
	}
	if !isFile(in.memoryJSON()) {
		in.warn("no ProjectZomboid64.json; ZBBatch is not switched on")
		in.say("")
		return nil
	}
	if in.opts.GOOS == "windows" {
		err = in.zbbatchWindows(ctx)
	} else {
		err = in.zbbatchLinux(ctx)
	}
	in.say("")
	return err
}

func (in *Installer) zbbatchWindows(ctx context.Context) error {
	t, err := readText(in.memoryJSON())
	if err != nil {
		return err
	}
	if HasZBBatchAgent(t) {
		nt := RemoveZBBatchAgent(t)
		if jsonValid(nt) {
			if err := in.writeGameJSON(nt); err != nil {
				return err
			}
			in.change("  removed -javaagent:ZBBatch.jar from ProjectZomboid64.json (on Windows it stops the game from starting)")
		} else {
			in.warn("could not remove -javaagent:ZBBatch.jar from ProjectZomboid64.json; remove it by hand or the game will not start")
		}
	}
	files := in.launchFiles()
	pending, already := 0, 0
	for _, f := range files {
		ft, err := readText(f)
		if err != nil {
			continue
		}
		r := LaunchEditWindows(ft)
		if r.Changed > 0 {
			pending++
		}
		if r.Already > 0 {
			already++
		}
	}
	switch {
	case pending == 0 && already > 0:
		in.say("  already switched on in Steam's launch options")
		return nil
	case pending == 0 && in.zb.planned:
		in.say("  (dry run) switched on in Steam's launch options once ZombieBuddy is set up")
		return nil
	case pending == 0:
		in.warn("ZombieBuddy's Steam launch option (-agentlib:zbNative --) was not found, so ZBBatch is not switched on. Set up ZombieBuddy first, then run the installer again.")
		return nil
	}
	touchSteam := !in.opts.NoWait && !in.opts.DryRun
	wasRunning := false
	if touchSteam && in.steamRunning() {
		wasRunning = true
		in.say("  closing Steam to change the launch options...")
		if _, err := in.closeSteam(ctx, 45); err != nil {
			return err
		}
	}
	if touchSteam && in.steamRunning() {
		in.warn("Steam did not close, so ZBBatch is not switched on. " + in.exitSteamHint() + " and run the installer again.")
	} else {
		root := filepath.Clean(in.steamRoot)
		for _, f := range files {
			ft, err := readText(f)
			if err != nil {
				return err
			}
			r := LaunchEditWindows(ft)
			if r.Changed == 0 {
				continue
			}
			rel, err := filepath.Rel(root, f)
			if err != nil {
				return err
			}
			if err := in.backupFile(rel, root, "Steam"); err != nil {
				return err
			}
			if err := in.writeFile(f, []byte(r.Text)); err != nil {
				return err
			}
			word := "entries"
			if r.Changed == 1 {
				word = "entry"
			}
			in.change(fmt.Sprintf("  switched on in Steam's launch options (%d %s in %s): -agentlib:zbNative -javaagent:ZBBatch.jar --", r.Changed, word, f))
		}
	}
	if wasRunning {
		if err := in.sys.StartSteam(in.steamRoot); err == nil {
			in.say("  Steam started again")
		} else {
			in.say("  could not start Steam again; start it yourself")
		}
	}
	return nil
}

func (in *Installer) zbbatchLinux(ctx context.Context) error {
	t, err := readText(in.memoryJSON())
	if err != nil {
		return err
	}
	loader, inLaunch := false, false
	for _, f := range in.launchFiles() {
		ft, err := readText(f)
		if err != nil {
			continue
		}
		r := LaunchScanLinux(ft)
		if r.Already > 0 {
			inLaunch = true
		}
		if r.Changed > 0 || r.Already > 0 {
			loader = true
		}
	}
	if HasZBLoader(t) {
		loader = true
	}
	if inLaunch {
		in.say("  already switched on in Steam's launch options")
		return nil
	}
	if !loader && in.zb.planned {
		loader = true
	}
	if !loader {
		in.warn("ZombieBuddy's loader (-javaagent:ZombieBuddy.jar in Steam's launch options) was not found, so ZBBatch is not switched on. Set up ZombieBuddy first, then run the installer again.")
		return nil
	}
	nt, changed, err := AddVMArg(t, "-javaagent:ZBBatch.jar")
	if err != nil {
		in.warn("could not add -javaagent:ZBBatch.jar to ProjectZomboid64.json (" + err.Error() + "); ZBBatch is not switched on")
		return nil
	}
	if !changed {
		in.say("  already switched on in ProjectZomboid64.json")
		return nil
	}
	if err := in.writeGameJSON(nt); err != nil {
		return err
	}
	in.change("  switched on: -javaagent:ZBBatch.jar added to ProjectZomboid64.json")
	return nil
}

func (in *Installer) ModList(ctx context.Context) error {
	list := filepath.Join(in.zdir, "mods", "default.txt")
	in.say("Mod list:")
	if !isFile(list) {
		in.say("  No mod list at " + list + " yet; start the game once, then run this again.")
		return nil
	}
	t, err := readText(list)
	if err != nil {
		return err
	}
	r := PlaceShim(t)
	switch {
	case r.NoZB:
		in.say("  ZombieBuddy isn't enabled in " + list + ", so PZArrayList Compat wasn't added; enable ZombieBuddy first.")
	case r.Unchanged:
		in.say("  Already enabled in the mod list, directly below ZombieBuddy.")
	default:
		if err := in.backupFile("default.txt", filepath.Dir(list), "Zomboid-mods"); err != nil {
			return err
		}
		if err := in.writeFile(list, []byte(r.Text)); err != nil {
			return err
		}
		in.change("  Enabling it in the mod list (" + list + "), directly below ZombieBuddy.")
		t = r.Text
	}
	ids := in.localModEnableIDs()
	if len(ids) == 0 {
		return nil
	}
	nt, added, err := EnableMods(t, ids)
	if err != nil {
		in.warn("could not enable the local mods in " + list + ": " + err.Error())
		return nil
	}
	if len(added) == 0 {
		in.say("  Local mods already enabled: " + strings.Join(ids, ", "))
		return nil
	}
	if err := in.backupFile("default.txt", filepath.Dir(list), "Zomboid-mods"); err != nil {
		return err
	}
	if err := in.writeFile(list, []byte(nt)); err != nil {
		return err
	}
	in.change("  Enabled local mods: " + strings.Join(added, ", "))
	return nil
}

func (in *Installer) sameAfterModList(src, dst, rel string) bool {
	if filepath.ToSlash(rel) != "mods/default.txt" {
		return false
	}
	st, err1 := readText(src)
	dt, err2 := readText(dst)
	if err1 != nil || err2 != nil {
		return false
	}
	if r := PlaceShim(st); !r.NoZB {
		st = r.Text
	}
	if ids := in.localModEnableIDs(); len(ids) > 0 {
		if nt, _, err := EnableMods(st, ids); err == nil {
			st = nt
		}
	}
	return st == dt
}

func testTrigger(dir string) string {
	files, err := listFiles(dir)
	if err != nil {
		return ""
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err == nil && bytes.Contains(b, []byte("AutoLua.txt")) {
			return f
		}
	}
	return ""
}
