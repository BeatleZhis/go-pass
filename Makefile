APP_NAME  := GoPass
BIN       := bin/go-pass
ICON_PNG  := assets/icon-1024.png
ICON_ICNS := assets/AppIcon.icns
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
LDFLAGS   := -s -w -X main.version=$(VERSION)

.PHONY: all build run test vet fmt clean bundle install icon icns

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) .

run:
	go run .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# Иконка рисуется генератором на Go (без внешних зависимостей).
icon:
	go run tools/genicon/main.go -out $(ICON_PNG) -size 1024
	sips -z 256 256 $(ICON_PNG) --out internal/ui/assets/icon.png >/dev/null

	# Набор размеров для macOS .icns
icns: $(ICON_PNG)
	rm -rf build/AppIcon.iconset
	mkdir -p build/AppIcon.iconset
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s $(ICON_PNG) --out build/AppIcon.iconset/icon_$${s}x$${s}.png >/dev/null; \
		sips -z $$((s * 2)) $$((s * 2)) $(ICON_PNG) --out build/AppIcon.iconset/icon_$${s}x$${s}@2x.png >/dev/null; \
	done
	iconutil -c icns build/AppIcon.iconset -o $(ICON_ICNS)

clean:
	rm -rf bin dist build $(ICON_ICNS)

# macOS .app-бандл: нужен, чтобы приложение запускалось двойным кликом и
# корректно забирало фокус для pinentry.
bundle: build icns
	rm -rf dist/$(APP_NAME).app
	mkdir -p dist/$(APP_NAME).app/Contents/MacOS
	mkdir -p dist/$(APP_NAME).app/Contents/Resources
	cp packaging/Info.plist dist/$(APP_NAME).app/Contents/Info.plist
	cp $(ICON_ICNS) dist/$(APP_NAME).app/Contents/Resources/AppIcon.icns
	cp $(BIN) dist/$(APP_NAME).app/Contents/MacOS/go-pass
	chmod +x dist/$(APP_NAME).app/Contents/MacOS/go-pass
	codesign --force --sign - dist/$(APP_NAME).app >/dev/null 2>&1 || true
	@echo "Готово: dist/$(APP_NAME).app"

install: bundle
	rm -rf /Applications/$(APP_NAME).app
	cp -R dist/$(APP_NAME).app /Applications/
	@echo "Установлено: /Applications/$(APP_NAME).app"
