# dnstest Android

This directory contains the native Android application for the dnstest engine.

## Features

- Native Kotlin + Jetpack Compose interface (no browser wrapper or Termux dependency).
- Tests the phone's current network path using the shared Go engine: UDP, TCP, DoT, DoH and DoQ.
- Filters the resolver list by mainland China, global or other regional groups.
- Reads the active network's DNS server addresses from Android ConnectivityManager and includes them in the comparison.
- Selects IPv4 or IPv6, test rounds and online/offline resolver-list mode.
- Shows the fastest result, median response time and no-answer rate; shares the raw JSON report.

The app asks only for Internet and network-state permissions. It does not request location permission. Like the CLI, an online run may fetch the resolver list from dns.1ms.dog and the network-origin metadata from Cloudflare; DNS measurements themselves are sent directly to the DNS resolvers being tested. Results are not uploaded.

## Build

The current APK target is arm64-v8a (most modern Android phones). Install Go (matching go.mod), Android NDK 27.2.12479018, JDK 17, Android SDK 35 and Gradle 8.10.2.

From the repository root:

```sh
mkdir -p android/app/src/main/jniLibs/arm64-v8a
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/27.2.12479018"
export CC="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang"
CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC="$CC" \
  go build -trimpath -buildmode=c-shared \
  -o android/app/src/main/jniLibs/arm64-v8a/libdnstest.so .
cd android
gradle assembleDebug
```

The debug APK is written to `app/build/outputs/apk/debug/app-debug.apk`. The GitHub Actions workflow builds the same APK and uploads it as an artifact.

The Go engine is invoked through a small JNI bridge. Android-specific DNS discovery is supplied by the app via ConnectivityManager; the CLI's existing Unix resolver-file implementation remains unchanged on other platforms.
