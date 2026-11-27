# The pre-sleep release lowers power before it touches the fans, and fails closed

**The pre-sleep release lowers power before it touches the fans, and fails
closed.** Above `TDPMaxSafe` it writes the underlying firmware profile's
`StockProfilePPT` row via `restoreStockPPTErr` and *returns without releasing
the fans* if that fails — the mirror image of `ApplyTDPSafely`, and the same
release order as `applyStockHW`/`handleTDPReset`. `restoreStockPPTErr` treats a
profile absent from `StockProfilePPT` as an error rather than the silent no-op
`restoreStockPPT` can afford: "there was nothing to write" is not "the limit is
now low enough to release the fans".
