// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"log/slog"
)

// GLibDomainKey is the attribute gotk4 attaches to every message it forwards from
// GLib/GTK. It is how app logs are told apart from toolkit noise.
const GLibDomainKey = "glib_domain"

// filterHandler applies separate level thresholds to app logs and to GTK/GLib
// logs. gotk4's glib.init() routes every GLib/GTK message through slog.Default()
// with a glib_domain attribute, so without this the drawer's own Info lines are
// buried under toolkit chatter — and turning the level up to hide that chatter
// would hide the drawer's messages too.
type filterHandler struct {
	inner    slog.Handler
	appLevel slog.Level
	gtkLevel slog.Level

	// gtkFromAttrs records that glib_domain arrived through WithAttrs rather than
	// on the record. A handler that only inspected the record would misclassify
	// every message from a logger derived with slog.With(glib_domain, …) — gotk4
	// currently passes it per-record, but the slog.Handler contract requires
	// WithAttrs values to be treated as though they were on the record.
	gtkFromAttrs bool
}

// NewFilterHandler wraps inner with split-level filtering. appLevel is the
// threshold for application messages, gtkLevel for GTK/GLib ones.
func NewFilterHandler(inner slog.Handler, appLevel, gtkLevel slog.Level) slog.Handler {
	return &filterHandler{inner: inner, appLevel: appLevel, gtkLevel: gtkLevel}
}

// Enabled passes anything either threshold would admit: the source of a message
// is not known until Handle can inspect its attributes.
func (h *filterHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.appLevel || level >= h.gtkLevel
}

func (h *filterHandler) Handle(ctx context.Context, r slog.Record) error {
	isGTK := h.gtkFromAttrs
	if !isGTK {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == GLibDomainKey {
				isGTK = true
				return false
			}
			return true
		})
	}
	if isGTK && r.Level < h.gtkLevel {
		return nil
	}
	if !isGTK && r.Level < h.appLevel {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

func (h *filterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	gtk := h.gtkFromAttrs
	for _, a := range attrs {
		if a.Key == GLibDomainKey {
			gtk = true
			break
		}
	}
	return &filterHandler{
		inner:        h.inner.WithAttrs(attrs),
		appLevel:     h.appLevel,
		gtkLevel:     h.gtkLevel,
		gtkFromAttrs: gtk,
	}
}

func (h *filterHandler) WithGroup(name string) slog.Handler {
	return &filterHandler{
		inner:        h.inner.WithGroup(name),
		appLevel:     h.appLevel,
		gtkLevel:     h.gtkLevel,
		gtkFromAttrs: h.gtkFromAttrs,
	}
}
