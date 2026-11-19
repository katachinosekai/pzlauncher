package install

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	AppID        = "108600"
	ViewpointID  = "3809306528"
	CollectionID = "3825994973"
	ZBBatchItem  = "3842156223"
	VpNightlySha = "d1c95a9af369aba499fe3d7fe721275bc928c944bd8288ed2da58af3ea801e32"
)

var ModIDs = []string{
	"PZArrayListCompat", "RealUSA_ALifeDrivingPatch", "ALifeDrivingTweaks", "KnoxGunworksLoadFix",
	"ALifeCorpseSync", "FastVoronoi", "MapVisitedSaveFix", "EngineLoadTimeFixes", "ViewpointSamplerFix",
	"PhysicsPropsReachFix", "EFTLootSpeedup", "PlowRadiusSpeedup", "ZBBatch", "ALifeLookSync", "LogWallsLadderFix",
}

var VersionMaxFiles = []string{
	"3820190473/mods/ViewpointLogWalls/42/mod.info",
	"3820190473/mods/ViewpointLogWalls/common/mod.info",
}

type WaitStatus struct {
	What      string
	Installed int
	Total     int
	Missing   []string
	Done      bool
}

type Options struct {
	PackDir                string
	Log                    func(string)
	DryRun                 bool
	NoWait                 bool
	SteamRoot              string
	ZomboidDir             string
	ZombieBuddyHome        string
	BackupRoot             string
	LogDir                 string
	NoLogFile              bool
	GOOS                   string
	System                 System
	HTTPClient             *http.Client
	CollectionAPI          string
	PollInterval           time.Duration
	SteamPollInterval      time.Duration
	OnWait                 func(WaitStatus)
	Now                    func() time.Time
	SetupZombieBuddy       bool
	RemoveZombieBuddy      bool
	RemoveZombieBuddyFiles bool
}

type Report struct {
	BackupDir string
	LogFile   string
	Changes   []string
	Warnings  []string
	Failed    bool
}

type Step struct {
	ID    string
	Title string
	Run   func(ctx context.Context) error
}

type FatalError struct{ Msg string }

func (e *FatalError) Error() string { return e.Msg }

func fatal(format string, a ...any) error { return &FatalError{Msg: fmt.Sprintf(format, a...)} }

type Installer struct {
	opts      Options
	sys       System
	logFile   *os.File
	report    Report
	backup    string
	steamRoot string
	library   string
	workshop  string
	gameDir   string
	zdir      string
	zbHome    string
	zb        zbState
}

func New(opts Options) (*Installer, error) {
	if opts.PackDir == "" {
		return nil, errors.New("PackDir is required")
	}
	abs, err := filepath.Abs(opts.PackDir)
	if err != nil {
		return nil, err
	}
	opts.PackDir = abs
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.System == nil {
		opts.System = DefaultSystem()
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 10 * time.Second
	}
	if opts.SteamPollInterval == 0 {
		opts.SteamPollInterval = 2 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.BackupRoot == "" {
		opts.BackupRoot = opts.PackDir
	}
	in := &Installer{opts: opts, sys: opts.System}
	in.startLog()
	return in, nil
}

func Run(ctx context.Context, opts Options) (Report, error) {
	in, err := New(opts)
	if err != nil {
		return Report{Failed: true}, err
	}
	for _, s := range in.Steps() {
		if err = s.Run(ctx); err != nil {
			break
		}
	}
	return in.Finish(err), err
}

func (in *Installer) Steps() []Step {
	return []Step{
		{"preflight", "Check that Project Zomboid is closed", in.Preflight},
		{"locate", "Find Steam and Project Zomboid", in.Locate},
		{"viewpoint", "Viewpoint nightly check", in.Viewpoint},
		{"collection", "Workshop collection", in.Collection},
		{"oldmods", "Remove old local copies of our mods", in.OldMods},
		{"settings", "Controls and mod settings", in.Settings},
		{"presets", "World presets", in.Presets},
		{"localmods", "Local mods", in.LocalMods},
		{"versionmax", "42.21 mods on 42.22", in.VersionMax},
		{"zombiebuddy", "ZombieBuddy setup", in.ZombieBuddy},
		{"approvals", "ZombieBuddy approvals", in.Approvals},
		{"memory", "Game memory", in.Memory},
		{"zbbatch", "ZBBatch", in.ZBBatch},
		{"modlist", "Mod list", in.ModList},
	}
}

func (in *Installer) startLog() {
	if in.opts.NoLogFile {
		return
	}
	dir := in.opts.LogDir
	if dir == "" {
		dir = filepath.Join(in.opts.PackDir, "logs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = filepath.Join(os.TempDir(), "PZ-Fixes-logs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			in.say("warning: could not create the log folder; continuing without a log")
			return
		}
	}
	p := filepath.Join(dir, "install-"+in.opts.Now().Format("20060102-150405")+".log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		in.say("warning: could not open the log file; continuing without a log")
		return
	}
	in.logFile = f
	in.report.LogFile = p
	in.say("PZ-Fixes installer - " + in.opts.Now().Format("2006-01-02 15:04:05"))
	in.say("OS: " + in.opts.GOOS + "/" + runtime.GOARCH)
	in.say("Pack folder: " + in.opts.PackDir)
	if in.opts.DryRun {
		in.say("Dry run: nothing is written, Steam is not touched")
	}
}

func (in *Installer) say(msg string) {
	if in.opts.Log != nil {
		in.opts.Log(msg)
	}
	if in.logFile != nil {
		fmt.Fprintln(in.logFile, msg)
	}
}

func (in *Installer) warn(msg string) {
	in.report.Warnings = append(in.report.Warnings, msg)
	in.say("warning: " + msg)
}

func (in *Installer) change(msg string) {
	in.report.Changes = append(in.report.Changes, strings.TrimSpace(msg))
	in.say(msg)
}

func (in *Installer) Finish(err error) Report {
	in.restartSteamIfClosed()
	if err != nil {
		in.report.Failed = true
		in.say("error: " + err.Error())
		if in.report.LogFile != "" {
			in.say("Something went wrong. Please send this file to the person who gave you the fixes: " + in.report.LogFile)
		}
	} else {
		in.say("")
		if in.backup != "" {
			in.say("Done. Originals backed up to: " + in.backup)
		} else if len(in.report.Changes) > 0 {
			in.say("Done.")
		} else {
			in.say("Done. Nothing needed changing.")
		}
		if in.report.LogFile != "" {
			in.say("Log saved to: " + in.report.LogFile)
		}
	}
	if in.logFile != nil {
		in.logFile.Close()
		in.logFile = nil
	}
	in.report.BackupDir = in.backup
	return in.report
}

func (in *Installer) Report() Report {
	r := in.report
	r.BackupDir = in.backup
	return r
}

func (in *Installer) Workshop() string  { return in.workshop }
func (in *Installer) GameDir() string   { return in.gameDir }
func (in *Installer) SteamRoot() string { return in.steamRoot }

func (in *Installer) sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (in *Installer) notify(s WaitStatus) {
	if in.opts.OnWait != nil {
		in.opts.OnWait(s)
	}
}

func (in *Installer) steamRunning() bool { return in.sys.Running(steamProcess) }

func (in *Installer) closeSteam(ctx context.Context, tries int) (bool, error) {
	if !in.steamRunning() {
		return true, nil
	}
	if err := in.sys.CloseSteam(ctx, in.steamRoot); err != nil {
		in.say("  could not ask Steam to exit: " + err.Error())
	}
	for i := 0; i < tries; i++ {
		if err := in.sleep(ctx, in.opts.SteamPollInterval); err != nil {
			return false, err
		}
		if !in.steamRunning() {
			return true, nil
		}
	}
	return false, nil
}

func (in *Installer) exitSteamHint() string {
	if in.opts.GOOS == "windows" {
		return "Exit Steam (Steam menu > Exit)"
	}
	return "Exit Steam (Steam menu > Exit, or run: steam -shutdown)"
}
