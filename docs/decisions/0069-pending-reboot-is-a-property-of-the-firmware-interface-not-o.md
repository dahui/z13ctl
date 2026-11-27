# `pending_reboot` is a property of the firmware *interface*, not of any one toggle, so it is an optional interface rather than a `Toggles` method

**`pending_reboot` is a property of the firmware *interface*, not of any one
toggle, so it is an optional interface rather than a `Toggles` method.**
`driver.RebootPending` is type-asserted; a device whose settings apply
immediately has nothing to implement, and two no-op methods would suggest a
choice where there is none — the same reasoning as `fullSurfaceHost` in the
GUI. asus-armoury exposes it as a plain file *beside* the attribute
directories (`attributes/pending_reboot`), not as an attribute, which is why
it is read directly rather than through `toggleValuePath`.
`api.State.PendingReboot` is a `*bool` on the established rule: absent means
the device cannot say — no firmware interface, a pre-2.0 daemon, or a failed
read — and rendering that as "nothing pending" would be a claim nothing
established. `settingsui.RebootNotice` gates on a *known* true, and the banner
deliberately does **not** name the setting, because the firmware reports one
flag for the whole interface and promising more would send the user looking
for a row to fix. It is updated *before* `sync`'s no-rows return: the flag can
be set on a device whose toggles this build cannot render, and that is exactly
the user who most needs telling why the machine did not change.
**Unexercised on this hardware.** Toggling `boot_sound` does not raise the
flag — the Z13's two toggles both apply immediately — so the true branch was
verified with a throwaway build forcing it, and never on real firmware. New
pointer on `api.State` ⇒ `cloneState` got it.
