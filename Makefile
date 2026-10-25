APP_NAME  := GoPass
BIN       := bin/go-pass
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
LDFLAGS   := -s -w -X main.version=$(VERSION)

.PHONY: all build run test vet fmt clean bundle install

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

clean:
	rm -rf bin dist

# macOS .app-бандл: нужен, чтобы приложение запускалось двойным кликом и
# корректно забирало фокус для pinentry.
bundle: build
	rm -rf dist/$(APP_NAME).app
	mkdir -p dist/$(APP_NAME).app/Contents/MacOS
	mkdir -p dist/$(APP_NAME).app/Contents/Resources
	cp packaging/Info.plist dist/$(APP_NAME).app/Contents/Info.plist
	cp $(BIN) dist/$(APP_NAME).app/Contents/MacOS/go-pass
	chmod +x dist/$(APP_NAME).app/Contents/MacOS/go-pass
	codesign --force --sign - dist/$(APP_NAME).app >/dev/null 2>&1 || true
	@echo "Готово: dist/$(APP_NAME).app"

install: bundle
	rm -rf /Applications/$(APP_NAME).app
	cp -R dist/$(APP_NAME).app /Applications/
	@echo "Установлено: /Applications/$(APP_NAME).app"
