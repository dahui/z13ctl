# Trademark and Naming Policy
 
`z13ctl` and `z13gui` are released under the **Apache License 2.0**. That license
governs the **source code**. It does **not** grant rights to the project **names**.
This distinction is drawn by the license itself (Apache 2.0 §6 grants no trademark
rights) and this document explains how the names may and may not be used.
 
## Names this policy covers
 
The names **"z13ctl"** and **"z13gui"**, the command names they install (`z13ctl`,
`z13gui`), and the package names this project publishes (e.g. `z13ctl-bin`) are used
to identify the official software and its origin.
 
## What you may do
 
- Use, fork, modify, and redistribute the **code** under the terms of Apache 2.0.
  The license means what it says, and forking is welcome.
- Refer to the project **by name, truthfully** — e.g. "a fork of z13ctl",
  "compatible with z13ctl", "based on z13ctl". Nominative references to the origin
  are fine and expected.
  
## What we ask of forks and derivatives
 
Because the license lets you reuse the code freely, the responsibility for avoiding
user confusion falls on **naming**. If you distribute a modified version, please:
 
1. **Use a distinct name.** A name confusingly similar to "z13ctl" / "z13gui" —
   including a bare suffix like `-plus`, `-ng`, or `+` — reads as an official
   variant and is not, on its own, sufficient distinction.
2. **Ship your own command name** (e.g. `mytool`, not `z13ctl`) and your own
   systemd unit, socket, and state paths, so your build does not shadow or collide
   with an installed copy of the upstream software.
3. **Publish under your own package name.** If your package is intended to replace
   the upstream one, declare that relationship explicitly in your packaging
   (e.g. `conflicts` / `provides` on Arch) rather than silently occupying the same
   files.
4. **Do not imply endorsement** by, or affiliation with, this project or its
   maintainer.
   
## Why
 
These requests exist so that a user who installs, runs, or depends on "z13ctl" gets
the software they think they are getting, and so the origin of changes stays clear.
They are about preventing confusion, not about restricting your freedom to build on
the code, which Apache 2.0 fully grants.
 
## Questions
 
If you are unsure whether a particular use is fine, please ask first. Open an issue
or contact the maintainer. We are glad to work it out.
 
