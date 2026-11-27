# Daemon socket protocol

**Daemon socket protocol**: Newline-delimited JSON over Unix socket. All responses
are `{"ok":bool,...}`. CLI `--get` commands read sysfs directly (always ground
truth) — except `profile --get`, which asks the daemon first and falls back to
sysfs: `platform_profile` is never a custom profile name, so sysfs alone cannot
say which custom profile is running; and `tdp --get`, which asks the daemon
and on a firmware profile shows its stock row rather than any readback (entry
below on `ReadDisplayed`).
GUI/Decky callers use the daemon socket for all operations including GET. Protocol:
| Command | Request | Response field |
|---|---|---|
| apply | `{"cmd":"apply","mode":"cycle","color":"FF0000","brightness":3,"device":"lightbar"}` | `ok` |
| off | `{"cmd":"off","device":""}` | `ok` |
| brightness | `{"cmd":"brightness","brightness":2,"device":""}` | `ok` |
| profile set | `{"cmd":"profile","set":"performance"}` | `ok` |
| profile get | `{"cmd":"profile-get"}` | `ok`, `value` |
| battery set | `{"cmd":"batterylimit","set":"80"}` | `ok` |
| battery get | `{"cmd":"batterylimit-get"}` | `ok`, `value` |
| boot sound set | `{"cmd":"bootsound","set":"1"}` | `ok` |
| boot sound get | `{"cmd":"bootsound-get"}` | `ok`, `value` |
| device document | `{"cmd":"device-get"}` | `ok`, `device` (capabilities/limits; static, cacheable) |
| telemetry history | `{"cmd":"telemetry-history","seconds":60}` | `ok`, `history` (typed array; absent `seconds` = the whole window) |
| feature set | `{"cmd":"feature","id":"boot_sound","set":"1"}` | `ok` |
| feature get | `{"cmd":"feature-get","id":"boot_sound"}` | `ok`, `value` |
| button that opens voltaire | `{"cmd":"button-set","set":"armoury-crate"}` (an id, `none`, or `default`) | `ok` |
| cpu boost set | `{"cmd":"cpuboost","set":"0"}` | `ok` |
| cpu boost get | `{"cmd":"cpuboost-get"}` | `ok`, `value` (`0`/`1`) |
| controller vibration set | `{"cmd":"rumble","set":"2"}` | `ok` |
| controller vibration get | `{"cmd":"rumble-get"}` | `ok`, `value` |
| panel overdrive set | `{"cmd":"paneloverdrive","set":"1"}` | `ok` |
| panel overdrive get | `{"cmd":"paneloverdrive-get"}` | `ok`, `value` |
| fan curve get | `{"cmd":"fancurve-get"}` | `ok`, `value` (JSON) |
| fan curve set | `{"cmd":"fancurve","set":"48:2,..."}` | `ok` |
| fan curve reset | `{"cmd":"fancurve-reset"}` | `ok` |
| tdp get | `{"cmd":"tdp-get"}` | `ok`, `value` (JSON) |
| tdp set | `{"cmd":"tdp","set":"60","pl1":"55","pl2":"65","pl3":"70","force":true}` | `ok` |
| tdp reset | `{"cmd":"tdp-reset"}` | `ok` |
| tuning reset | `{"cmd":"tuning-reset"}` | `ok` |
| undervolt set | `{"cmd":"undervolt","set":"-20"}` | `ok` |
| undervolt get | `{"cmd":"undervolt-get"}` | `ok`, `value` (JSON: `cpu_co`, `active`, `profile`) |
| undervolt reset | `{"cmd":"undervolt-reset"}` | `ok` |
| profile create | `{"cmd":"profile-create","set":"gaming"}` | `ok` |
| profile save-as | `{"cmd":"profile-save","set":"gaming"}` | `ok` |
| profile delete | `{"cmd":"profile-delete","set":"gaming"}` | `ok` |
| profile list | `{"cmd":"profile-list"}` | `ok`, `value` (JSON) |
| autoswitch set | `{"cmd":"autoswitch","enabled":true,"ac":"balanced","battery":"gaming"}` | `ok` |
| autoswitch get | `{"cmd":"autoswitch-get"}` | `ok`, `value` (JSON) |
| full state | `{"cmd":"get-state"}` | `ok`, `state` (cached + sysfs + live telemetry + undervolt_available + on_ac/source_known + battery_health + pending_reboot + tdp_limits) |
| subscribe | `{"cmd":"subscribe","events":["gui-toggle"]}` | `ok`, then streams `{"ok":true,"event":"gui-toggle"}` |
(events: `gui-toggle`, `gui-open-full`, `power-source`, `state-changed`)

`fancurve`, `fancurve-reset`, `tdp`, `tdp-reset`, `tuning-reset`, `undervolt` and
`undervolt-reset` take an optional `"profile"` field naming the custom profile
to edit; absent/empty means the active one. `profile --set` now *rejects* a
name that is neither a firmware profile nor a saved custom profile, where it
used to forward any string to `platform_profile`.
