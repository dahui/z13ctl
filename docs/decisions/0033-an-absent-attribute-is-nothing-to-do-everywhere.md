# An absent attribute is nothing to do, everywhere

**An absent attribute is nothing to do, everywhere** (2026-11-17, then the
boot pass). Every udev rule's loop skips a missing file with `|| continue`:
`[ -e f ] && chgrp …` left the status at 1 when the last file was absent,
and udev logged a failed RUN at every boot on any machine without that
attribute (`TestRulesGuards` checks every device's output). The boot pass
treats an absent file the same way and a failed change as a warning, so the
unit never fails for one attribute.
