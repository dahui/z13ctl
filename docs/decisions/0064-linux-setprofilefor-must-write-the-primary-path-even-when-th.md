# `linux.setProfileFor` must write the primary path even when the loop misses it

**`linux.setProfileFor` must write the primary path even when the loop misses it.**
It writes every platform-profile class device but only tracks the error for
the one `FindProfilePath` picked; when no class device has a `profile` file
that path is the ACPI alias, which lives outside `sysProfileDir` and the loop
never visits. It returned nil having written nothing, and still called
the power-profiles-daemon sync (`ppdRunner`). Guarded by `primaryWritten` + a fallback write.
