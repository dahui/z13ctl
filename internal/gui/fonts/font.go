// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package fonts embeds the Inter typeface and registers it with fontconfig
// at application startup so the drawer renders identically across desktop
// (KDE/Wayland) and gamescope (Steam Gaming Mode) environments.
package fonts

import (
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
	"unsafe" //nolint:gocritic // used with cgo, requires separate import block
)

// #cgo pkg-config: fontconfig
// #include <fontconfig/fontconfig.h>
// #include <stdlib.h>
import "C" //nolint:gocritic // cgo requires standalone import

//go:embed Inter-Regular.ttf
var interRegular []byte

//go:embed Inter-Bold.ttf
var interBold []byte

// Register writes the embedded Inter font files to a runtime directory and
// registers them with fontconfig via FcConfigAppFontAddFile. This makes the
// "Inter" family available to GTK/Pango CSS without installing system-wide.
// Must be called before any GTK CSS providers are loaded.
func Register() {
	dir := filepath.Join(runtimeDir(), "z13gui", "fonts")
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gocritic // err is checked
		slog.Warn("fonts: cannot create dir", "path", dir, "err", err)
		return
	}

	for _, f := range []struct {
		name string
		data []byte
	}{
		{"Inter-Regular.ttf", interRegular},
		{"Inter-Bold.ttf", interBold},
	} {
		path := filepath.Join(dir, f.name)
		// Skip write if file already exists with correct size.
		if info, err := os.Stat(path); err != nil || info.Size() != int64(len(f.data)) {
			if err := os.WriteFile(path, f.data, 0o644); err != nil { //nolint:gocritic // err is checked
				slog.Warn("fonts: write failed", "path", path, "err", err)
				continue
			}
		}
		cpath := C.CString(path)
		ok := C.FcConfigAppFontAddFile(C.FcConfigGetCurrent(), (*C.FcChar8)(unsafe.Pointer(cpath)))
		C.free(unsafe.Pointer(cpath))
		if ok == C.FcTrue {
			slog.Debug("fonts: registered", "path", path)
		} else {
			slog.Warn("fonts: FcConfigAppFontAddFile failed", "path", path)
		}
	}
}

// runtimeDir returns XDG_RUNTIME_DIR or falls back to /tmp.
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return "/tmp"
}
