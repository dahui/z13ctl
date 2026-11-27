# The hardware button's name is device data

**The hardware button's name is device data** (the default `[[buttons]]`
entry's `label` → `Device.ButtonLabel` → device-get `button_label`, and the
chosen button's label via `buttonpref.Drawer`); `buttonpref` falls back to
"hardware button", so no surface names the Armoury Crate button on a machine
that has none. evdevkey checks the device's static
`capabilities/key` bitmap for the keycode once per watch and only warns: the
Z13's node reports KEY_PROG1 as well as KEY_PROG3, so the bitmap says what a
node *can* send, not which key is the button.
