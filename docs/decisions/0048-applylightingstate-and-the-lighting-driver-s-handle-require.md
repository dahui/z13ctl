# `applyLightingState` and the lighting driver's handle require `d.mu`

**`applyLightingState` and the lighting driver's handle require `d.mu`.** The
hotplug watcher reopens it (`d.hw.Lighting.Reopen()`); `applyLightingState` also reads the `d.state.Devices`
map that socket handlers mutate. The resume watcher previously read both
unlocked, racing the hotplug watcher over a live HID handle.
