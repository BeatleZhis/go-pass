#!/bin/sh
# Собирает DMG с приложением и ярлыком /Applications.
# Использование: scripts/make-dmg.sh <путь .app> <выходной .dmg> <имя тома>
set -eu

app="$1"
out="$2"
volume="${3:-GoPass}"

if [ ! -d "$app" ]; then
	echo "make-dmg.sh: нет бандла $app" >&2
	exit 1
fi

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"

rm -f "$out"

# На macOS 26+ hdiutil create объявлен устаревшим, есть diskutil image create.
if diskutil image create from --help >/dev/null 2>&1; then
	diskutil image create from --format UDZO --volumeName "$volume" "$stage" "$out" >/dev/null
else
	hdiutil create -volname "$volume" -srcfolder "$stage" -ov -format UDZO "$out" >/dev/null
fi

echo "Готово: $out"
