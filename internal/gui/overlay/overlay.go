// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package overlay implements the display backend for Wayland compositors that
// do not implement zwlr_layer_shell_v1 — GNOME/Mutter above all, which has
// never adopted it because it is a wlroots extension rather than part of
// wayland-protocols.
//
// Core Wayland gives a client no way to position its own window, so an ordinary
// toplevel can only ever land wherever the compositor puts it (centred, on
// Mutter). This backend sidesteps positioning entirely: the window is made
// fullscreen — a standard xdg_toplevel request every compositor honours — and
// the drawer is placed against the right edge inside it, the same structure the
// gamescope backend uses.
//
// Covering the output would normally mean covering the user's screen, so the
// window is transparent everywhere except the drawer, and its *input region* is
// restricted to the drawer's rectangle. Everything outside is therefore
// click-through: pointer events reach the windows underneath and the desktop
// stays fully usable while the drawer is open. Nothing is obscured, visually or
// functionally.
//
// One consequence is deliberate. With no backdrop swallowing clicks there is no
// click-outside-to-dismiss; dismissal is Escape plus focus loss, because
// clicking another window focuses it and deactivates ours. That is the same
// gesture without the modal takeover.
package overlay

import (
	"log/slog"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dahui/z13gui/internal/panelgeom"
)

const (
	animDuration   = 200 * time.Millisecond // slide duration, matches layer-shell
	focusLossDelay = 200                    // ms; confirmation delay before auto-hide
	keyEscape      = 0xff1b                 // GDK_KEY_Escape
)

// Backend manages the drawer as a right-aligned panel inside a fullscreen,
// transparent, click-through window.
type Backend struct {
	appWin *gtk.ApplicationWindow
	gtkWin *gtk.Window

	drawerWidth int
	fixed       *gtk.Fixed // fills the window; positions panel
	panel       *gtk.Box   // the drawer itself

	geomReady  bool
	monW, monH int
	rest       panelgeom.Rect // panel rectangle at rest

	progress  float64 // 0 = parked off the right edge, 1 = at rest
	animGen   uint64  // incremented to cancel in-flight animations
	animating bool

	// focusedSinceShow gates focus-loss dismiss on the window having actually
	// been focused first. CLAUDE.md forbids this guard in the layer-shell
	// backend, where KWin can drop focus during a keyboard-mode transition and
	// never re-grant it, so the drawer would dismiss itself on first show.
	// There is no keyboard-mode transition here — this is an ordinary toplevel
	// that either gets focus or does not — so the guard is safe, and it is
	// better than a wall-clock settle timer because it keys on the real event.
	focusedSinceShow bool

	isVisible func() bool
	onDismiss func()
}

// New creates an overlay backend. drawerWidth is the drawer panel width in pixels.
func New(appWin *gtk.ApplicationWindow, gtkWin *gtk.Window, drawerWidth int) *Backend {
	return &Backend{appWin: appWin, gtkWin: gtkWin, drawerWidth: drawerWidth}
}

// Configure sets up the fullscreen window, Escape dismiss and focus-loss
// dismiss. Unlike the other two backends it does not map the window: the
// layer-shell backend keeps its surface alive to dodge a KWin ghost-surface
// artifact, and gamescope keeps its overlay mapped so gamescope can composite
// it, but a mapped fullscreen window here would sit in the task switcher and
// hold an input region across the whole desktop while the drawer is closed.
func (b *Backend) Configure(isVisible func() bool, onDismiss func()) {
	b.isVisible = isVisible
	b.onDismiss = onDismiss
	b.appWin.SetDecorated(false)

	// Geometry is only knowable once there is a surface to ask which monitor it
	// is on, which is after WrapContent has built the panel.
	b.appWin.Connect("realize", func() { b.updateGeometry() })

	key := gtk.NewEventControllerKey()
	key.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == keyEscape {
			onDismiss()
			return true
		}
		return false
	})
	b.gtkWin.AddController(key)

	// Focus-loss dismiss. Deliberately not a copy of the layer-shell handler:
	// its pointer-inside check and 500ms settle window exist to filter spurious
	// focus drops KWin produces during layer-shell keyboard-mode transitions,
	// which cannot happen to a plain toplevel. Worse, the pointer-inside check
	// would actively swallow a real dismiss here — alt-tabbing away with the
	// pointer resting over the drawer is a genuine focus loss.
	b.appWin.Connect("notify::is-active", func() {
		if b.appWin.IsActive() {
			b.focusedSinceShow = true
			return
		}
		if !b.isVisible() || b.animating || !b.focusedSinceShow {
			return
		}
		// Re-check after a moment: compositors flicker focus during their own
		// transitions (GNOME's overview animation being the obvious one).
		glib.TimeoutAdd(focusLossDelay, func() bool {
			if b.isVisible() && !b.appWin.IsActive() {
				slog.Debug("overlay: dismissing on focus loss")
				b.onDismiss()
			}
			return false
		})
	})

	slog.Info("backend", "mode", "overlay")
}

// updateGeometry re-reads the monitor the window is on and resizes the panel to
// match. Called on realize and again on every Show, because the user may have
// changed resolution, scaling or monitor since the drawer was last open.
func (b *Backend) updateGeometry() {
	display := gdk.DisplayGetDefault()
	surfacer := b.appWin.Surface()
	if display == nil || surfacer == nil {
		return
	}
	surface, ok := surfacer.(*gdk.Surface)
	if !ok || surface == nil {
		return
	}
	monitor := display.MonitorAtSurface(surface)
	if monitor == nil {
		return
	}

	geo := monitor.Geometry()
	b.monW, b.monH = geo.Width(), geo.Height()
	b.rest = panelgeom.Panel(b.monW, b.monH, b.drawerWidth, panelgeom.DefaultMarginFraction)
	b.geomReady = b.rest.W > 0 && b.rest.H > 0
	if !b.geomReady {
		slog.Warn("overlay: monitor reported an unusable geometry", "w", b.monW, "h", b.monH)
		return
	}

	// Pin to this monitor so the window covers exactly one output.
	b.appWin.FullscreenOnMonitor(monitor)

	if b.panel != nil {
		// The explicit height is what makes this backend immune to the
		// collapsing-ScrolledWindow problem: the panel is told how tall to be
		// rather than asking its content.
		b.panel.SetSizeRequest(b.rest.W, b.rest.H)
	}
	b.applyProgress(b.progress)
	slog.Debug("overlay: geometry", "monW", b.monW, "monH", b.monH, "panel", b.rest)
}

// WrapContent puts the drawer in a GtkFixed so it can be moved horizontally
// during the slide. A box with margins cannot do this: GTK margins are clamped
// to zero or more, so there is no way to express "off the right edge".
func (b *Backend) WrapContent(drawer gtk.Widgetter) gtk.Widgetter {
	b.fixed = gtk.NewFixed()
	b.fixed.SetHExpand(true)
	b.fixed.SetVExpand(true)

	b.panel = gtk.NewBox(gtk.OrientationVertical, 0)
	b.panel.SetSizeRequest(b.drawerWidth, -1)
	b.panel.Append(drawer)

	// Geometry is not known yet; realize positions it before the window is
	// ever on screen.
	b.fixed.Put(b.panel, 0, 0)
	return b.fixed
}

// Scale is 1.0: GTK applies the compositor's scale factor itself here, so the
// drawer's CSS pixel values are already correct. internal/uiscale must not be
// reused — it exists only because gamescope runs its own scaler on top of GTK's
// and the two would compound.
func (b *Backend) Scale() float64 { return 1.0 }

// Show maps the window and slides the panel in from the right edge.
func (b *Backend) Show() {
	b.focusedSinceShow = false
	b.appWin.SetVisible(true)
	b.appWin.Present()
	b.updateGeometry()
	b.slideTo(1, func() {
		b.animating = false
		// Re-apply once more after GTK has settled the layout. The first frames
		// of the slide can run before the panel has been allocated, so the final
		// position must be recomputed from a width that is known by now, or the
		// drawer rests a few pixels off the edge for as long as it is open.
		glib.IdleAdd(func() bool {
			b.applyProgress(1)
			b.applyInputRegion()
			return false
		})
	})
}

// Hide slides the panel out and then unmaps the window.
func (b *Backend) Hide() {
	// Drop the input region first so the desktop is click-through for the whole
	// slide-out rather than only once it finishes.
	b.clearInputRegion()
	b.slideTo(0, func() {
		b.animating = false
		b.appWin.SetVisible(false)
		b.focusedSinceShow = false
	})
}

// slideTo animates progress to target over animDuration.
//
// animGen mirrors the layer-shell backend's generation counter: a show landing
// mid-hide bumps it, and the in-flight tick callback sees the mismatch and stops
// instead of dragging the panel back off-screen behind the new animation.
func (b *Backend) slideTo(target float64, onDone func()) {
	b.animGen++
	gen := b.animGen
	b.animating = true
	from := b.progress
	t0 := time.Now()

	b.gtkWin.AddTickCallback(func(_ gtk.Widgetter, _ gdk.FrameClocker) bool {
		if b.animGen != gen {
			slog.Debug("overlay: animation cancelled", "gen", gen, "current", b.animGen)
			return false
		}
		raw := float64(time.Since(t0)) / float64(animDuration)
		if raw >= 1 {
			b.applyProgress(target)
			if onDone != nil {
				onDone()
			}
			return false
		}
		b.applyProgress(from + (target-from)*panelgeom.Smoothstep(raw))
		return true
	})
}

// currentRest returns the at-rest rectangle, corrected for the width GTK
// actually gave the panel.
//
// This correction is load-bearing. GtkFixed allocates every child its *natural*
// size, and SetSizeRequest only sets a minimum, so the drawer comes out wider
// than drawerWidth once .drawer's border and padding are added — about 337px for
// a 320px request. Positioning at monW-320 then pushes that extra width off the
// right of the screen, clipping the second column of every button row.
//
// The layer-shell backend never has to think about this because the compositor
// sizes the anchored surface, and it says so where it polls for "the actual
// width, which may exceed drawerWidth due to CSS borders/styling". Here nothing
// constrains the child, so the position has to follow the real width instead.
func (b *Backend) currentRest() panelgeom.Rect {
	r := b.rest
	w := b.panel.Width()
	if w <= 0 {
		return r // not allocated yet; the next frame corrects it
	}
	if w > b.monW {
		w = b.monW
	}
	r.W, r.X = w, b.monW-w
	return r
}

// applyProgress moves the panel to the position for p.
func (b *Backend) applyProgress(p float64) {
	b.progress = p
	if !b.geomReady || b.fixed == nil || b.panel == nil {
		return
	}
	rest := b.currentRest()
	x := panelgeom.SlideX(rest, b.monW, p)
	b.fixed.Move(b.panel, float64(x), float64(rest.Y))
}

// applyInputRegion restricts input to the panel rectangle, making the rest of
// the fullscreen window click-through.
func (b *Backend) applyInputRegion() {
	if !b.geomReady || b.panel == nil {
		return
	}
	rest := b.currentRest()
	x := panelgeom.SlideX(rest, b.monW, b.progress)
	// Log the rectangle actually applied, not the requested one: this is the
	// width after GTK's allocation, which is the number that matters when the
	// drawer looks misplaced. The "geometry" line above is logged before
	// allocation and still carries the uncorrected drawerWidth.
	slog.Debug("overlay: input region", "x", x, "y", rest.Y, "w", rest.W, "h", rest.H)
	b.setInputRegion(cairo.RectangleNew(x, rest.Y, rest.W, rest.H))
}

// clearInputRegion makes the whole window click-through.
func (b *Backend) clearInputRegion() { b.setInputRegion(nil) }

// setInputRegion applies rect as the surface's input region, or an empty region
// when rect is nil. Note the inversion: an *empty* region means the surface
// accepts no pointer input at all, which is what makes it click-through.
func (b *Backend) setInputRegion(rect *cairo.Rectangle) {
	surfacer := b.appWin.Surface()
	if surfacer == nil {
		return
	}
	surface, ok := surfacer.(*gdk.Surface)
	if !ok || surface == nil {
		return
	}

	region, err := cairo.RegionCreate()
	if err != nil {
		slog.Warn("overlay: could not create an input region", "err", err)
		return
	}
	if rect != nil {
		if region, err = region.CreateRectangle(rect); err != nil {
			slog.Warn("overlay: could not build the panel input region", "err", err)
			return
		}
	}
	surface.SetInputRegion(region)
}
