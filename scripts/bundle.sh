#!/bin/sh
# Собирает .app-бандл из готового бинарника, Info.plist и иконки.
# Использование: scripts/bundle.sh <бинарник> <путь .app> [icns]
set -eu

bin="$1"
out="$2"
icns="${3:-assets/AppIcon.icns}"

if [ ! -f "$bin" ]; then
	echo "bundle.sh: нет бинарника $bin" >&2
	exit 1
fi

rm -rf "$out"
mkdir -p "$out/Contents/MacOS" "$out/Contents/Resources"

cp packaging/Info.plist "$out/Contents/Info.plist"
if [ -f "$icns" ]; then
	cp "$icns" "$out/Contents/Resources/AppIcon.icns"
fi

cp "$bin" "$out/Contents/MacOS/go-pass"
chmod +x "$out/Contents/MacOS/go-pass"

# Подпись: с CODESIGN_IDENTITY (Developer ID) — полноценная, иначе ad-hoc,
# чтобы приложение запускалось локально без предупреждений.
if [ -n "${CODESIGN_IDENTITY:-}" ]; then
	codesign --force --options runtime --timestamp --sign "$CODESIGN_IDENTITY" "$out"
else
	codesign --force --sign - "$out" >/dev/null 2>&1 || true
fi

echo "Готово: $out ($(lipo -archs "$out/Contents/MacOS/go-pass" 2>/dev/null || uname -m))"
