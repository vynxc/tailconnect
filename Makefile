GO ?= go
BIN ?= bin/tailconnect

.PHONY: all build test fmt vet android-bin android-bin-x86 android-apk clean

all: build test

build:
	mkdir -p $(dir $(BIN))
	$(GO) build -trimpath -o $(BIN) ./cmd/tailconnect

test:
	$(GO) test ./...

fmt:
	gofmt -w cmd internal

vet:
	$(GO) vet ./...

android-bin:
	mkdir -p android/app/src/main/jniLibs/arm64-v8a
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -buildmode=pie \
		-ldflags="-s -w" -o android/app/src/main/jniLibs/arm64-v8a/libtailconnect.so ./cmd/tailconnect

android-bin-x86:
	@test -n "$$ANDROID_NDK_HOME" || (echo "set ANDROID_NDK_HOME to an NDK (r29 recommended)" >&2; exit 1)
	mkdir -p android/app/src/main/jniLibs/x86_64
	CC="$$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android26-clang" \
	CGO_ENABLED=1 GOOS=android GOARCH=amd64 $(GO) build -trimpath -buildmode=pie \
		-ldflags="-s -w" -o android/app/src/main/jniLibs/x86_64/libtailconnect.so ./cmd/tailconnect

android-apk: android-bin
	cd android && ./gradlew :app:assembleDebug

clean:
	rm -rf bin
	rm -rf android/app/src/main/jniLibs
	rm -rf android/app/build android/build android/.gradle
