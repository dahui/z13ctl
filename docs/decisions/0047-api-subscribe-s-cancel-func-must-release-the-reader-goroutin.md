# `api.Subscribe`'s cancel func must release the reader goroutine

**`api.Subscribe`'s cancel func must release the reader goroutine.** The reader
parks in a channel send once the 8-slot buffer fills, where closing the
connection cannot reach it — so cancel closes a `done` channel that the send
selects on, guarded by `sync.Once` for idempotency. Without it, any caller that
stops consuming leaks the goroutine and never closes the channel.
