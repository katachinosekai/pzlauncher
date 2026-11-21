#!/bin/bash
set -euo pipefail
usage="usage: scripts/release.sh build <version> | publish <version> [--prerelease] | verify <version>"
CMD="${1:?$usage}"
V="${2:?$usage}"
shift 2
PRE=""
for a in "$@"; do
	case "$a" in
	--prerelease) PRE="--prerelease" ;;
	*) echo "unknown option $a" >&2; exit 2 ;;
	esac
done
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
APPIMAGETOOL="${APPIMAGETOOL:-$HOME/.local/share/appimagetool/appimagetool}"
SIGNKEY="${SIGNKEY:-$HOME/.signing/pzlauncher-release-ed25519.key}"
REPO="katachinosekai/pzlauncher"
ID="io.github.katachinosekai.pzlauncher"
RELAY="https://pzlauncher-report-relay.katachinosekai.workers.dev"
OUT="$ROOT/release/v$V"
ASSETS=(PZLauncher-windows-amd64.exe PZLauncher-linux-x86_64.AppImage manifest.json manifest.json.sig THIRD_PARTY_LICENSES.txt)

die() { echo "release: $*" >&2; exit 1; }
clean() { [ -z "$(git status --porcelain --untracked-files=no)" ] || die "$1"; }

check() {
	local dir="$1"
	(cd "$dir" && python3 -I - "$V" <<'PY'
import hashlib, json, sys
m = json.load(open("manifest.json"))
if m.get("launcher_version") != sys.argv[1]:
    sys.exit("manifest says version %r, want %r" % (m.get("launcher_version"), sys.argv[1]))
for n, h in m["sha256"].items():
    got = hashlib.sha256(open(n, "rb").read()).hexdigest()
    if got != h:
        sys.exit("%s: sha256 %s does not match the manifest (%s)" % (n, got, h))
    print("%s  %s" % (got, n))
PY
	) || die "$dir: files don't match the manifest"
	go run ./tools/relsign verify "$dir/manifest.json" >/dev/null || die "$dir/manifest.json: signature not valid"
	local got
	got="$("$dir/PZLauncher-linux-x86_64.AppImage" -version)"
	[ "$got" = "$V" ] || die "$dir: AppImage reports version '$got', want '$V'"
	grep -aqF "$RELAY" "$dir/PZLauncher-windows-amd64.exe" || die "$dir: exe does not contain the report relay URL"
}

build() {
	[ -f "$SIGNKEY" ] || die "release key $SIGNKEY not found"
	clean "commit your changes first"
	scripts/thirdparty.sh >/dev/null
	clean "THIRD_PARTY_LICENSES.txt was out of date: commit the regenerated file"
	rm -rf "$OUT"
	mkdir -p "$OUT"
	for os in windows linux; do
		name=PZLauncher
		[ "$os" = windows ] && name=PZLauncher.exe
		fyne-cross "$os" -arch amd64 -app-id "$ID" -app-version "$V" -icon cmd/pzlauncher/Icon.png -name "$name" ./cmd/pzlauncher
	done
	for b in fyne-cross/bin/windows-amd64/PZLauncher.exe fyne-cross/bin/linux-amd64/pzlauncher; do
		grep -aqF "$RELAY" "$b" || die "$b does not contain the report relay URL"
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
	(cd "$OUT" && python3 -I - "$V" <<'PY'
import hashlib, json, sys
names = ["PZLauncher-windows-amd64.exe", "PZLauncher-linux-x86_64.AppImage"]
m = {"launcher_version": sys.argv[1], "sha256": {n: hashlib.sha256(open(n, "rb").read()).hexdigest() for n in names}}
json.dump(m, open("manifest.json", "w"), indent=2)
PY
	)
	go run ./tools/relsign sign "$SIGNKEY" "$OUT/manifest.json"
	git rev-parse HEAD >"$OUT/BUILT_FROM"
	check "$OUT"
	echo "built v$V from $(cut -c1-7 "$OUT/BUILT_FROM"); next: write release/notes-v$V.md, then scripts/release.sh publish $V"
}

publish() {
	[ -f "$OUT/BUILT_FROM" ] || die "release/v$V was not built with this script: run scripts/release.sh build $V"
	clean "uncommitted changes: the release must match the commit it was built from"
	local built head
	built="$(cat "$OUT/BUILT_FROM")"
	head="$(git rev-parse HEAD)"
	[ "$built" = "$head" ] || die "release/v$V was built from ${built:0:7} but HEAD is ${head:0:7}: rebuild"
	git fetch -q origin
	git merge-base --is-ancestor "$head" origin/master || die "${head:0:7} is not pushed to origin/master yet"
	git rev-parse -q --verify "refs/tags/v$V" >/dev/null && die "tag v$V already exists"
	NOTES="$ROOT/release/notes-v$V.md"
	[ -f "$NOTES" ] || die "write $NOTES first"
	check "$OUT"
	git tag -a "v$V" -m "PZ Launcher $V"
	git push -q origin "v$V"
	(cd "$OUT" && gh release create "v$V" -R "$REPO" $PRE --title "PZ Launcher $V" --notes-file "$NOTES" "${ASSETS[@]}")
	local dl
	dl="$(mktemp -d)"
	(cd "$dl" && gh release download "v$V" -R "$REPO")
	for a in "${ASSETS[@]}"; do
		cmp -s "$OUT/$a" "$dl/$a" || die "downloaded $a differs from the built one"
	done
	chmod +x "$dl/PZLauncher-linux-x86_64.AppImage"
	check "$dl"
	if [ -z "$PRE" ]; then
		latest="$(gh api "repos/$REPO/releases/latest" -q .tag_name)"
		[ "$latest" = "v$V" ] || die "releases/latest is $latest, not v$V"
	fi
	rm -rf "$dl"
	echo "published v$V (${head:0:7}); fresh download verified: hashes, signature, version, relay URL"
}

case "$CMD" in
build) build ;;
publish) publish ;;
verify) check "$OUT" ;;
*) echo "$usage" >&2; exit 2 ;;
esac
