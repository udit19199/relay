#!/bin/zsh

# Local ad-hoc signed bundle only. No releases, no signing identities.

set -euo pipefail

SCRIPT_DIR=${0:A:h}
PROJECT_DIR=${SCRIPT_DIR:h}
APP_ICON="$PROJECT_DIR/assets/AppIcon.icns"
APP_VERSION=0.2.0

cd "$PROJECT_DIR"
BUILD_DIR=$(swift build -c release --show-bin-path)
swift build -c release
APP_DIR="$BUILD_DIR/Relay.app"
CONTENTS_DIR="$APP_DIR/Contents"
MACOS_DIR="$CONTENTS_DIR/MacOS"
RESOURCES_DIR="$CONTENTS_DIR/Resources"

rm -rf "$APP_DIR"

mkdir -p "$MACOS_DIR" "$RESOURCES_DIR"
cp "$BUILD_DIR/Relay" "$MACOS_DIR/Relay"
chmod 0755 "$MACOS_DIR/Relay"
ditto "$APP_ICON" "$RESOURCES_DIR/AppIcon.icns"
cp "$PROJECT_DIR/LICENSE" "$RESOURCES_DIR/LICENSE.txt"
cp "$PROJECT_DIR/LICENSE-GPL-3.0-or-later" "$RESOURCES_DIR/GPL-3.0-or-later.txt"

/usr/bin/plutil -create xml1 "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleDevelopmentRegion -string en "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleDisplayName -string "Relay" "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleExecutable -string Relay "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleIdentifier -string com.udit19199.relay "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleIconFile -string AppIcon "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleInfoDictionaryVersion -string 6.0 "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleName -string "Relay" "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundlePackageType -string APPL "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleShortVersionString -string "$APP_VERSION" "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert CFBundleVersion -string "$APP_VERSION" "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert LSMinimumSystemVersion -string 14.0 "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert LSUIElement -bool true "$CONTENTS_DIR/Info.plist"
/usr/bin/plutil -insert NSHighResolutionCapable -bool true "$CONTENTS_DIR/Info.plist"

codesign --force --deep --sign - "$APP_DIR"
codesign --verify --deep --strict "$APP_DIR"

echo "$APP_DIR"
