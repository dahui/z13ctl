# The profile selector expands in the flow of the view; it is not a `GtkDropDown`

**The profile selector expands in the flow of the view; it is not a
`GtkDropDown`.** It reads as a dropdown — one row collapsed, `▾`/`▴`, the
current target highlighted — but a real dropdown's popup is a separate
window that gamescope does not composite, so Gaming Mode would be choosing
from an invisible list. Expanding in place also means the rows are ordinary
widgets the existing focus grid navigates via `isVisible`, with no second
mechanism for a popup's contents. Same reasoning as the autoswitch cycle
buttons and the inline name entry; `grep Popover internal/` must stay empty.
