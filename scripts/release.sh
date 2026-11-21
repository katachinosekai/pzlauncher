#!/bin/bash
set -euo pipefail
V="${1:?usage: scripts/release.sh <version> [--publish] [--prerelease]}"
shift
PUBLISH=0
PRE=""
for a in "$@"; do
	case "$a" in
	--publish) PUBLISH=1 ;;
	--prerelease) PRE="--prerelease" ;;
	*) echo "unknown option $a" >&2; exit 2 ;;
	esac
done
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
APPIMAGETOOL="${APPIMAGETOOL:-$HOME/.local/share/appimagetool/appimagetool}"
SIGNKEY="${SIGNKEY:-$HOME/.signing/pzlauncher-release-ed25519.key}"
[ -f "$SIGNKEY" ] || { echo "release key $SIGNKEY not found" >&2; exit 1; }
REPO="katachinosekai/pzlauncher"
ID="io.github.katachinosekai.pzlauncher"
RELAY="${RELAY:-https://pzlauncher-report-relay.katachinosekai.workers.dev}"
OUT="$ROOT/release/v$V"
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "commit your changes first" >&2; exit 1; }
scripts/thirdparty.sh >/dev/null
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "THIRD_PARTY_LICENSES.txt was out of date: commit the regenerated file" >&2; exit 1; }
rm -rf "$OUT"
mkdir -p "$OUT"
for os in windows linux; do
	name=PZLauncher
	[ "$os" = windows ] && name=PZLauncher.exe
	fyne-cross "$os" -arch amd64 -app-id "$ID" -app-version "$V" -icon cmd/pzlauncher/Icon.png -name "$name" -ldflags "-X github.com/katachinosekai/pzlauncher/internal/report.RelayURL=$RELAY" ./cmd/pzlauncher
done
cp fyne-cross/bin/windows-amd64/PZLauncher.exe "$OUT/PZLauncher-windows-amd64.exe"
cp cmd/pzlauncher/THIRD_PARTY_LICENSES.txt "$OUT/THIRD_PARTY_LICENSES.txt"
APPDIR="$(mktemp -d)/PZLauncher.AppDir"
mkdir -p "$APPDIR/usr/bin"
cp fyne-cross/bin/linux-amd64/pzlauncher "$APPDIR/usr/bin/pzlauncher"
cp cmd/pzlauncher/Icon.png "$APPDIR/pzlauncher.png"
ln -s usr/bin/pzlauncher "$APPDIR/AppRun"
printf '[Desktop Entry]\nType=Application\nName=PZ Launcher\nComment=Project Zomboid launcher and fix-pack installer\nExec=pzlauncher\nIcon=pzlauncher\nCategories=Game;\nTerminal=false\n' >"$APPDIR/pzlauncher.desktop"
ARCH=x86_64 "$APPIMAGETOOL" --no-appstream "$APPDIR" "$OUT/PZLauncher-linux-x86_64.AppImage" >/dev/null
got="$("$OUT/PZLauncher-linux-x86_64.AppImage" -version)"
[ "$got" = "$V" ] || { echo "AppImage reports version '$got', want '$V'" >&2; exit 1; }
for b in fyne-cross/bin/windows-amd64/PZLauncher.exe fyne-cross/bin/linux-amd64/pzlauncher; do
	grep -q "$RELAY" "$b" || { echo "$b does not contain the report relay URL" >&2; exit 1; }
done
cd "$OUT"
python3 -I - "$V" <<'PY'
import hashlib, json, sys
names = ["PZLauncher-windows-amd64.exe", "PZLauncher-linux-x86_64.AppImage"]
m = {"launcher_version": sys.argv[1], "sha256": {n: hashlib.sha256(open(n, "rb").read()).hexdigest() for n in names}}
json.dump(m, open("manifest.json", "w"), indent=2)
PY
(cd "$ROOT" && go run ./tools/relsign sign "$SIGNKEY" "$OUT/manifest.json")
cat manifest.json
if [ "$PUBLISH" = 1 ]; then
	NOTES="$ROOT/release/notes-v$V.md"
	[ -f "$NOTES" ] || { echo "write $NOTES first" >&2; exit 1; }
	git -C "$ROOT" tag -a "v$V" -m "PZ Launcher $V"
	git -C "$ROOT" push origin "v$V"
	gh release create "v$V" -R "$REPO" $PRE --title "PZ Launcher $V" --notes-file "$NOTES" \
		PZLauncher-windows-amd64.exe PZLauncher-linux-x86_64.AppImage manifest.json manifest.json.sig THIRD_PARTY_LICENSES.txt
fi
