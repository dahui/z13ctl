# `--color 000000` is not black

**`--color 000000` is not black.** `aura.SetMode` sets the random-colour flag
(`0xFF`) for an all-zero primary colour, matching g-helper, so the firmware
picks a colour. `off` / `--brightness off` is how you get no light. Documented
in the commands reference and the `--color` flag help.
