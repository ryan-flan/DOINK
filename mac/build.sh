#!/bin/bash
# Builds mac/build/DOINK.app: the SwiftUI menu bar app with the Go
# companion (doink) inside it, both universal (Apple silicon + Intel), and
# a zip of it next to it.
#
#   ./build.sh            version from companion/VERSION
#   ./build.sh 0.20.0     explicit version (the release workflow passes the tag)
#
# Needs Xcode or the Command Line Tools (swift, lipo, iconutil, codesign)
# and Go (devbox shell, or set GO=/path/to/go).
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(dirname "$here")"
version="${1:-$(tr -d '[:space:]' < "$root/companion/VERSION")}"
version="${version#v}"
go="${GO:-$(command -v go || true)}"
if [ -z "$go" ]; then
	echo "Go not found: run this inside 'devbox shell', or set GO=/path/to/go" >&2
	exit 1
fi

out="$here/build"
app="$out/DOINK.app"
work="$out/work"
rm -rf "$app" "$work"
mkdir -p "$work" "$app/Contents/MacOS" "$app/Contents/Resources"

echo "==> companion (Go) v$version"
for arch in arm64 amd64; do
	(cd "$root/companion" && CGO_ENABLED=0 GOOS=darwin GOARCH=$arch "$go" build -trimpath \
		-ldflags "-s -w -X main.version=$version" -o "$work/doink-$arch" ./cmd/doink)
done
lipo -create -output "$app/Contents/Resources/doink" "$work/doink-arm64" "$work/doink-amd64"

echo "==> menu bar app (Swift)"
if swift build --package-path "$here" -c release --arch arm64 --arch x86_64 >"$work/swift.log" 2>&1; then
	bin="$(swift build --package-path "$here" -c release --arch arm64 --arch x86_64 --show-bin-path)"
else
	# Universal Swift builds need full Xcode; the Command Line Tools build
	# for this Mac only.
	echo "    (universal build unavailable, building for $(uname -m) only; see $work/swift.log)"
	swift build --package-path "$here" -c release
	bin="$(swift build --package-path "$here" -c release --show-bin-path)"
fi
cp "$bin/DOINKBar" "$app/Contents/MacOS/DOINK"

echo "==> icon"
iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
	sips -z $size $size "$root/docs/logo-400.png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z $double $double "$root/docs/logo-400.png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

cat >"$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>DOINK</string>
	<key>CFBundleDisplayName</key><string>DOINK</string>
	<key>CFBundleIdentifier</key><string>io.github.doink.menubar</string>
	<key>CFBundleExecutable</key><string>DOINK</string>
	<key>CFBundleIconFile</key><string>AppIcon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$version</string>
	<key>CFBundleVersion</key><string>$version</string>
	<key>LSMinimumSystemVersion</key><string>14.0</string>
	<key>LSUIElement</key><true/>
	<key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
	<key>NSHumanReadableCopyright</key><string>MIT License</string>
</dict>
</plist>
PLIST

echo "==> sign (ad hoc)"
codesign --force --sign - "$app/Contents/Resources/doink"
codesign --force --sign - "$app"

(cd "$out" && rm -f "DOINK-v$version-macos.zip" && ditto -c -k --keepParent DOINK.app "DOINK-v$version-macos.zip")
rm -rf "$work"
echo "==> $app"
echo "==> $out/DOINK-v$version-macos.zip"
