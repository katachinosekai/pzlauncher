# Notes

## play

- `Env{ZomboidDir, OpenURL, Running}` makes everything injectable for tests; package funcs `ListSaves/ListServers/Launch` use `Default()` (real ~/Zomboid, real opener, real process check).
- Saves: every dir under `Saves/` except `Multiplayer` (and mode names with spaces, which the Lua side can't parse) counts as a mode; worlds sorted newest first by dir mtime.
- Servers: `db/ServerListSteam.db` opened as `file:...?mode=ro` with modernc.org/sqlite (no CGO). Only `id,name,ip,port` and `account.username,isSavePassword` are selected; password columns are never read. One `Server` per row with its `Accounts`.
- Launch writes all three triggers every time so stale ones can't win: save/latest -> `AutoLoadSave.txt` + empty `AutoConnect.txt`; server -> `AutoConnect.txt` "<ip> <port> [account]" + empty `AutoLoadSave.txt`; always `AutoStart.txt` "1". The Lua mod checks AutoConnect first, so clearing it matters.
- Then opens `steam://rungameid/108600` (xdg-open / open / `rundll32 url.dll,FileProtocolHandler`, falling back to `cmd /c start "" url`). No +connect/+password args ever (Steam puts them before the launch options, they reach the JVM).
- Running check: Linux scans /proc/*/comm for `ProjectZomboid*` (comm is truncated to `ProjectZomboid6`) and java processes whose cmdline has `zombie.gameclient.main`; falls back to `pgrep -x ProjectZomboid6` where /proc is missing. Windows: Toolhelp snapshot for `projectzomboid*.exe`; if a java/javaw exists, PowerShell CIM reads command lines and matches `projectzomboid` / `zombie.gameclient.main`, skipping `zombie.network.gameserver` so a dedicated server on the same box doesn't block playing.
- From an AppImage, xdg-open gets an environment with AppImage vars (LD_LIBRARY_PATH, APPDIR, ...) stripped so the browser/Steam don't load bundled libs.
- Account names with spaces are refused (Lua pattern is `%S*`).

## update

- `update.Repo` default `OWNER/pzlauncher`; set with `-ldflags "-X github.com/kata/pzlauncher/internal/update.Repo=owner/repo"`. `Latest()` refuses the placeholder.
- `Latest()` reads `/repos/<repo>/releases/latest` (no token, 60 req/h limit) and then `manifest.json` from the release assets. Pack asset = first `PZ-Fixes.zip` / `PZ-Fixes-*.zip`.
- `Download` streams to a temp file next to the destination while hashing; a missing/invalid hash in the manifest is an error, mismatch returns `ErrChecksum` and deletes the temp file.
- `FetchPack` -> cache `os.UserCacheDir()/PZLauncher/packs/pack-<version>`; zip deleted after extraction. Returns `PackRoot(dir)`: if the zip has exactly one top-level folder, that folder is returned instead of the extraction root. The installer gets this path as `PackDir`.
- `Extract` validates every entry first (rejects `..`, absolute, backslash-escapes, drive letters/colons, symlinks, >4 GiB total) and only then writes, so a bad zip leaves nothing behind.
- State: `os.UserConfigDir()/PZLauncher/state.json` `{"pack_version","installed_at"}`, written atomically. GUI records it only after a successful non-dry-run install.
- Versions compare as dotted numbers, leading `v` and `-suffix`/`+meta` ignored. `Newer(latest, "")` is true (nothing installed). `LauncherOutdated` is false for `dev` builds so local builds never self-update.
- Self-update: Linux only from an AppImage (`$APPIMAGE` is replaced by rename, the running FUSE mount keeps the old inode). Windows: `exe -> exe.old`, `exe.new -> exe`, rolls back if the second rename fails; `CleanupOld()` at startup retries deleting `.old` for 15 s while the old process exits. `Restart` starts the new binary with the same args; the GUI then quits.

## gui

- `cmd/pzlauncher`, Fyne v2.8. `main.version` via `-ldflags "-X main.version=1.2.3"`; `--version` prints it.
- Installer hook: `runInstall` in `installer.go` (`func(ctx, packDir string, dryRun bool, log func(string)) (summary string, err error)`), currently a stub that returns an error so state is never written. Wire with e.g. `runInstall = func(ctx context.Context, dir string, dry bool, log func(string)) (string, error) { r, err := install.Run(ctx, install.Options{PackDir: dir, Log: log, DryRun: dry}); return fmt.Sprint(r), err }`.
- All UI updates from goroutines go through `fyne.Do`.
- Install steps for the progress bar: check, download (percent), verify+extract, install, save state.
- Local Linux build on this Fedora box: `go build -tags wayland ./cmd/pzlauncher` works (wayland/xkbcommon/EGL headers present). The default X11 build needs `libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel mesa-libGL-devel`.

## install

Port of `Zomboid-B42.22-Fixes/scripts/install.ps1` (727 lines, state of 2026-11-19) to `internal/install`. Installs from an extracted PZ-Fixes pack directory (`Options.PackDir`).

### API

- `Run(ctx, Options) (Report, error)` runs every step in order and stops at the first error. For a progress UI: `in, _ := New(opts)`, loop over `in.Steps()` (`ID`, `Title`, `Run(ctx)`), then `in.Finish(err)` for the report and the closing log lines.
- Every root is injectable: `SteamRoot`, `ZomboidDir`, `ZombieBuddyHome`, `BackupRoot` (default PackDir, like `$Here\bk-*`), `LogDir` (default `PackDir/logs`, fallback `$TMP/PZ-Fixes-logs`; `NoLogFile` turns it off), `GOOS` (decides Windows vs Linux behaviour; tests run both on Linux), `System` (process check, open URL, close/start Steam), `CollectionAPI` + `HTTPClient` (Steam GetCollectionDetails), `PollInterval` (10 s like the ps1), `SteamPollInterval` (2 s), `Now`.
- `NoWait` = the ps1's hidden `-HarnessNoWait`: no Viewpoint/collection waiting, Steam is never closed for the launch options (files are still edited, like the harness).
- `DryRun` (dropped from the ps1 on 11-16, re-added here because the launcher wants a preview): every step runs and logs what it would change (`Report.Changes`), but nothing is written, no backup folder is made and Steam is never touched or opened. Implies NoWait.
- Waiting (Viewpoint re-download, collection "Subscribe to all") is a polling loop inside the `viewpoint` / `collection` steps that calls `Options.OnWait(WaitStatus{What, Installed, Total, Missing, Done})` every poll and stops on ctx cancel (the ps1's Ctrl+C). `Installer.WaitCollection(ctx, ids)` is exported on its own too.
- `FatalError` = the ps1's `Fatal` (game running, no library, no Workshop folder, Steam did not close for the Viewpoint swap). Report has `Changes`, `Warnings`, `BackupDir`, `LogFile`, `Failed`.
- Pure text edits are exported and table-tested: `SetXmx`, `AddJVMFlags`, `HasZBBatchAgent`/`RemoveZBBatchAgent`, `AddVMArg`, `PlaceShim`, `EnableMods`, `LaunchEditWindows`, `LaunchScanLinux`, `ParseApprovals`/`MergeApprovals`, `BumpVersionMax`, `RemoveAcfItem`, `NightlyFromModInfo`.

### Step mapping (install.ps1 line ranges)

| step / func | ps1 lines | notes |
|---|---|---|
| `New`/`startLog`/`Finish` | 19-87, 721-727 | log file next to the pack; "Log saved to" is still the last line. Done line: ps1 says "Nothing needed changing" whenever no backup was made, even on a fresh install; here that line only appears when nothing changed ("Done." otherwise). |
| `Preflight` | 89-91 | ProjectZomboid64/32 running -> fatal. |
| `Locate` | 94-139 | Steam root from `Options.SteamRoot` or `paths.SteamRoot()` (registry on Windows); library = first `paths.SteamLibraries` entry with `appmanifest_108600.acf`; Workshop/game dir from `paths.WorkshopDir`/`paths.GameDir`. The ps1 tries several roots; here only one root (paths picks it). |
| `Viewpoint` | 142-208 | nightly detection (mod.info modversion `-rc/nightly/alpha/beta`, or the nightly jar sha), close Steam, `appworkshop_108600.acf` copy to `.pzfix-<stamp>` + entry removal, delete item dir, open `steam://url/CommunityFilePage/3809306528`, wait. |
| `Collection`, `WaitCollection`, `CollectionItems`, `MissingItems` | 211-333 | recursive collection fetch (filetype 2 = sub-collection), item = non-empty `<Workshop>/<id>`; API failure = warning and continue; missing Workshop folder afterwards = fatal. |
| `backupFile`/`ensureBackupDir` | 335-348 | one `bk-MMdd-HHmmss[-n]` per run, created lazily; a path already in it is never overwritten (first version kept). Prefixes kept: `Zomboid-mods`, `Zomboid`, `.zombie_buddy`, `ProjectZomboid`, `Steam`, none for Workshop files. |
| `LaunchEditWindows` (`scanAppBlocks`) | 350-389 | same brace/quote scanner over every `"108600" {` block. |
| `launchFiles` | 391-395 | `<steamRoot>/userdata/*/config/localconfig.vdf`. |
| fsops (`sameFile`, `sameTree`, `copyIfChanged`) | 397-428 | byte compare instead of SHA256 (same result). |
| `OldMods` | 461-478 | the 15 `$ModIds` (`ModIDs`). |
| `Settings` | 480-498 | overwrite + backup every run (user decision). One addition: `mods/default.txt` counts as up to date when it equals the pack copy after the shim/local-mod edits, otherwise a rerun would copy it again and re-edit it every time (the ps1 never needed this because it did not enable local mods). |
| `Presets` | 500-516 | only if absent. |
| `LocalMods` | 518-542 | `payload/localmods/*` (ps1) and also `localmods/*` (new). |
| `VersionMax` | 544-565 | 3D Construction 3820190473, two mod.info files. |
| `Approvals` | 567-596 | see below. |
| `Memory` | 599-649 | `-Xmx8192m` regex replace / insert at `"vmArgs" : [`, then the six JVM flags chained after it, JSON validated. |
| `ZBBatch` | 651-715 | per OS, see below. |
| `ModList` | 717-719 + 430-459 | `PlaceShim` (Enable-Shim) then the new local-mod enable. |

Ported: all steps. Not ported: transcript-style logging of PowerShell errors with line numbers (Go errors are logged as `error: ...`), the multi-root fallback search, and the `-EnableShim` no-op switch.

### Regex/case details

PowerShell `-match`/`-replace`/`-eq` are case-insensitive, `[regex]::Replace` and `-ceq` are not. Kept: `-Xmx` detection/replace case-sensitive (the ps1 would back up and rewrite unchanged text for a lower-case `-xmx`; here it is just inserted-or-replaced consistently), flag presence check case-insensitive, `-javaagent:ZBBatch.jar` removal case-insensitive, shim lines case-insensitive, approvals id/hash compare case-insensitive. BOMs are stripped on read (ReadAllText/Get-Content did that); everything is written UTF-8 without BOM like the ps1.

### approvals merge

`mod_approvals.json` is merged at the raw-JSON level: top-level keys keep their order, existing `mods` entries are copied byte-for-byte (so 64-bit `author_id`/`workshop_id` never go through float64), new entries are `{"id","jar_hash","decision":true}`; output is 2-space indented (the ps1's ConvertTo-Json produced its own layout). Bad JSON is an error (the ps1 would die in ConvertFrom-Json too).

### Windows vs Linux

- Game dir: Windows `<lib>/steamapps/common/ProjectZomboid`, Linux `.../ProjectZomboid/projectzomboid` (from `paths.GameDir`); `ProjectZomboid64.json`, `ZBBatch.jar` and `ZombieBuddy.jar` all live there.
- Process names: Windows Toolhelp snapshot, `ProjectZomboid64.exe`/`steam.exe`; Linux `/proc/*/comm` (names cut to 15 chars, so `ProjectZomboid6`), `steam`.
- Closing Steam: Windows `steam://exit` via `rundll32 url.dll,FileProtocolHandler`; Linux `steam -shutdown` (falls back to `flatpak run com.valvesoftware.Steam -shutdown` when there is no `steam` on PATH). Both then poll until the process is gone (30 x 2 s for the Viewpoint swap, 45 x 2 s for launch options). Restart: Windows `<steamRoot>/steam.exe`, Linux `steam` in a new session. URLs: Linux `xdg-open`.
- ZBBatch switch-on:
  - Windows (as the ps1): plain `-javaagent:ZBBatch.jar` in ProjectZomboid64.json kills the game at launch with no log (instrument.dll can't load until zbNative fixes the DLL path), so it is removed from the json and inserted into Steam's LaunchOptions right after `-agentlib:zbNative...` (`-agentlib:zbNative -javaagent:ZBBatch.jar --`). Needs Steam closed (Steam rewrites localconfig.vdf on exit); restarted afterwards if it was running. No zbNative loader -> warning, left off.
  - Linux: the DLL problem doesn't exist and ZombieBuddy is loaded as a plain agent (`gamemoderun %command% -javaagent:ZombieBuddy.jar -- -`). ZBBatch goes into the json instead, appended as the last `vmArgs` element (`AddVMArg`, keeps the file's indentation, validated). That is exactly the user's own working Linux setup (checked 2026-11-19: json ends with `"-javaagent:ZBBatch.jar"`, launch options only carry ZombieBuddy) and needs no Steam restart, so on Linux localconfig.vdf is only read, never written. It is switched on only when a ZombieBuddy loader is found (`-javaagent:ZombieBuddy.jar` or `-agentlib:zbNative` in a 108600 LaunchOptions, or ZombieBuddy in the json); if the launch options already contain `-javaagent:ZBBatch.jar` the json is left alone (no double load). Steam closing on Linux is therefore only used by the Viewpoint nightly swap.

### Local mods (AutoLoadSave)

`LocalMods` copies every directory in `<pack>/localmods/` (and the ps1's `<pack>/payload/localmods/`) to `Zomboid/mods/<dir>` (unchanged tree = skipped; changed = old copy backed up to `Zomboid-mods/` and replaced). Ids listed one per line in `localmods/enable.txt` (`#` comments allowed; it is a file, so it is never copied as a mod) are added to the end of the `mods { }` block of `Zomboid/mods/default.txt` in the `ModList` step, after the shim, copying the existing `mod = ` / `mod = \` style. Intended for the trimmed AutoLoadSave helper (only the AutoLoadSave.txt / AutoConnect.txt / AutoStart.txt triggers, built from `zomboid-fastload/mods/AutoLoadSave`), shipped as `localmods/AutoLoadSave/` + `localmods/enable.txt` containing `AutoLoadSave`. The mod itself is not built here. Note the settings snapshot `mods/default.txt` is copied over the user's list each run; the enable step re-adds the ids afterwards and the settings step treats "pack copy + shim + enabled ids" as up to date.

### Tests

`edits_test.go`: table tests from `tools/check_xmx_insert.py` cases a-d plus more, the harness (`tools/pzfix-harness.ps1`) localconfig.vdf / json / approvals / default.txt fixtures, the user's Linux launch-options line. `run_test.go`: builds a fake Steam (libraryfolders.vdf with a second library) + Workshop + Zomboid + .zombie_buddy + pack tree in `t.TempDir()`, a fake `System` (simulates Steam closing/starting and the downloads that `steam://` URLs trigger) and an httptest collection API with a sub-collection; runs the full install for windows and linux, asserts every resulting file and backup, then reruns and asserts nothing changed (no backup dir, no file change, Steam untouched). Also DryRun (no file change, Steam untouched), game running (fatal, no change), API down (warning, no wait), wait cancel via ctx (WaitStatus contents), missing Workshop folder (fatal). Nothing touches the real ~/Zomboid, Steam or game.
