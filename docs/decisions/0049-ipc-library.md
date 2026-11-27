# IPC library

**IPC library**: Hand-rolled JSON. gRPC/drpc require protobuf code generation;
`net/rpc` lacks streaming; Twirp is HTTP-only. Decky plugin Python backend connects
via `asyncio.open_unix_connection()` + `json` — zero extra deps.
