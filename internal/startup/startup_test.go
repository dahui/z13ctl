// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseArgsPassesUnknownFlagsThrough(t *testing.T) {
	got := ParseArgs([]string{"z13gui", "--gtk-something", "extra"})

	if got.Debug {
		t.Error("Debug = true, want false")
	}
	if got.Action != ActionNone {
		t.Errorf("Action = %q, want none", got.Action)
	}
	want := []string{"z13gui", "--gtk-something", "extra"}
	assertArgs(t, got.GTKArgs, want)
}

// Our own flags must be removed, not merely read: app.Run() hands the remainder to
// GLib's option parser, which errors on anything it does not recognise.
func TestParseArgsConsumesOurFlags(t *testing.T) {
	for _, flag := range []string{"-d", "--debug", "--version", "--print-theme", "--list-themes"} {
		got := ParseArgs([]string{"z13gui", flag})
		assertArgs(t, got.GTKArgs, []string{"z13gui"})
	}
}

func TestParseArgsDebugFlag(t *testing.T) {
	for _, flag := range []string{"-d", "--debug"} {
		if got := ParseArgs([]string{"z13gui", flag}); !got.Debug {
			t.Errorf("ParseArgs(%q) Debug = false, want true", flag)
		}
	}
	// -d must be honoured wherever it appears, including after another flag.
	got := ParseArgs([]string{"z13gui", "--print-theme", "-d"})
	if !got.Debug {
		t.Error("Debug = false, want true when -d follows an action")
	}
	if got.Action != ActionPrintTheme {
		t.Errorf("Action = %q, want print-theme", got.Action)
	}
}

func TestParseArgsActions(t *testing.T) {
	tests := []struct {
		flag string
		want Action
	}{
		{flag: "--version", want: ActionVersion},
		{flag: "--print-theme", want: ActionPrintTheme},
		{flag: "--list-themes", want: ActionListThemes},
	}
	for _, tt := range tests {
		if got := ParseArgs([]string{"z13gui", tt.flag}); got.Action != tt.want {
			t.Errorf("ParseArgs(%q) Action = %q, want %q", tt.flag, got.Action, tt.want)
		}
	}
}

// Several actions on one line: the first wins, matching what a user sees from a
// single flag — print and exit.
func TestParseArgsFirstActionWins(t *testing.T) {
	got := ParseArgs([]string{"z13gui", "--list-themes", "--version"})
	if got.Action != ActionListThemes {
		t.Errorf("Action = %q, want list-themes (the first given)", got.Action)
	}
}

// GApplication expects argv[0]; dropping it would break option parsing in ways
// that only show up at runtime.
func TestParseArgsAlwaysKeepsArgv0(t *testing.T) {
	got := ParseArgs([]string{"/usr/local/bin/z13gui", "-d"})
	assertArgs(t, got.GTKArgs, []string{"/usr/local/bin/z13gui"})
}

func TestParseArgsHandlesEmptyArgv(t *testing.T) {
	got := ParseArgs(nil)
	if len(got.GTKArgs) != 1 {
		t.Errorf("GTKArgs = %v, want a single placeholder element", got.GTKArgs)
	}
	if got.Debug || got.Action != ActionNone {
		t.Errorf("unexpected result for empty argv: %+v", got)
	}
	if got := ParseArgs([]string{}); len(got.GTKArgs) != 1 {
		t.Errorf("GTKArgs = %v, want a single placeholder element", got.GTKArgs)
	}
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("GTKArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GTKArgs = %v, want %v", got, want)
		}
	}
}

// newTestLogger returns a logger writing to buf through the filter handler.
func newTestLogger(buf *bytes.Buffer, appLevel, gtkLevel slog.Level) *slog.Logger {
	inner := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(NewFilterHandler(inner, appLevel, gtkLevel))
}

func TestFilterHandlerSplitsAppAndGTKThresholds(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo, slog.LevelError)

	log.Info("app info")                         // app >= Info: kept
	log.Debug("app debug")                       // below app level: dropped
	log.Info("gtk info", GLibDomainKey, "Gtk")   // GTK below Error: dropped
	log.Error("gtk error", GLibDomainKey, "Gtk") // GTK >= Error: kept
	log.Warn("gtk warn", GLibDomainKey, "Gdk")   // GTK below Error: dropped

	out := buf.String()
	for _, want := range []string{"app info", "gtk error"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"app debug", "gtk info", "gtk warn"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output should not contain %q:\n%s", unwanted, out)
		}
	}
}

// The -d case: both thresholds drop to Debug, so everything is shown.
func TestFilterHandlerDebugModeShowsEverything(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelDebug, slog.LevelDebug)

	log.Debug("app debug")
	log.Debug("gtk debug", GLibDomainKey, "Gtk")

	out := buf.String()
	if !strings.Contains(out, "app debug") || !strings.Contains(out, "gtk debug") {
		t.Errorf("debug mode dropped messages:\n%s", out)
	}
}

// The bug this fix addresses: a logger derived with slog.With(glib_domain, …)
// carries the attribute outside the record, so a handler that only inspected the
// record would classify GTK noise as application output and let it through.
func TestFilterHandlerClassifiesGTKAttrsFromWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo, slog.LevelError).With(GLibDomainKey, "Gtk")

	log.Info("gtk noise via With")
	if got := buf.String(); strings.Contains(got, "gtk noise via With") {
		t.Errorf("GTK message from a derived logger was not filtered:\n%s", got)
	}

	log.Error("gtk failure via With")
	if got := buf.String(); !strings.Contains(got, "gtk failure via With") {
		t.Errorf("GTK error from a derived logger was dropped:\n%s", got)
	}
}

// WithGroup must not lose the classification established by WithAttrs.
func TestFilterHandlerWithGroupPreservesClassification(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo, slog.LevelError).
		With(GLibDomainKey, "Gtk").
		WithGroup("g")

	log.Info("still gtk")
	if got := buf.String(); strings.Contains(got, "still gtk") {
		t.Errorf("classification lost across WithGroup:\n%s", got)
	}
}

// An app logger that adds unrelated attributes must stay classified as app.
func TestFilterHandlerWithAttrsDoesNotMisclassifyAppLogs(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, slog.LevelInfo, slog.LevelError).With("component", "drawer")

	log.Info("app message with attrs")
	if got := buf.String(); !strings.Contains(got, "app message with attrs") {
		t.Errorf("app message with attrs was dropped:\n%s", got)
	}
}

// Enabled cannot know a record's source yet, so it must admit anything either
// threshold would accept — otherwise GTK errors would be discarded before Handle
// ever sees them.
func TestFilterHandlerEnabledAdmitsEitherThreshold(t *testing.T) {
	h := NewFilterHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), slog.LevelWarn, slog.LevelDebug)
	ctx := context.Background()

	if !h.Enabled(ctx, slog.LevelDebug) {
		t.Error("Enabled(Debug) = false, want true — the GTK threshold accepts it")
	}
	if !h.Enabled(ctx, slog.LevelError) {
		t.Error("Enabled(Error) = false, want true")
	}

	strict := NewFilterHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), slog.LevelError, slog.LevelError)
	if strict.Enabled(ctx, slog.LevelInfo) {
		t.Error("Enabled(Info) = true, want false when both thresholds are Error")
	}
}
