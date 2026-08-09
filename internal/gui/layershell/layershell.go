// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package layershell implements the Wayland layer-shell display backend.
// It handles layer-shell initialization, margin-based slide animation,
// and focus-loss auto-hide for compositors like KDE Plasma, Hyprland, and Sway.
package layershell

import (
	"log/slog"
	"math"
	"time"

	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	animDuration   = 200 * time.Millisecond // slide animation duration
	showSettleTime = 500 * time.Millisecond // ignore focus loss within this window after Show
	focusLossDelay = 200                    // ms; confirmation delay before auto-hide
	marginFraction = 20                     // screen height / N for 5% top/bottom margins
)

// Backend manages the layer-shell overlay drawer on Wayland compositors.
type Backend struct {
	appWin *gtk.ApplicationWindow
	gtkWin *gtk.Window

	drawerWidth   int
	hiddenMargin  int
	margin        int       // current right margin: 0=on-screen, hiddenMargin=1px visible
	opacity       float64   // current window opacity, tracked for fade interpolation
	animGen       uint64    // incremented to cancel in-flight animations
	animating     bool      // true during show/hide slide animation
	pointerInside bool      // true when pointer is over the drawer surface
	showTime      time.Time // when Show() was last called; used to ignore early focus loss
}

// New creates a layer-shell backend. drawerWidth is the drawer panel width in pixels.
func New(appWin *gtk.ApplicationWindow, gtkWin *gtk.Window, drawerWidth int) *Backend {
	return &Backend{
		appWin:       appWin,
		gtkWin:       gtkWin,
		drawerWidth:  drawerWidth,
		hiddenMargin: -(drawerWidth - 1),
		margin:       -(drawerWidth - 1),
	}
}

// Configure initializes layer-shell anchoring, margins, keyboard mode,
// the realize handler for monitor-based vertical margins, the keep-visible
// hack, and the focus-loss auto-hide handler.
func (b *Backend) Configure(isVisible func() bool, onDismiss func()) {
	b.appWin.SetDecorated(false)

	gtk4layershell.InitForWindow(b.gtkWin)
	gtk4layershell.SetLayer(b.gtkWin, gtk4layershell.LayerShellLayerOverlay)
	gtk4layershell.SetAnchor(b.gtkWin, gtk4layershell.LayerShellEdgeRight, true)
	gtk4layershell.SetAnchor(b.gtkWin, gtk4layershell.LayerShellEdgeTop, true)
	gtk4layershell.SetAnchor(b.gtkWin, gtk4layershell.LayerShellEdgeBottom, true)
	gtk4layershell.SetKeyboardMode(b.gtkWin, gtk4layershell.LayerShellKeyboardModeNone)
	gtk4layershell.SetMargin(b.gtkWin, gtk4layershell.LayerShellEdgeRight, b.hiddenMargin)
	b.appWin.SetSizeRequest(b.drawerWidth, -1)

	// Pin the layer surface to its monitor and set top/bottom margins to
	// 5% of screen height once the surface is realized. Pinning prevents
	// negative right-margin hide from bleeding onto an adjacent display.
	b.appWin.Connect("realize", func() {
		surface := b.appWin.Surface()
		if surface == nil {
			return
		}
		monitor := gdk.DisplayGetDefault().MonitorAtSurface(surface)
		if monitor == nil {
			return
		}
		gtk4layershell.SetMonitor(b.gtkWin, monitor)
		geo := monitor.Geometry()
		margin := geo.Height() / marginFraction
		gtk4layershell.SetMargin(b.gtkWin, gtk4layershell.LayerShellEdgeTop, margin)
		gtk4layershell.SetMargin(b.gtkWin, gtk4layershell.LayerShellEdgeBottom, margin)
	})

	// Keep the window surface alive at all times; "hidden" = margin off-screen.
	// This prevents KDE Plasma ghost-surface artifact on remap.
	// Opacity starts at 0 so the surface is in KWin's composited output (for
	// damage tracking) but invisible to the user. Show() sets opacity to 1.
	b.appWin.SetVisible(true)
	b.setOpacity(0)

	// After the surface is mapped, poll until the compositor configures the
	// actual width (which may exceed drawerWidth due to CSS borders/styling).
	// Once known, set hiddenMargin to -(actualWidth - 1) so exactly 1px remains
	// on-screen. This keeps the surface in KWin's composited output and avoids
	// a damage tracking gap that prevents rendering when launched via systemd.
	b.appWin.Connect("map", func() {
		glib.TimeoutAdd(16, func() bool {
			w := b.gtkWin.Width()
			if w <= 0 {
				return true // compositor hasn't configured yet, retry
			}
			b.hiddenMargin = -(w - 1)
			b.setMargin(b.hiddenMargin)
			return false
		})
	})

	// Track pointer position to distinguish spurious KDE focus drops from
	// genuine click-outside. When KDE drops focus after rapid keyboard-mode
	// transitions, the pointer is still inside the drawer. When the user
	// clicks outside, the pointer is outside.
	motion := gtk.NewEventControllerMotion()
	motion.ConnectEnter(func(_, _ float64) {
		b.pointerInside = true
	})
	motion.ConnectLeave(func() {
		b.pointerInside = false
	})
	b.gtkWin.AddController(motion)

	// Hide when the window loses focus, but only if:
	//   - focus was actually received since the last Show (focusedSinceShow)
	//   - enough time has passed since Show for the compositor to settle (showSettleTime)
	//   - pointer is outside the drawer (genuine click-outside, not spurious KDE drop)
	b.appWin.Connect("notify::is-active", func() {
		active := b.appWin.IsActive()
		vis := isVisible()
		slog.Debug("focus changed", "is-active", active, "visible", vis,
			"pointerInside", b.pointerInside)
		if active {
			return
		}
		if !vis {
			return
		}
		if time.Since(b.showTime) < showSettleTime {
			slog.Debug("focus lost too soon after show, ignoring")
			return
		}
		if b.pointerInside {
			slog.Debug("focus lost but pointer inside drawer, ignoring spurious drop")
			return
		}
		slog.Debug("focus lost with pointer outside, dismissing after delay")
		glib.TimeoutAdd(focusLossDelay, func() bool {
			if !isVisible() || b.appWin.IsActive() {
				slog.Debug("dismiss cancelled: hidden or refocused")
				return false
			}
			slog.Debug("dismiss confirmed: focus still lost")
			onDismiss()
			return false
		})
	})

	// Escape key dismiss (matches gamescope backend).
	key := gtk.NewEventControllerKey()
	key.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == 0xff1b { // GDK_KEY_Escape
			onDismiss()
			return true
		}
		return false
	})
	b.gtkWin.AddController(key)

	// Report what actually happened, not what was attempted. This line used to
	// be unconditional, so a session where gtk_layer_init_for_window had already
	// failed still logged a clean "mode=layer-shell" — the single most
	// misleading line in the diagnostic path, and why issue #16 looked like a
	// rendering bug rather than a missing protocol. gui.go only builds this
	// backend when IsSupported() is true, so a false here means the answer
	// changed underneath us and every anchor and margin call above was a no-op.
	if !gtk4layershell.IsSupported() {
		slog.Error("layer-shell became unavailable after the backend was selected; " +
			"the drawer will not be anchored to the screen edge")
		return
	}
	slog.Info("backend", "mode", "layer-shell",
		"protocolVersion", gtk4layershell.GetProtocolVersion())
}

// WrapContent returns the drawer as-is — layer-shell uses the drawer directly
// as the window child.
func (b *Backend) WrapContent(drawer gtk.Widgetter) gtk.Widgetter {
	return drawer
}

// Scale is always 1.0: GTK applies the compositor's scale factor itself on
// Wayland, so the drawer's CSS pixel values are already correct.
func (b *Backend) Scale() float64 { return 1.0 }

// Show starts the slide-in animation using a smoothstep easing curve.
func (b *Backend) Show() {
	slog.Debug("backend.Show", "startMargin", b.margin, "rightNeighbor", b.hasRightNeighbor())
	b.showTime = time.Now()

	gtk4layershell.SetKeyboardMode(b.gtkWin, gtk4layershell.LayerShellKeyboardModeOnDemand)
	if b.hasRightNeighbor() {
		// A monitor sits to the right: a rightward slide-off would cross onto it
		// while opaque (KWin does not clip layer-surface overflow to the assigned
		// output). Fade in place at margin 0, fully on the primary instead.
		b.setMargin(0)
		b.setOpacity(0)
		b.fadeOpacity(1, func() { b.animating = false })
	} else {
		b.setOpacity(1)
		b.slideMargin(0, func() { b.animating = false })
	}
	b.appWin.Present()
}

// Hide starts the slide-out (or fade-out) animation.
func (b *Backend) Hide() {
	slog.Debug("backend.Hide", "startMargin", b.margin, "rightNeighbor", b.hasRightNeighbor())
	if w := b.gtkWin.Width(); w > 0 {
		b.hiddenMargin = -(w - 1)
	}
	gtk4layershell.SetKeyboardMode(b.gtkWin, gtk4layershell.LayerShellKeyboardModeNone)
	b.pointerInside = false // clear stale state; surface stays mapped off-screen
	if b.hasRightNeighbor() {
		// Fade out in place (margin 0, on the primary), then park the now-invisible
		// surface off-screen. The drawer never crosses onto the right monitor.
		b.fadeOpacity(0, func() {
			b.animating = false
			b.setMargin(b.hiddenMargin)
		})
	} else {
		b.slideMargin(b.hiddenMargin, func() {
			b.animating = false
			b.setOpacity(0) // hide the 1px sliver on the primary's own right edge
		})
	}
}

// slideMargin animates b.margin from its current value to target over
// animDuration using smoothstep easing. onDone is called on the main thread
// when the animation completes (or nil to skip). Returns the animation
// generation for use in post-completion callbacks.
//
// Uses AddTickCallback (GTK4's proper animation mechanism) instead of
// glib.TimeoutAdd. Tick callbacks fire in the frame clock's UPDATE phase,
// synchronized with the compositor's VSync, so margin changes and surface
// commits happen in the same frame cycle.
func (b *Backend) slideMargin(target int, onDone func()) uint64 {
	b.animGen++
	gen := b.animGen
	b.animating = true
	start := b.margin
	t0 := time.Now()

	b.gtkWin.AddTickCallback(func(_ gtk.Widgetter, _ gdk.FrameClocker) bool {
		if b.animGen != gen {
			slog.Debug("anim cancelled", "gen", gen, "currentGen", b.animGen)
			return false
		}
		t := float64(time.Since(t0)) / float64(animDuration)
		if t >= 1.0 {
			b.margin = target
			gtk4layershell.SetMargin(b.gtkWin, gtk4layershell.LayerShellEdgeRight, target)
			b.invalidateSurface()
			slog.Debug("anim complete", "gen", gen, "margin", target)
			if onDone != nil {
				onDone()
			}
			return false
		}
		t = t * t * (3 - 2*t) // smoothstep
		b.margin = start + int(math.Round(float64(target-start)*t))
		gtk4layershell.SetMargin(b.gtkWin, gtk4layershell.LayerShellEdgeRight, b.margin)
		b.invalidateSurface()
		return true
	})

	return gen
}

// setMargin sets the right margin and keeps b.margin in sync.
func (b *Backend) setMargin(m int) {
	b.margin = m
	gtk4layershell.SetMargin(b.gtkWin, gtk4layershell.LayerShellEdgeRight, m)
}

// setOpacity sets the window opacity and keeps b.opacity in sync.
func (b *Backend) setOpacity(o float64) {
	b.opacity = o
	b.appWin.SetOpacity(o)
}

// fadeOpacity animates the window opacity from its current value to target over
// animDuration using smoothstep easing. onDone runs on the main thread when the
// animation completes. Shares animGen with slideMargin so the two cancel each
// other when a show interrupts a hide (or vice versa).
func (b *Backend) fadeOpacity(target float64, onDone func()) {
	b.animGen++
	gen := b.animGen
	b.animating = true
	start := b.opacity
	t0 := time.Now()

	b.gtkWin.AddTickCallback(func(_ gtk.Widgetter, _ gdk.FrameClocker) bool {
		if b.animGen != gen {
			slog.Debug("fade cancelled", "gen", gen, "currentGen", b.animGen)
			return false
		}
		t := float64(time.Since(t0)) / float64(animDuration)
		if t >= 1.0 {
			b.setOpacity(target)
			b.invalidateSurface()
			slog.Debug("fade complete", "gen", gen, "opacity", target)
			if onDone != nil {
				onDone()
			}
			return false
		}
		t = t * t * (3 - 2*t) // smoothstep
		b.setOpacity(start + (target-start)*t)
		b.invalidateSurface()
		return true
	})
}

// hasRightNeighbor reports whether any monitor sits to the right of the drawer's
// monitor (vertically overlapping its band). When true, hiding by sliding the
// surface rightward off the primary would bleed onto that monitor, so a fade is
// used instead. Recomputed per Show/Hide so monitor hotplug is handled.
func (b *Backend) hasRightNeighbor() bool {
	surface := b.appWin.Surface()
	if surface == nil {
		return false
	}
	display := gdk.DisplayGetDefault()
	if display == nil {
		return false
	}
	self := display.MonitorAtSurface(surface)
	if self == nil {
		return false
	}
	g := self.Geometry()
	rightEdge := g.X() + g.Width()
	monitors := display.Monitors()
	n := monitors.NItems()
	for i := uint(0); i < n; i++ {
		m, ok := monitors.Item(i).Cast().(*gdk.Monitor)
		if !ok || m == nil {
			continue
		}
		mg := m.Geometry()
		if mg.X() >= rightEdge && mg.Y() < g.Y()+g.Height() && mg.Y()+mg.Height() > g.Y() {
			return true
		}
	}
	return false
}

// invalidateSurface forces both widget-level and GDK surface-level
// invalidation to maximize the chance the compositor notices changes.
func (b *Backend) invalidateSurface() {
	b.gtkWin.QueueDraw()
	if s := b.appWin.Surface(); s != nil {
		gdk.BaseSurface(s).QueueRender()
	}
}
