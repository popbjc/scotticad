# macOS-only build and release entry points. Metal and Cocoa require Xcode's
# command line tools; cross-building this cgo application from Linux is not
# supported.

GO ?= go
VENV ?= .venv
PYTHON ?= python3
SIGN_IDENTITY ?=
NOTARY_PROFILE ?=

# one module: a go.work in a folder above mustn't take over the build
export GOWORK := off
export CGO_ENABLED := 1

# -trimpath keeps the build machine's folders out of the executable
BUILD := $(GO) build -trimpath
LDFLAGS := -s -w
VERSION := $(strip $(shell cat VERSION))
APP := build/ScottiCAD.app
APP_EXEC := $(APP)/Contents/MacOS/ScottiCAD

.PHONY: all check-macos scotticad macos examples test sign-macos notarize-macos release release-macos release-test clean

all: macos

check-macos:
	@test "$$(uname -s)" = Darwin || { echo "ScottiCAD's native build requires macOS and Xcode Command Line Tools." >&2; exit 1; }

scotticad: check-macos
	$(BUILD) -ldflags '$(LDFLAGS)' -o scotticad ./cmd/scotticad

macos: check-macos
	mkdir -p $(dir $(APP_EXEC))
	$(BUILD) -ldflags '$(LDFLAGS)' -o $(APP_EXEC) ./cmd/scotticad
	sed 's/@VERSION@/$(VERSION)/g' packaging/macos/Info.plist > $(APP)/Contents/Info.plist

examples: scotticad
	$(PYTHON) examples/build.py

test:
	$(GO) test ./...
	@if [ "$$(uname -s)" = Darwin ]; then \
		test -x $(VENV)/bin/python || { $(PYTHON) -m venv $(VENV) && $(VENV)/bin/python -m pip install --quiet --disable-pip-version-check pytest; }; \
		$(MAKE) scotticad && $(VENV)/bin/python -m pytest python/tests -q; \
	fi

release-macos: macos
	mkdir -p dist
	@set -e; \
	if [ -n "$(SIGN_IDENTITY)" ] && [ -n "$(NOTARY_PROFILE)" ]; then \
		$(MAKE) notarize-macos; \
	elif [ -n "$(SIGN_IDENTITY)" ]; then \
		$(MAKE) sign-macos; \
		echo "warning: signed but not notarized; configure NOTARY_PROFILE for public distribution" >&2; \
	else \
		echo "warning: creating an unsigned test build; configure SIGN_IDENTITY and NOTARY_PROFILE for public distribution" >&2; \
	fi
	ditto -c -k --sequesterRsrc --keepParent $(APP) dist/scotticad-v$(VERSION)-macos-$(shell uname -m).zip
	cd dist && shasum -a 256 scotticad-v$(VERSION)-macos-$(shell uname -m).zip

sign-macos: macos
	@test -n "$(SIGN_IDENTITY)" || { echo "Set SIGN_IDENTITY to a Developer ID Application certificate." >&2; exit 1; }
	codesign --force --options runtime --timestamp --sign "$(SIGN_IDENTITY)" "$(APP)"
	codesign --verify --deep --strict --verbose=2 "$(APP)"

notarize-macos: sign-macos
	@test -n "$(NOTARY_PROFILE)" || { echo "Set NOTARY_PROFILE to a configured notarytool keychain profile." >&2; exit 1; }
	mkdir -p dist
	ditto -c -k --sequesterRsrc --keepParent "$(APP)" dist/.scotticad-notarize.zip
	xcrun notarytool submit dist/.scotticad-notarize.zip --keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple "$(APP)"
	rm -f dist/.scotticad-notarize.zip

release: release-macos

release-test:
	$(MAKE) test
	$(MAKE) release-macos

clean:
	rm -rf scotticad build dist
