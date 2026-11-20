# PZ Launcher

A launcher for Project Zomboid (Build 42) and installer for a friends' fix pack.
Unofficial fan tool; not affiliated with The Indie Stone.

## Download

Get the latest release from the [Releases page](https://github.com/katachinosekai/pzlauncher/releases/latest):

- Windows: `PZLauncher-windows-amd64.exe`
- Linux: `PZLauncher-linux-x86_64.AppImage` (`chmod +x` it first)

The launcher can update itself (bottom bar > Check for launcher update).

## Features

- **Play**: saves and servers with pre-launch checks (game version, missing mods, server status), launch straight into a save or server
- **Install / Update**: installs a `PZ-Fixes.zip` fix pack (Workshop collection, settings, ZombieBuddy approvals, memory settings, ZBBatch); dry run shows what would change; backups of everything it replaces
- **Workshop**: search and browse the Workshop, your subscriptions and the pack collection; subscribe in the background after signing in on Steam's own page
- **Mods**, **Settings** (memory, Java options), **Tools** (ZombieBuddy setup, Java mod approvals, log collection, save backups)

## Build

Requires Go and the Fyne build dependencies.

```sh
go build -tags wayland ./cmd/pzlauncher   # or without the tag on X11
```

Release builds use [fyne-cross](https://github.com/fyne-io/fyne-cross).

## Credits

Project Zomboid is made by The Indie Stone. ZombieBuddy is by zed-0xff. Mods belong to their authors.

## License

MIT
