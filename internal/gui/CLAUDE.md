# voltaire-gui (`internal/gui`) — Project Context for Claude

> **This file predates the 2.0 merge and has not been refreshed yet.** It was
> the standalone z13gui repo's CLAUDE.md, moved here unchanged so its reasoning
> survives the merge. The GTK, gamescope, focus, animation and CSS decisions
> below are all still accurate — that is the value of the file. The *names
> around them* are not:
>
> | It says | It is now |
> |---|---|
> | module `github.com/dahui/z13gui` | `github.com/dahui/voltaire/v2` (one module, two binaries) |
> | binary / command `z13gui` | `voltaire-gui` (built to `voltaire-gui/voltaire-gui`) |
> | sibling repo `z13ctl`, its `api/` at `api/v1.1.7` | same repo; `github.com/dahui/voltaire/api/v2` |
> | `internal/power` | `internal/limits` |
> | `internal/daemon` (the `Err` helper) | `internal/apiresult` |
> | `main.go` at the repo root | `voltaire-gui/main.go` |
> | socket `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock` | `…/voltaire/voltaire.sock` (legacy path still served through 2.x) |
> | config `~/.config/z13gui` | `~/.config/voltaire` (copied on first run) |
> | `Z13GUI_SCALE`, `Z13GUI_NO_GAMEPAD` | `VOLTAIRE_GUI_*` via `startup.GUIEnv` (old names honoured through 2.x) |
> | app ID `com.github.dahui.z13gui` | `io.github.dahui.Voltaire` |
> | `contrib/z13gui.{service,desktop}` | `contrib/systemd/user/voltaire-gui.service`, `contrib/voltaire-gui.desktop` |
> | `make test` greps out `internal/gui` | root Makefile's `HERMETIC_PKGS` does, for both binaries |
> | the 2×2 profile grid | three firmware buttons + one `Custom` button in the main view; the custom profiles are a **dropdown** inside the custom view (`profiles.go` + `dropdown.go` + `internal/profileui`) |
> | `GtkDropDown in gamescope` → "use buttons" | still true for GtkDropDown itself — but dropdowns now exist, drawn **inside our own surface** by the popup layer (`popup.go`, and "The in-surface popup layer" below) |
> | bare `api.SendTdpSet`/`SendFanCurveSet`/`SendUndervoltSet` everywhere | the `...For` variants, addressed per operation by `profileui.PlanEdit` (live vs stored) with a `SendProfileList` probe before every stored send |
>
> The root `CLAUDE.md` is authoritative for layout, build and release.

## What this project is

`z13gui` is a GTK4 Wayland layer-shell overlay drawer for controlling the 2025 ASUS ROG
Flow Z13 via the `z13ctl` daemon. It slides in from the right edge of the screen when the
Armoury Crate button (KEY_PROG3) is pressed. The daemon broadcasts `gui-toggle` events over
a subscribe socket; this GUI listens for them.

It has three display backends:
- **Layer-shell** (KDE/Wayland): margin-based slide animation
- **Overlay** (GNOME and any compositor without layer-shell): fullscreen
  transparent window, drawer right-aligned inside it, click-through everywhere else
- **Gamescope** (Steam Gaming Mode): X11 overlay via `STEAM_OVERLAY` atom

- Module: `github.com/dahui/z13gui`
- Binary: `z13gui`

## Companion project: z13ctl

The `z13ctl` daemon (module `github.com/dahui/z13ctl`) is a sibling repo.
Its `api/` submodule (`github.com/dahui/z13ctl/api`) is published at tag `api/v1.1.7`
on GitHub.

During local development, a `go.work` file in this repo (if present, gitignored) provides
the local override. In production the `go.mod` imports the published tag.

## Package layout

```
main.go                         GTK Application entry; ConnectActivate → gui.New(app)
                                Gamescope env detection + stale socket validation
Makefile                        build, install, lint, clean, snapshot, release
internal/gui/
  gui.go                        Window struct, backend selection, show/hide, subscribeLoop, theming
  backend.go                    Backend interface (Configure, WrapContent, Show, Hide)
  controls.go                   All GTK widget construction (drawer, views, bottom bar)
  mainwindow.go                 The full window: a real toplevel with a tab per page,
                                opened by the double-press gui-open-full event
  viewhost.go                   viewHost — the two things a view must be told about the
                                surface it was built into (how to leave, am I on screen)
  customview.go                 Custom profile view: the customView struct, TDP + undervolt
                                widgets, sync, and its focus list
  customsend.go                 How that view addresses its target: editPlan, the GTK-thread
                                snapshots (tdpRequest), probeStoredTarget, every save/reset
  profiles.go                   Its profile selector and inline name entry (on customView)
  fancurve.go                   The 8-point fan curve chart: mapping, hit test, Cairo drawing
  sync.go                       Daemon state sync, refreshState, the get-state telemetry poll,
                                and API send functions
  color.go                      colorInput widget + color picker view (math in internal/colorconv)
  errbar.go                     Error bar: reportError/clearError, the only user-facing error surface
  focus.go                      Focus widget adaptor (navigation logic in internal/focusgrid)
  layout.css                    Embedded structural CSS (touch targets, sizing) — PRIORITY_APPLICATION
  theme-default.css             Embedded theme template with @define-color placeholders — PRIORITY_USER
  theme-default.toml            Embedded default theme colors (rog-dark), used by --print-theme
internal/gui/fonts/
  font.go                       Embedded Inter font loading
internal/gui/layershell/
  layershell.go                 Layer-shell display backend (KDE/Wayland)
internal/gui/overlay/
  overlay.go                    Fullscreen transparent click-through backend
                                (GNOME/Mutter and anything without layer-shell)
internal/gui/gamepad/
  gamepad.go                    evdev gamepad reader; two-pass scan + EVIOCGRAB.
                                classify() is a PURE function over identity and
                                capabilities, and gamepad_test.go drives it — this
                                package IS in HERMETIC_PKGS (see below)
  steam.go                      Steam PID discovery; drives the hidraw blocker
internal/gui/gamepad/hidblocker/
  hidblocker.go                 BPF LSM blocker: blocks hidraw reads for specific PIDs
  blocker.bpf.c                 BPF C program (SEC("lsm/file_permission"), returns -EAGAIN)
  gen.go                        bpf2go generate directive
  blocker_x86_bpfel.go          Generated Go bindings (committed)
  blocker_x86_bpfel.o           Generated BPF ELF object (committed)
  hidblocker_test.go            Tests (skip without root/BPF LSM)
  vmlinux.h                     Generated kernel BTF header (gitignored, machine-specific)
internal/gui/gamescope/
  gamescope.go                  Gamescope X11 overlay backend (Steam Gaming Mode)
internal/theme/
  theme.go                      Colors struct (8 tokens), 15 built-in themes, accent variants
  parse.go                      theme.toml parsing; starts from DefaultColors so missing keys
                                inherit defaults — this is what keeps old theme.toml files working
                                when a new color token is added
  css.go                        @define-color generation from a Colors value
  config.go                     Config persistence (selected theme/accent)
  *_test.go                     Theme parsing, CSS generation, and built-in completeness tests
internal/power/                 Limits value: TDP/fan bounds + rules (mirrors z13ctl internal/cli)
internal/daemon/                Err(handled, err): collapses an api result pair into one error
internal/focusgrid/             Gamepad focus navigation: row/col/section index math
internal/keyrepeat/             Tracker: which held direction owns the gamepad auto-repeat
internal/colorconv/             hex <-> HSL/RGB conversion and colour validation
internal/lighting/              RGB mode resolution, per-mode controls, defaults
internal/uiscale/               Gamescope UI scale factor (cannot live in the cgo package)
internal/panelgeom/             Overlay backend panel rectangle + slide interpolation
internal/startup/               CLI arg scanning + split-level slog handler
internal/togglegate/            Debounce helper for duplicate gui-toggle bursts
contrib/
  z13gui.service                systemd user service (EnvironmentFile for gamescope-session)
  z13gui.desktop                Desktop entry
```

## Key architectural decisions

- **Never call into gtk4-layer-shell without checking `IsSupported()` first.**
  `zwlr_layer_shell_v1` is a **wlroots** extension, not part of `wayland-protocols`:
  KWin, Hyprland and Sway implement it, GNOME's Mutter never has and has no plan to.
  Installing `gtk4-layer-shell` does not help — that is the *client* library; the
  protocol has to come from the compositor. The failure is silent, which is what
  made issue #16 hard: `gtk_layer_init_for_window` logs one `G_LOG_LEVEL_WARNING`
  and returns, then every `SetLayer`/`SetAnchor`/`SetMargin`/`SetMonitor`/
  `SetKeyboardMode` warns once and no-ops. Nothing aborts, so the drawer came up
  as an unanchored window — and since the anchors were the only thing supplying a
  height, it collapsed to a ~320px box in the middle of the screen. `gui.go`'s
  `layerShellUsable()` gates this, and checks the GDK backend *before* calling
  `IsSupported()`, which asserts on a non-Wayland display.
- **Layer-shell** (KDE): `github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell`
  (NOT `gtklayershell` which is GTK3). pkg-config name: `gtk4-layer-shell-0`.
- **Anchor**: right + top + bottom edges. Top/bottom margins set to 5% of screen height
  on realize. The surface is pinned to its monitor via `SetMonitor` (helps wlroots
  compositors clip overflow; KWin does NOT clip, see conditional fade below).
- **Keyboard mode**: `LayerShellKeyboardModeOnDemand` — gets focus when visible.
- **Animation**: layer-shell right-margin animation (`gtk4layershell.SetMargin`).
  `margin=0` → on-screen; `margin=-320` → off-screen to the right.
  Avoids GTK Revealer which causes pixman errors and smearing artifacts in Wayland.
- **Window visibility**: window is kept `SetVisible(true)` at all times after creation.
  It's "hidden" by setting margin = -(width-1) (off-screen) and opacity = 0, not by
  destroying/hiding the surface. This prevents the ghost-surface artifact that KDE Plasma
  shows when remapping a surface. The 1px margin keeps the surface in KWin's composited
  output for damage tracking; opacity 0 makes the sliver invisible to the user.
- **Width**: `SetSizeRequest(320, -1)`. Height is natural (content-driven, scrolled).
- **Show/hide animation**: smoothstep easing via `AddTickCallback` (VSync-synced),
  with a shared `animGen` generation counter so a show cancels an in-flight hide
  (and vice versa). Two paths, chosen per Show/Hide by `hasRightNeighbor()`:
  - **No monitor to the right** → slide the right margin (`slideMargin`). Show sets
    opacity=1 then slides in; Hide slides out then sets opacity=0 (hides the 1px
    sliver on the primary's own right edge).
  - **A monitor to the right** → fade in place (`fadeOpacity`) at margin 0, fully on
    the primary, then park the transparent surface off-screen. A rightward slide
    would otherwise bleed onto that monitor because KWin doesn't clip layer-surface
    overflow to the assigned output. `Backend.margin`/`Backend.opacity` track current
    state; use `setMargin`/`setOpacity` to keep them in sync.
- **Single instance / activate guard**: `gtk.NewApplication("com.github.dahui.z13gui", 0)`
  registers the app on the session bus, so launching the binary a second time does not
  start a second process — GApplication forwards `activate` to the running instance and
  the new process exits. `main.go` therefore holds the `*gui.Window` and only calls
  `gui.New` on first activation; re-activation calls `Toggle()` instead. Without that
  guard each re-activation builds an entire second drawer (its own layer surface,
  subscribe loop, gamepad reader, telemetry poller) overlapping the first, and both stay
  live and interactive. One click on `contrib/z13gui.desktop` while the user service is
  running is enough to trigger it. Diagnostic: two `drawer initialized` log lines under
  a single PID means the guard is missing or broken.
- **State source of truth**: daemon is the source of truth. On show, `api.SendGetState()`
  is called and `syncState()` updates widgets. Widget signals are suppressed during sync
  via `Window.syncing bool`.
- **Error surface** (`errbar.go`): every daemon call reports failures through
  `w.reportError(op, err)` and clears on success with `clearError()`/`clearErrorAsync()`.
  The bar is appended to `outer` between `viewStack` and the bottom bar, so one instance
  covers all four views in both backends. Its dismiss button is in every view's focus
  grid via `errBarFocusItem()` at `errBarRow` — without that a controller cannot
  dismiss an error at all. `reportError` drops the message when the drawer is already
  closed, so a call still in flight at close does not leave the bar up for the next
  open; the journal still has it. **Never drop a daemon error into `slog` alone** —
  that is what made z13ctl issue #14 look like a dead button for weeks. `reportError` is
  safe from any goroutine (it marshals via `glib.IdleAdd`) and logs internally, so call
  sites should not also `slog.Warn`.
- **`handled == false` means the daemon is not running, and it is not an error.**
  Every `api.Send*` returns `(handled bool, err error)`; when the socket dial fails
  it returns `handled=false, err=nil`, because nothing was sent. **Never test `err`
  alone** — wrap every call in `daemon.Err(api.SendX(...))`, which takes the result
  pair directly so a site cannot read one and forget the other. All thirteen call
  sites used to discard `handled`, so with the daemon stopped every operation took
  its success path: `Save TDP` cleared the error bar, logged "custom TDP saved" and
  left the typed values on screen. That is z13ctl issue #14's dead-button failure
  rebuilt one layer up, and it defeated the error bar entirely.
  `internal/daemon`'s contract test dials a temp `XDG_RUNTIME_DIR` to pin the api
  convention rather than trusting its doc comment.
  - The telemetry poll is the one deliberate exception, and says so in a comment:
    it is a background poll, and reporting it every second would overwrite
    whatever error the user was reading.
- **Daemon calls must not run on the GTK thread**: `api` commands carry a 10s deadline
  (`commandTimeout`, api v1.1.7), so an inline call freezes the drawer for up to 10s
  against a wedged daemon. Read widget values on the main thread, then do the socket
  round-trip in a goroutine — see `sendApply()` in `sync.go` for the pattern.
- **Decisions live outside `internal/gui`; widgets live inside it.** `internal/gui`
  needs CGO + GTK4 headers, so `make test` cannot even compile it — anything left in
  there is permanently unverifiable. Every rule, calculation or classification
  belongs in a pure package (`power`, `focusgrid`, `colorconv`, `lighting`,
  `uiscale`, `startup`, `togglegate`); the GTK files are thin adaptors that read
  widgets, call out, and apply the answer. Extracting logic this way has caught
  seven real bugs so far, none of which were found by reading the code.
  - `make test` derives its package list (`HERMETIC_PKGS`) rather than
    hand-listing it, so a new pure package is picked up automatically — nothing
    to remember.
  - **The boundary is cgo/GTK, not the directory name, and the two must not be
    confused.** `internal/gui/gamepad` and `internal/gui/gamepad/hidblocker` are
    an evdev reader and a cilium/ebpf loader that merely *live* under
    `internal/gui`; both compile with `CGO_ENABLED=0` and both are in
    `HERMETIC_PKGS`. The exclusion was a substring match on the path, so it had
    been swallowing them by location rather than by rule — which is how
    `hidblocker_test.go` went unexecuted from the day it was written, and how a
    device-classification fix ported from z13gui arrived carrying 280 lines of
    tests that would never have run. Keep the list derived from the rule; if a
    package here compiles cgo-free, it belongs in the run.
  - **Never read or write a GTK widget from a goroutine.** GTK is not thread-safe;
    this is undefined behaviour, not a stale read. Snapshot widget values on the
    main thread into plain data, then do the socket call in the goroutine — see
    `readTdpRequest`/`tdpRequest.send` in `customsend.go` and `sendApply` in
    `sync.go`.
    Come back to the main thread with `glib.IdleAdd`.
  - **`Window.visible` is an `atomic.Bool`, and it is the only `Window` field any
    goroutine may touch.** The gamepad reader gates every event on it, so a plain
    bool there is a data race — and because `internal/gui` is excluded from
    `go test -race`, nothing would ever report it. Anything else a goroutine needs
    must be passed to it as plain data, not read off `Window`.
- **Ordering matters for anything a goroutine applies to the outside world.**
  `show`/`hide` issue their gamepad grab and release from separate goroutines, so
  the two race: a grab landing after a hide leaves every controller exclusively
  grabbed with nothing on screen and no input reaching the game, and a release
  landing after a re-show (easy inside the gamescope path's deliberate 200ms delay)
  hands the game the same D-pad presses navigating the drawer. `Window.grabGen` is
  incremented on the GTK thread and passed to `gamepad.Reader.SetGrabbed(seq, grab)`,
  which drops superseded requests. Add a sequence to any similar pair.
- **Device limits are a value, not constants.** `power.Limits` holds the TDP range,
  fan floor, temperature axis and stock PPT table; `Window.limits` is initialised to
  `power.DefaultLimits()` (the Z13's values). **No TDP or fan bound may be hardcoded
  in `internal/gui`** — derive it from `w.limits` / `fc.limits()`, because z13ctl is
  being extended to devices with different envelopes. The design brief for the
  eventual daemon-served limits is in z13ctl's `.claude/plans/device-limits-api.md`;
  when it lands, only where `Window.limits` is assigned changes.
  - Presentation policy stays derived, not fixed: `BasicSliderMax()` is
    `TDPMaxSafe - 5`, not a literal 70, because 70 is meaningless on a device whose
    safe max is 54.
  - `Sanitized()` replaces zero fields with defaults, for the day a daemon older
    than the client omits one, **and enforces the ordering/width invariants** that
    a zero check cannot see: `TDPMin < TDPMaxSafe <= TDPMaxForced`, and a
    temperature axis at least `CurvePoints-1` wide. Each group falls back whole,
    because an inconsistent triple does not say which member is wrong. Without the
    width check a narrow axis makes `EnforceCurve` emit points below `TempMin` that
    the daemon rejects, and `TempMin == TempMax` divides by zero in the editor's
    coordinate mapping. The invariant was asserted in the tests but not enforced,
    so it held only for limits compiled in — exactly what breaks when the daemon
    starts serving them. `HighTDPMinPWM` is exempt from the zero check — 0
    legitimately means "no fan floor on this device" — but is clamped to `PWMMax`.
  - `power.Curve` is a fixed `[8]` array. If a device ever needs a different point
    count it becomes a slice and the compile-time length guarantee is lost.
- **High-TDP fan floor**: while sustained PL1 exceeds 75W the daemon rejects any fan
  curve point below 204 PWM (80%) and refuses a fan reset outright. `fanFloorPWM()`
  derives this from applied daemon state (not slider position); `enforceConstraints`
  clamps drags to it, `fanCurveEditor.draw` renders the floor line, and `resetFanBtn` is
  desensitized with a `.block-note` beneath the Reset row pointing at Reset TDP
  (never a tooltip — see "Hints and block notes" below). Both the threshold and
  the floor come from `w.limits`, not from literals.
- **Basic vs advanced TDP view**: basic mode is one slider applying a single value to
  all three limits, capped at 70W. `power.NeedsAdvanced` decides whether a state can be
  shown there; `syncCustomView` force-checks the Advanced box when it cannot. Without
  that the slider clamps, the label misreports the hardware, and a save sends the
  clamped value — silently lowering the user's power limit.
- **Subscribe loop**: background goroutine, exponential backoff reconnect, dispatches
  `Toggle()` onto the GTK main thread via `glib.TimeoutAdd(0, ...)` followed by
  `MainContextDefault().Wakeup()` (the wakeup is required — the loop may deliver an
  event while the main context is blocked).
- **Toggle debounce**: on some firmware revisions a single Armoury Crate press reaches
  the GUI as two `gui-toggle` events in the same instant, which cancel each other out
  (an open drawer gets hide → show and appears stuck open). `subscribeLoop` gates events
  through `togglegate.Accept(last, now, daemonToggleDebounce)` (leading edge: keep the
  first event of a burst, drop the rest; the window does not extend on suppression).
  **The window is 50ms and must stay well under ~120ms.** It is a duplicate filter, not
  an animation rate limiter: measured human tapping bottoms out near 129ms between
  presses, so a larger window discards deliberate input — the original 250ms swallowed
  38% of presses in a 96-event sample from real use. If a future change wants to rate
  limit the 200ms slide animation, do it in `Toggle()`/the backend, not here.
  The debounce timestamp is a **local variable** in `subscribeLoop`, not a `Window`
  field: every other piece of `Window` state is main-thread-owned, so keeping this one
  out of the struct makes it unreachable from the main thread and removes any chance of
  an unsynchronized read. Do not promote it to a field — reading it from `show()`/
  `hide()` would be a data race, and `internal/gui` is not covered by `go test -race`.
- **Anything painted rather than styled must apply the scale and theme itself.**
  The fan curve chart is Cairo, so it reads neither the `@voltaire-*` tokens nor the
  gamescope CSS scaling. `Backend.Scale()` returns 1.0 on layer-shell and the
  resolution factor under gamescope; `Window.colors` holds the active palette
  alongside the CSS built from it. Every dimension in `fanCurveEditor.draw` and
  `hitTest` multiplies by `fc.scale()` — `.fan-curve-area` grew with resolution
  while the 6px points and 20px grab radius stayed at 1x, so the drag targets got
  harder to hit the larger the output. `applyTheme`/`applyCustomAccent` call
  `redrawFanCurve()`, since swapping the CSS provider does not repaint Cairo.
- **CSS architecture**:
  - `layout.css` → `STYLE_PROVIDER_PRIORITY_APPLICATION` (structural, not overridable)
  - `theme-default.css` → `STYLE_PROVIDER_PRIORITY_USER` (colors, user-overridable)
  - No `hexpand: true` in CSS — use `widget.SetHExpand(true)` in Go instead.
  - No `box-shadow` on `.drawer` — it causes smearing outside the widget clip region
    during slide animations in Wayland Vulkan rendering.
  - No `AddMark()` on scales — scale marks inside an animated context cause GTK
    `GtkGizmo` allocation warnings and pixman errors.
  - CSS class hierarchy for text labels in custom view:
    - `.section-label` — section headers ("TDP", "UNDERVOLT", "FAN CURVE"): 11px, bold, letter-spaced, dim
    - `.scale-name` — slider name labels ("PL1 (SPL)", "CPU Curve Optimizer"): 10px, bold, no letter-spacing, dim
    - `.scale-value` — slider value readouts ("50 W", "CPU CO: -20"): 10px, normal weight, bright
    - `.error-bar` / `.error-text` / `.error-dismiss` — error surface; colored via the
      `@z13-error` theme token, as is `.tdp-warning`
- **Profile buttons (main view)**: buttons (`gtk.Button`), stored in
  `w.profileBtns map[string]*gtk.Button`. The custom view's profile selector and
  the autoswitch targets are `dropdown`s (`dropdown.go`) over the in-surface
  popup layer — never GtkDropDown, whose popover is a separate surface.
- **Focus-loss dismiss** (layer-shell): `EventControllerMotion` tracks `pointerInside`
  on the backend. On `notify::is-active` focus loss: if within 500ms of Show, ignored
  (compositor settle time for keyboard-mode transition). If pointer is inside, the drop
  is spurious (KDE Plasma briefly drops focus during keyboard-mode transitions) → ignored.
  If pointer is outside, user clicked elsewhere → dismiss after 200ms confirmation delay.
  Do NOT add a `focusedSinceShow` guard — it causes first-show dismiss regression on KDE
  where the compositor drops focus during keyboard-mode transition and never re-grants it.
  Escape key also dismisses in both backends.
- **GTK_A11Y=none**: set in `main.go` and `contrib/z13gui.service`. Disables GTK4
  AT-SPI accessibility bridge, which sends D-Bus events on every widget state change.
  Under systemd (especially gamescope sessions), the AT-SPI bus may be unavailable,
  causing D-Bus timeouts that block GTK initialization.

## Gamescope backend (`internal/gui/gamescope/gamescope.go`)

The gamescope backend renders z13gui as an X11 overlay in Steam Gaming Mode.

- **Overlay type**: `STEAM_OVERLAY` atom (z-pos 3, interactive with input routing).
  NOT `GAMESCOPE_EXTERNAL_OVERLAY` (z-pos 2, display-only, no input).
- **Visibility**: opacity-based (`_NET_WM_WINDOW_OPACITY`). Window stays mapped always.
- **Input**: keyboard-only X11 grab (`XGrabKeyboard`) + `STEAM_INPUT_FOCUS` atom.
  `XGrabPointer` was removed because its core X11 event mask interferes with XI2
  touch delivery. STEAM_INPUT_FOCUS handles pointer/touch routing natively.
- **Scaling**: resolution-based CSS scaling (`outputWidth / 1707`). Reference 1707 = 2560/1.5
  (matches KDE 150% at Z13 native resolution). `Z13GUI_SCALE` env var overrides.
  GDK_SCALE CANNOT be used — causes double scaling (GTK + gamescope scaler).
- **Layout**: fullscreen window → horizontal box (backdrop + right-aligned panel).
  Panel has 5% top/bottom margins, scaled drawer width.
- **GTK popups don't work**: GTK4 popovers/dropdowns create separate X11 windows
  that gamescope doesn't composite as input-receiving windows. Solved via view
  switching for the large pickers (see below) and the in-surface popup layer for
  dropdowns and hints (see "The in-surface popup layer").

### View switching

`buildContent()` wraps content in a `gtk.Stack` with 5 pages, in **both** backends:
- `"main"` — normal drawer (profiles, RGB, battery, etc.)
- `"custom"` — custom profile view (TDP, fan curve, undervolt, telemetry)
- `"dashboard"` — telemetry charts over the daemon's sample history (`dashboard.go`)
- `"theme"` — theme picker (radio buttons + accent dots)
- `"color"` — HSL color picker (H/S/L sliders + presets + preview)

Bottom bar stays visible across all views. `hide()` resets to "main".
Every page but `"main"` is lazy-built on first navigation.

**There are no popovers left anywhere.** The stack replaced them in both modes
(`e19f76f`, which added gamepad support). `grep Popover internal/` returns nothing,
and it should stay that way — but the *reasoning* has moved on: the drawer now has
popups again, drawn inside its own surface by the popup layer, which reuses the
one `focusgrid` mechanism (a suspended-frame stack, not a second system) and is
visible in Gaming Mode precisely because it is not a separate window. What stays
forbidden is any GTK-native transient — `GtkPopover`, `GtkDropDown`,
`GtkMenuButton`, tooltips — because each owns a `GdkSurface`. See "The in-surface
popup layer" below.

### The in-surface popup layer (`popup.go`, `dropdown.go`, `hint.go`, `internal/popupgeom`)

`buildContent` wraps the view stack + error bar + bottom bar in a `GtkOverlay`
whose overlay children are the popup **scrim**, the popup **surface** (dropdown
lists), and the anchored **hint** label. All placement decisions live in
`internal/popupgeom` (pure, tested — the same split `panelgeom` has with the
overlay backend); the `get-child-position` handler only measures widgets and
applies the returned rect.

Why OS-level popups are a dead end, so nobody re-chases it:

- **GTK4 gives every transient its own `GdkSurface`.** `gtk_popover_realize()`
  calls `gdk_surface_new_popup()` unconditionally; `GtkDropDown` wraps a popover;
  `GtkTooltipWindow` implements `GtkNative` too. GTK3's
  `gtk_popover_set_constrain_to` was removed — there is no in-window mode.
- **gamescope's compositing slots are all closed to us.** Its override-redirect
  plane (the one real dropdowns land in) is PID-matched to the focused *game*,
  and `GetPossibleFocusWindows()` skips windows flagged `isOverlay`. Tagging a
  popup `STEAM_OVERLAY` ourselves lands it in the notification slot: painted
  upscaled to fullscreen, receiving no input.
- **gamescope's layer-shell support (June 2024+) is not a way out either**: it
  auto-tags layer surfaces `isExternalOverlay` — z-pos 2, display-only, no input
  routing — which is exactly why this backend chose `STEAM_OVERLAY`.
- Every shipping gamescope overlay (HHD, Decky, mangoapp) draws popups inside its
  own surface. HHD is not immune "because it is web": its menus are DOM nodes
  with a z-index in one Electron surface, and Chromium's native `<select>` — a
  real separate window — would break there exactly as `GtkDropDown` breaks here.

Load-bearing implementation facts:

- **gotk4 v0.3.1's `get-child-position` marshaller dereferences the returned
  rectangle before consulting `ok`** (`gtk/v4/gtk_export.go`), so returning
  `(nil, false)` — the natural "use the default position" — segfaults inside a C
  callback with a useless stack trace. Every return path produces a non-nil rect;
  hidden children get a zero-size rect. Never "clean this up".
- **No setters inside the position handler** (it runs during allocation; a setter
  loops) and **never `SetMeasureOverlay(child, true)`** — overlay children not
  contributing to measurement is what keeps a popup from widening the 320px panel.
- The overlay's main child is a Box, never a `ScrolledWindow` (the GTK docs place
  overlays relative to a scrolled main child's *contents*).
- The popup surface and hint carry **`.drawer`**, so a user's verbatim
  `theme.css` styles them with zero edits; option rows carry `.btn-group`, which
  is already themed and already scaled under gamescope. Popup rules that override
  `.drawer` properties must live in `theme-default.css` (PRIORITY_USER beats
  layout.css's PRIORITY_APPLICATION at any specificity).
- The scrim's capture-phase `GestureClick` dismisses; it also blocks scrolling
  under the popup for free (it is a sibling of the view's scroller). Popups
  contain only `gtk.Button`s — a `CheckButton`/`Switch` in a popup would need
  `addTouchActivate` (bubble-phase gestures fail for touch under XWayland).
- **Focus**: `openPopup` pushes the current focus list onto `w.focusStack`;
  `closePopup` pops it and re-anchors via `focusgrid.Restore` (the item that
  opened the popup may have been desensitized while it was open).
  `activeScroll()` returns the popup's scroller while one is open, which is what
  makes gamepad navigation of a long list scroll it. Every view switch and
  `hide()` call `closePopup()`; `swapFocusList` clears the stack defensively and
  logs if it was non-empty.
- **Syncs are frozen while a popup is open** (`syncsSuppressed` in `syncState`,
  `syncCustomView`, and `refreshState`'s idle closure): a state refresh would
  relabel or hide the anchor and tear the list out from under the pointer.
  `closePopup` runs the deferred refresh.
- Arrow keys stay blocked (`gui.go` capture-phase key controller), so popups are
  not keyboard-navigable — consistent with the rest of the drawer; do not "fix"
  this, it would re-enable GTK's radio auto-activation. Escape lives in that
  same capture controller (capture reliably precedes the backends' bubble-phase
  Escape handlers; two same-phase controllers have no ordering contract).
- **The window's motion controller ignores motion at an unchanged position, and
  that guard is what keeps gamepad navigation alive.** GTK synthesizes a motion
  event at the *current* pointer position whenever the widget under it changes,
  and every D-pad press changes the layout (the anchored hint appears,
  `ensureVisible` scrolls). Without the guard each press was followed by a
  synthetic motion that `hideGamepadFocus` read as "the user reached for the
  mouse", so the next press restarted at the first item and focus could never
  move past it — D-pad navigation was dead on arrival, while the gamepad log
  still showed a focus line per press. Real pointer motion always carries new
  coordinates. Any future work that shows or hides a widget from the focus path
  depends on this.

### Hints and block notes (`hint.go`, `blockNote`)

`SetTooltipText` is banned — `grep SetTooltipText internal/gui` must stay empty.
Tooltips are separate surfaces (invisible in gamescope), hover-only (touch never
sees them), and unreachable on a controller for insensitive widgets (the focus
grid deliberately skips those). Two replacements, split by purpose:

- **Hints** (`w.setHint(widget, text)`): descriptions of *usable* controls.
  Shown as the popup layer's third overlay child after a 500ms pointer dwell, or
  immediately on gamepad focus — one help path on all three backends, so KDE and
  gamescope cannot diverge. Entries in `w.hints` are never removed; hint only
  permanent widgets (a hinted transient would leave a stale map entry).
  **A hint must never outlive the pointer being on its anchor**, and the
  anchor's own `Leave` is not sufficient to guarantee that. Two ways it never
  arrives, both found on hardware: a widget desensitized under a stationary
  pointer (click Activate and it greys out beneath the cursor) stops receiving
  events, and a crossing is swallowed while the popup scrim covers the anchor.
  Either one left the hint sitting **over the `.block-note` explaining the
  control it was anchored to** — the one moment that note matters. Hence both
  `notify::sensitive`/`notify::visible` in `setHint` and the position-based
  `pruneHintAt` backstop on the window's motion controller.
- **Block notes** (`blockNote()`/`setBlockNote()`): refusal reasons for
  *desensitized* controls, as in-flow labels beneath the control (the pattern
  `editorNote` and `tdpWarningLabel` established). A focus-triggered hint can
  never fire on an insensitive widget, so a refusal in a hint would be unreadable
  by exactly the users staring at the dead button.

That conversion left CSS behind, which is worth knowing about because it hid a real
regression for months: `popover.z13-popover` rules (12 of them) and
`.bottom-bar menubutton > button` outlived the widgets they selected, and the
latter was the theme-picker button's only colour styling — so it silently fell back
to stock GTK colours and stopped following the theme. Both are now removed, with
`.bottom-bar button` / `.view-back-btn` styled directly. **When a widget type
changes, grep the CSS for its element selector**: a rule that no longer matches
fails silently and looks like a theming gap rather than dead code.

### Focus coordinates come from `focusgrid.Builder`; nothing hand-numbers a row

All four focus lists (main, custom, theme, colour) declare a layout and let
`internal/focusgrid/builder.go` compute the coordinates. No view increments a
`row` variable between appends any more, and no new one should. The Builder
landed pure and tested *before* any widget changed — that ordering is the
mitigation for the plan's risk #4 — and four parity tests pin each view's
coordinates against what the hand-numbered code produced.

The main view's conversion was verified against the **running drawer**: the
`-d` dump of its focus list is byte-identical to the pre-conversion one, 42
items including the error-bar sentinel (43 since the dashboard button joined
the footer).

**`VOLTAIRE_GUI_DUMP_FOCUS=1` dumps all five lists at startup**, which is what
made that check available for the other four. Every view but `"main"` is built
on first navigation, so their lists previously existed only after a person
tapped a button — leaving them verified by the parity tests in
`internal/focusgrid` and by nothing that could confirm the widgets those tests
describe are the widgets actually built. `dumpAllFocusLists` calls the build
halves of the `show*View` functions (not the functions themselves —
`showCustomView` resolves an edit target and starts a poll, neither of which a
dump should do), so:

```sh
VOLTAIRE_GUI_DUMP_FOCUS=1 voltaire-gui -d 2>&1 | grep 'focus list built'
```

is a five-line fingerprint of the whole drawer's gamepad navigation. Capture it
before a refactor and diff after — that is how the view split below was checked.

Two things the hand-numbered form got wrong that the Builder cannot:

- A grid's height was written twice — `modeBase + i/3` in the loop and
  `row = modeBase + 1` after it. Those agree only while `modeOrder` holds
  exactly six entries. A seventh mode would have put a button on the row the
  next section claims, so D-pad down from the grid reaches the wrong control
  and nothing looks wrong on screen. `Grid` advances by the ceiling.
- Every coordinate assumes the panel stacks downward, so a top- or bottom-edge
  quickbar needs all of them transposed. `Orientation` applies that once, at
  the end; `Horizontal` is *defined* as the transpose of `Vertical` rather than
  as a second set of rules, and a property test asserts it over a non-trivial
  layout. Nothing uses `Horizontal` yet — see the edge note below.

`logFocusList` dumps each list at Debug as `row:col:section`. It is the only
way to check that a layout change moved what it meant to and nothing else,
since these lists live where no test can reach them.

### The drawer moves between the left and right edges, and no further

`gui.toml`'s `[quickbar] edge` picks the screen edge. `panelgeom.Edge` carries
it, all three backends take it, and the geometry is pure and table-tested:
`Panel` aligns the rect, `HiddenX` says which way "away" is, and `HasNeighbor`
answers whether sliding off that edge would bleed onto another monitor.

That last one is the reason to be careful here. The layer-shell backend fades
in place instead of sliding when another output sits beyond the edge, because
KWin does not clip a layer surface's overflow — and the check asked
exclusively about the *right* until the drawer could move. An assumption like
that survives a move silently and produces a drawer bleeding onto the monitor
it used to be nowhere near. Every anchor and margin write now goes through
`shellEdge()` for the same reason: there must be no path that relocates the
panel while leaving the animation driving the opposite side.

Gamescope has no rectangle to compute — it composites the whole fullscreen
window — so its edge is the append order of backdrop and panel around the
expanding spacer.

**Top and bottom are refused, and `ParseEdge` says why.** They are not an
anchor change: the drawer is a fixed-width column of stacked sections, so a
horizontal edge means laying every section out along the other axis — a
different panel, not a moved one. `focusgrid.Horizontal` is already written and
tested for the day that content work happens; until then a user who writes
`edge = "top"` gets a warning naming the reason and the default, rather than
"unknown edge" or a silent no-op.

### The window split: views own their widgets, `Window` owns the process

`Window` was a ~200-line struct of widget pointers for every view at once. M4
moves them into per-view structs, each with its own build, sync and focus list,
so the full window can host a view without a second implementation of it.

All seven are done — `errBarView` (`errbar.go`), `colorView`
(`colorview.go`), `themeView` (`themeview.go`), `lightingView`
(`lightingview.go`), `profileSection` and `autoswitchSection`
(`mainprofile.go`), `dashboardView` (`dashboard.go`, built in this shape from
the start), and `customView` (`customview.go`). What remains on `Window` is
process-level: the battery slider, the two footer toggles, the header label,
the palette, and the poll generation counters.

Five rules the moves follow:

- **A nil view pointer is the built-yet test.** `showThemeView` checks
  `w.themeView == nil` where it used to check `w.themeScroll == nil`. One field
  means one thing, and there is no way to get a half-built view.
- **State that outlives a view stays on `Window`.** The palette
  (`colors`, `themeProvider`, `isCustomTheme`, `customColors`, `customAccents`)
  is resolved by `loadCSS` at startup, long before the theme picker is built,
  and `applyTheme` swaps a display-wide provider. Moving it into `themeView`
  would mean the theme could not be applied until the user had opened the
  picker. `themeView` is the chooser, not the theme engine.
- **The call surface does not churn.** `w.reportError(...)` still exists at ~50
  sites and delegates to `errView`; only the *state* moved. A refactor whose
  point is to shrink one struct should not also rewrite every caller.
- **A field that serves every view stays on `Window`, even when it looks like
  it belongs to one.** `headerTelemetry` was grouped under the custom view's
  fields and is the *header's* label, shown on all five; `telemetryGen` and
  `telemetryBusy` drive the get-state poll, which runs for as long as the
  drawer is visible whichever view is showing (the dashboard's own loop is
  separate because it reads `telemetry-history`). Moving either into
  `customView` would have made the header stop updating the moment that view
  was not built. The tell is the caller: `show()` starts the poll, and
  `showCustomView` only restarts it.
- **A `Window`-level entry point nil-guards its section, and that guard is
  load-bearing, not defensive.** `controls.Resolve` genuinely drops a section on
  a device without the capability, so `w.lighting` can be nil on a real machine
  — `syncLightingSection`, `syncModeVis`, `updateSwatches`, `sendApply` and
  `queueApply` are all reachable while it is. One guard per entry point replaced
  a dozen scattered per-widget nil checks (`if w.color1 != nil`, `if
  w.brightScale != nil`, …), which is the same consolidation `errBarFocusItem`
  got: the section either exists or it does not, and that is one question.

**Zero behaviour change is checked, not asserted.** Two instruments, because
they cover different halves:

- **Build paths**: all five focus lists byte-identical to the pre-split baseline
  after *each* view moved, via `VOLTAIRE_GUI_DUMP_FOCUS` above.
- **Sync paths**: the focus dump never calls a `sync*` function, so those are
  exercised by driving a real `state-changed` — `voltaire profile --set quiet`
  then back — against a running drawer with `-d`. The subscribe loop's
  `refreshState` runs `syncCustomView`, `syncProfiles`, `syncAutoswitch`,
  `syncLightingSection`, `syncBattery` and `updateHeader` **whether or not the
  drawer is visible**, so this needs no interaction. Two clean refreshes with no
  panic is the check.

The linter earns its keep too, catching what a mechanical move strands:
`Window.updateColorPreview` went dead the moment its last caller moved into
`colorView`, and `Window.applyTimer` and `Window.tab` the moment `lightingView`
took them. Run `make lint` after every view, not just at the end — an unused
field is the signal that something was copied rather than moved.

**The custom view is the one view that is four files, and the seams are
subjects rather than size.** It is twice the next largest, so a single file was
1000+ lines; but "split it in half" would have put the cut somewhere arbitrary.
The cuts follow what the code is *about*:

- `customview.go` — the struct, the widget tree, `sync`, the focus list.
- `customsend.go` — how the view addresses its **target**: `editPlan`, the
  GTK-thread snapshots (`tdpRequest`, `readFanCurve`), `probeStoredTarget`, and
  every save/reset/delete. Every function in it obeys the same two rules, which
  is what makes it a file rather than a pile: snapshot on the main thread, and
  gate a stored send on the probe.
- `profiles.go` — the selector and inline name entry, already a self-contained
  block with its own rules in `internal/profileui`.
- `fancurve.go` — the chart. A widget, not a view: it holds no daemon state and
  its constraint rules are all in `internal/limits`. Renamed from `tdp.go`,
  which stopped describing what was left in it.

`fanCurveEditor` now points at `*customView` rather than `*Window`, which is
what it always wanted: it reads the target's `editorFloorPL1` on every draw,
and the floor is a property of the profile being edited, not of the drawer.

Two things the move surfaced, both worth stating because they are the kind of
thing a move is good at finding and nothing else is:

- `tdpWarningLabel` was a `Window` field written once at construction and never
  read again — the identical warning label two blocks below it (`uvWarn`) was
  already a local. It is a local now. A field that outlives its only use is a
  claim that something will read it later, and nothing did.
- `editProfile` was seeded in the `Window` literal, which the lazily-built view
  cannot inherit. `buildCustomView` seeds it instead, and `showCustomView`
  resolves the running target *after* the build rather than before it — the end
  state before the view is ever shown is identical, because `sync` runs ahead of
  `SetVisibleChildName`.

### The full window (`mainwindow.go`, `internal/mainwin`)

A double press of the hardware button emits `gui-open-full` *in addition to*
the `gui-toggle` the first press already sent, and this is its consumer: the
quickbar the first press opened is hidden and a real toplevel takes its place.
The brief flash is the accepted trade — a daemon that waited to see whether a
second press was coming would put 400 ms onto every press on the machine.

**It is a different surface, not a wider drawer.** The drawer is a 320px column
reached in a hurry and it already shows every control; the one thing it cannot
show is a chart at a size worth reading, which is why the telemetry tab leads.

**Both surfaces host the same view implementations — a second instance, never a
second copy.** That is what the window split was for, and `dashboardView`'s doc
comment has said so since it was written. Three things had to become
per-instance before it was true:

- **Constructors return the view.** `buildDashboardView` assigned `w.dashboard`
  and returned a `*gtk.Box`, so a second call clobbered the first. They are
  `newDashboardView`/`newCustomView` now, and the caller stores what it gets —
  which is also how `colorView` and `themeView` already worked.
- **Focus lists moved onto the view.** `w.customFocusItems` and
  `w.dashboardFocusItems` were `Window` fields; two instances would have fought
  over them. `colorView.focusItems` was already the pattern.
- **`viewHost` carries what differs between surfaces**, and it is deliberately
  only three fields, because only three things differ. `back` is nil in the
  window (the tab bar is the navigation), which is also what suppresses the
  view's own header — so the window's focus grids are the drawer's minus the
  `nav` row, which is exactly what the dump shows. `current` is "my surface is
  open and showing me", asked instead of reading `w.viewStack` — a view that
  consults the drawer's stack is a view that cannot live anywhere else. `errBar`
  is the surface's own error strip.

**One error bar per surface, and reports fan out to all of them.** The drawer's
bar is hidden whenever the window is up, so a failure reported only to it would
be invisible — the exact issue-#14 shape the bar was built to fix, reintroduced
by a second surface. `reportError` writes to every bar that exists and each
suppresses itself when its own surface is closed; a message written to a bar
nobody can see costs nothing, while the one the user *is* looking at showing
nothing costs them the reason their save failed.

**`Window.anyVisible` is what the gamepad reader and the get-state poll gate
on.** Both were gated on the drawer's `visible` alone, which left the window
with a dead controller and frozen readouts the moment the drawer closed behind
it. Both flags are atomics because the reader's goroutine reads them.

**The constructor selects a tab; it does not sync one.** `syncPage` starts the
dashboard's poll and asks the daemon for a history window, and the window is
not on screen when it is built — the first `show` then skipped its own refresh
as "already in flight" and drew the chart from a reply fetched before the user
asked for anything. `selectTab` moves the stack and the highlight; `setTab` is
what a tab button does.

**Gamescope is not handled yet — and the reason is narrower than this file
used to claim.** What fails there is a second *toplevel*, which is what
`mainwindow.go` creates: only one window carries `STEAM_OVERLAY`, and
gamescope's `GetPossibleFocusWindows()` skips windows flagged `isOverlay`. That
says nothing about screen space. The gamescope backend's window is **already
fullscreen** — `Configure` sizes it to the whole output and keeps it mapped,
and `WrapContent` puts a click-to-dismiss backdrop plus a right-aligned 320px
panel inside it — so a full window there is a different **layout of the surface
we already own**, not a second surface.

HHD does exactly that, and it is worth knowing because it is the same fact as
the popup-layer section above: its sidebar and its larger settings view are one
Electron surface re-laying-out its contents. Everything lives in the one surface
gamescope composites, which is why its menus work where `GtkDropDown` does not.

The claim that *does* hold is why the drawer's existing view stack is not the
substitute: that stack lives inside the 320px panel the backend sizes, so a page
added to it would be a 320px "full window". The seam wanted is a stack at the
**wrapper** level — a `Backend` method to swap the wrapped child, which
layer-shell and overlay satisfy by continuing to use the toplevel. Until then
`openFull` opens the drawer's own dashboard under gamescope: the double press
still reaches the charts, on the surface that session actually has.

**Two of the four specified pages are absent, not stubbed.** Settings is
blocked on the same two api additions `internal/controls` records for generic
toggle rows (a description on `api.ToggleInfo`, a per-feature value in
`get-state`) — rendering the device's toggles is the whole content of that
page. Quickbar customization needs a `gui.toml` writer and a reorder affordance
that works on a controller. A tab onto an empty page is the same trap as a
device document declaring a capability nothing reads.

**`VOLTAIRE_GUI_OPEN_FULL=1` opens it at startup**, for the same reason
`VOLTAIRE_GUI_DUMP_FOCUS=1` exists: the window is otherwise reachable only by
pressing a key on one laptop, so nothing about it could be checked while it was
being written. The focus dump covers the window too, logging its pages as
`full:<tab>`.

### The telemetry dashboard (`dashboard.go`, `internal/telemetryplot`)

The `"dashboard"` view draws the daemon's sample history: one Cairo chart per
measured quantity, refreshed once a second while it is the visible view.

**Everything about *what* to draw is in `internal/telemetryplot`**, which is pure
and 100% covered: which series exist at all, where each reading sits in the
window, where the line breaks across a suspend, and what the y-axis spans.
`dashboard.go` measures the widget, multiplies by `Backend.Scale()`, and strokes
the result — the same split `fanCurveEditor` has with `internal/limits`, and for
the same reason. The three rules and the axis policy are written up in the root
`CLAUDE.md`; the ones that bite here are:

- **A quantity no sample carries gets no chart.** On the Z13 that means two
  charts, not three: `Sample()` reports no package power, and a flat line at 0 W
  would read as a measurement. Verified on hardware, not just in the table test.
- **Series of one kind share an axis**, because two fans on one chart are only
  worth drawing together if their heights are comparable.
- **`Plot.Shape()` is the rebuild key.** Chart widgets are torn down only when
  the layout changes, never when the values do — a per-second `DrawingArea`
  rebuild is the kind of churn that shows up as flicker.

GTK-side facts worth keeping:

- The chart is **painted, not styled**, so it applies `Backend.Scale()` by hand
  and reads `Window.colors` — the standing rule for anything Cairo. Trace colours
  come from the theme (`Accent`, then `Text`), never a hardcoded hue, or a light
  palette gets a trace it cannot see.
- `.dash-chart` restates its `min-height` in `gamescope.scaledCSS()`, as
  `.fan-curve-area` does. A `SetSizeRequest` height alone would stay at 1x.
- **The history poll is its own loop**, separate from `startTelemetryPolling`'s
  `get-state` poll: history is a much larger reply and only this view wants it.
  Every exit calls `stopDashboardPolling()`, `hide()` included — the tick's
  visible-child guard would catch a view switch a second later, but `hide()`
  leaves no view switch to catch.
- **Charts are not in the focus grid.** There is nothing to activate on one, so
  the list is the back button plus the span selector, and a shape change cannot
  invalidate it.
- The bottom-bar button is hidden when `device.Telemetry` is nil, and kept when
  the whole document is nil — absence means the machine lacks the capability,
  while a missing document only means the daemon did not answer.

### Control sizing: 48px is the touch target, and the autoswitch rows now match

Every tappable control in the drawer is `min-height: 48px` — `.btn-group
button`, `checkbutton`, `.tab-btn` — because the drawer is driven by touch and
a gamepad at least as often as by a pointer. The autoswitch target rows were
the exception until the dropdown conversion: they used plain `gtk.Button`s at
GTK's default height (~34px), so they were the smallest tap targets in a view
otherwise built around 48px.

Converting them to dropdown triggers put them in a `.btn-group` row and so
brought them to 48px, growing the main view by roughly 28px when autoswitch is
enabled (~42px under gamescope, where everything scales). **That is a
deliberate keep, not drift** — Jeff chose the touch-friendly size once it was
measured. It is recorded because it is now part of M4's parity referent: the
restructure must reproduce *this* drawer, and a future reader finding the main
view 28px taller than 1.x should not "fix" it.

The main view must still fit without scrolling on the documented baseline
(that is why the custom profiles live in their own view — see the profile
selector notes above), so this spends real headroom. Check it before adding
another main-view row.

The coupling is worth understanding if the height is ever revisited: the
`.dropdown-trigger` padding rule is scoped `.drawer .btn-group
button.dropdown-trigger`, so the trigger only gets its own styling *because*
the row carries `.btn-group` — which is also what supplies the 48px. To change
the height independently, rescope the rule to `.drawer button.dropdown-trigger`
first, then set the height explicitly. Do not simply drop `.btn-group` from the
row: that removes the padding and the border radius with it.

**`scaledCSS` must restate the trigger's horizontal padding.** The gamescope
sheet is `PRIORITY_APPLICATION+1` and sets the `padding` *shorthand* on
`.drawer .btn-group button`, which beats `layout.css`'s `padding-left/right` on
`.dropdown-trigger` regardless of specificity — provider priority wins over
specificity in GTK. Without the restated rule the trigger renders with ordinary
button padding under gamescope only, so the two sheets look identical in review
and differ on screen. `.popup-scrim` is the opposite case and correctly has no
scaled rule: it is a colour with no dimensions, so there is nothing to scale.

### Service environment

`contrib/z13gui.service` uses `EnvironmentFile=-%t/gamescope-environment` (optional).
`main.go` validates the gamescope Wayland socket exists before selecting the backend
to handle stale environment files after session switching.

## API usage (`github.com/dahui/z13ctl/api`)

Functions used:
- `api.SendGetState() (bool, *api.State, error)` — fetch full daemon state on show
- `api.Subscribe([]string{"gui-toggle"}) (<-chan string, func(), error)` — event stream
- `api.SendApply(device, color1, color2, mode, speed string, brightness int) (bool, error)`
- `api.SendOff(device string) (bool, error)` — turn off lighting for a device
- `api.SendProfileSet(profile string) (bool, error)`
- `api.SendBatteryLimitSet(limit int) (bool, error)`
- `api.SendPanelOverdriveSet(value int) (bool, error)` — 0 or 1
- `api.SendBootSoundSet(value int) (bool, error)` — 0 or 1
- `api.SendTdpSet(watts, pl1, pl2, pl3 string, force bool) (bool, error)` — set TDP.
  **`watts` (the `set` field) is mandatory even in advanced mode.** The daemon's
  `handleTDP` does `strconv.Atoi(req.Set)` before it looks at `pl1`/`pl2`/`pl3` and
  rejects the request with `TDP value must be an integer` if it is empty. `watts` is
  then used only as the default for any PL field left blank, so advanced mode passes
  PL1 as the base value (`SendTdpSet(pl1, pl1, pl2, pl3, force)` in `sendTdp()`).
- `api.SendTdpReset() (bool, error)` — reset TDP to firmware defaults
- `api.SendFanCurveSet(curve string) (bool, error)` — set custom fan curve ("temp:pwm,..." format)
- `api.SendFanCurveReset() (bool, error)` — reset fan curves to auto
- `api.SendUndervoltSet(cpu string) (bool, error)` — set CPU Curve Optimizer offset
- `api.SendUndervoltReset() (bool, error)` — reset undervolt to stock (0)

Key types from `api`:
```go
type State struct {
    Lighting           LightingState
    Devices            map[string]LightingState  // keyed by "keyboard", "lightbar"
    Profile            string
    Battery            int
    BootSound          int  // 0 or 1
    PanelOverdrive     int  // 0 or 1
    TDP                *TDPState
    FanCurve           *FanCurveState
    Undervolt          *UndervoltState
    UndervoltAvailable bool  // true if ryzen_smu is loaded
    Temperature        int   // APU temp, degrees Celsius
    FanRPM             int   // fan1 speed in RPM
}
type LightingState struct {
    Enabled bool; Mode string; Color string; Color2 string
    Speed string; Brightness int
}
type TDPState struct {
    PL1SPL int; PL2SPPT int; FPPT int
}
type FanCurveState struct {
    Mode   int              // 0=auto, 1=custom
    Points []FanCurvePoint  // 8 points
}
type FanCurvePoint struct {
    Temp int; PWM int
}
type UndervoltState struct {
    CPUCO  int   // all-core CPU Curve Optimizer offset (0 to -40)
    Active bool  // true when CO is applied to hardware
}
```

## Daemon socket

Path: `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock`

Daemon must be running for any `api.*` calls to succeed. If the daemon is not running,
`api.Subscribe` returns `nil, nil, nil` and `SendGetState` returns `false, nil, nil`.
The subscribe loop handles this with backoff retry.

## Build

```sh
make build      # CGO_ENABLED=1 go build -o z13gui .
sudo make install  # installs pre-built binary to /usr/local/bin/z13gui
make test       # unit tests for the pure-Go packages (no GTK4 headers needed)
make lint       # golangci-lint run ./...
make clean      # rm z13gui
make snapshot   # goreleaser local build (no publish)
make release    # goreleaser build + publish
```

Requires at build time: `gtk4-layer-shell` C library (`pkg-config gtk4-layer-shell-0`).

`make test` derives its package list (`HERMETIC_PKGS`) rather than hand-listing
it, excluding `internal/gui` because it needs CGO and GTK4 headers while
`go list` only reads source — but adding back `internal/gui/gamepad/...`, which
is cgo-free. A new pure package is therefore tested automatically. `make race`
runs the same set under the race detector; `make cover` reports per-function
coverage.

CI (`.github/workflows/ci.yml`) runs tests + race on plain ubuntu and
build + gofmt + lint in the same Arch container as the release job. Before this
existed nothing ran tests on a push, which is how PR #10 merged tests that never
executed.

## Known GTK issues (do not re-introduce)

- **`hexpand: true` in CSS** — not a valid CSS property. Use `widget.SetHExpand(true)` in Go.
- **`scale.AddMark()`** — causes `GtkGizmo (slider) reported min width -2` warnings and
  pixman `Invalid rectangle` errors when the scale widget is in an animated context.
  Display-only values work fine with `SetDrawValue(true)`.
- **`gtk.Revealer` with `SlideLeft`** — causes smearing artifacts in Wayland Vulkan
  rendering because GTK's damage region doesn't properly clear the transparent areas left
  behind as the revealer collapses. Use layer-shell margin animation instead.
- **`SetSizeRequest` + Revealer** — keeping the window at fixed width while the Revealer
  collapses internally still leaves stale pixels; the compositor doesn't know the content
  region shrank.
- **`box-shadow` on animated containers** — shadow pixels extend outside the widget clip
  region and are not cleared each frame in Wayland Vulkan rendering, causing smearing.
- **GTK4 popovers in gamescope** — create separate override-redirect X11 windows that
  gamescope doesn't composite. Use `gtk.Stack` view switching instead, in **both**
  backends: one widget tree is what lets the gamepad focus grid work the same way in
  each, so a KDE-only popover would still be the wrong answer.
- **GDK_SCALE in gamescope** — causes double scaling (GTK scales buffer, then gamescope
  scaler scales again). Use manual CSS scaling via `scaledCSS()` instead.
- **GtkDropDown in gamescope** — popup list is a separate X11 window. Use buttons or
  radio buttons instead. Profile selector uses `gtk.Button` with CSS `.active` class.
- **CheckButton/Switch touch in gamescope** — GTK4's CheckButton and Switch use an
  internal BUBBLE-phase GestureClick, which fails for touch input in gamescope/XWayland.
  Button widgets use CAPTURE phase and work fine. Workaround: `addTouchActivate()` in
  controls.go adds a touch-only (`SetTouchOnly(true)`) CAPTURE-phase GestureClick to each
  affected widget. Do not remove — without it, all CheckButtons and Switches are
  untappable via touchscreen in gamescope mode.

## Current status

Feature-complete for both KDE and gamescope modes:
- Margin-based slide animation (smoothstep, 200ms) — KDE
- Gamescope X11 overlay with opacity-based visibility + keyboard grab + STEAM_INPUT_FOCUS
- Touch activation workaround for gamescope (CAPTURE-phase GestureClick on CheckButton/Switch)
- Pointer-inside guard for KDE focus-loss handling (spurious drop vs genuine click-outside)
- 50ms duplicate filter on daemon `gui-toggle` events (`internal/togglegate`)
- Single-instance activate guard (re-activation toggles instead of building a second drawer)
- GTK_A11Y=none for systemd AT-SPI timeout prevention
- RGB lighting controls (mode, color presets + custom chooser/HSL picker, speed, brightness)
- Profile switching via buttons (quiet/balanced/performance/custom)
- Custom profile view with:
  - TDP control: basic (single watt slider) and advanced (PL1/PL2/PL3) modes
  - Fan curve editor: 8-point Cairo graph with drag interaction, 35–105°C range
  - Undervolt: CPU Curve Optimizer slider (inside advanced TDP box, hidden when
    `ryzen_smu` unavailable). Slider shows 0 when not on custom profile.
    iGPU CO is not supported on Strix Halo.
  - Telemetry: APU temp + fan RPM in header and custom view, polled every 1s
  - Separate save/reset buttons for TDP, fans, and undervolt
- Battery charge limit slider
- Panel overdrive and boot sound toggles (footer switches)
- 15 built-in themes with accent variants + custom theme.toml support
- Gamescope view switching: theme picker view + HSL color picker view
- Resolution-based CSS scaling for gamescope (Z13GUI_SCALE override)
- Split-level logging (app=Info, GTK=Error; `-d` enables all Debug)
- goreleaser + GitHub Actions release pipeline
- systemd user service with optional gamescope-environment loading
