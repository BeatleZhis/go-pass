APP_NAME  := GoPass
BIN       := bin/go-pass
ICON_PNG  := assets/icon-1024.png
ICON_ICNS := assets/AppIcon.icns
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
LDFLAGS   := -s -w -X main.version=$(VERSION)
APP_DIR   := dist/$(APP_NAME).app
# Минимальная версия macOS: Go-тулчейн собирает объектные файлы под 13.0.
MACOS_MIN ?= 13.0

.PHONY: all build run test vet fmt clean bundle bundle-universal universal \
        icon icns zip dmg release install

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
	./scripts/bundle.sh $(BIN) $(APP_DIR) $(ICON_ICNS)

# Универсальный бинарник arm64 + x86_64. На Apple Silicon clang умеет обе
# архитектуры, поэтому Intel-сборка делается на том же раннере.
universal: icns
	mkdir -p bin
	CGO_ENABLED=1 GOARCH=arm64 MACOSX_DEPLOYMENT_TARGET=$(MACOS_MIN) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o bin/go-pass-arm64 .
	CGO_ENABLED=1 GOARCH=amd64 MACOSX_DEPLOYMENT_TARGET=$(MACOS_MIN) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o bin/go-pass-amd64 .
	lipo -create -output bin/go-pass-universal bin/go-pass-arm64 bin/go-pass-amd64
	lipo -info bin/go-pass-universal

bundle-universal: universal
	./scripts/bundle.sh bin/go-pass-universal $(APP_DIR) $(ICON_ICNS)

# Пакеты для выкладки: zip для GitHub Releases и dmg для установки мышкой.
zip: bundle-universal
	ditto -c -k --keepParent $(APP_DIR) dist/$(APP_NAME)-$(VERSION)-macos-universal.zip

dmg: bundle-universal
	./scripts/make-dmg.sh $(APP_DIR) dist/$(APP_NAME)-$(VERSION).dmg "$(APP_NAME)"

release: bundle-universal
	ditto -c -k --keepParent $(APP_DIR) dist/$(APP_NAME)-$(VERSION)-macos-universal.zip
	./scripts/make-dmg.sh $(APP_DIR) dist/$(APP_NAME)-$(VERSION).dmg "$(APP_NAME)"
	@echo "Пакеты: dist/*.zip dist/*.dmg"

install: bundle
	rm -rf /Applications/$(APP_NAME).app
	cp -R $(APP_DIR) /Applications/
	@echo "Установлено: /Applications/$(APP_NAME).app"
