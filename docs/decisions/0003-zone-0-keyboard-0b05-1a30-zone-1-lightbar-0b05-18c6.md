# Zone 0 = keyboard (`0b05:1a30`), Zone 1 = lightbar (`0b05:18c6`) —

Zone 0 = keyboard (`0b05:1a30`), Zone 1 = lightbar (`0b05:18c6`) — device data
now (`[[lighting.zones]]`), not a table in `internal/hid`. Every zone byte is
sent to every node written to; each physical device silently ignores the other
zone's packets.
