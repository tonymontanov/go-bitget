# go-bitget examples

Runnable end-to-end demos against the live Bitget V2 API. Every example
is a stand-alone `main.go` under the same module, so you can run them
directly without a separate `go.mod`:

```bash
go run ./examples/<name>           # default flags
go run ./examples/<name> -help     # see all flags
```

## Index

| Example | Auth required | What it demonstrates |
| --- | --- | --- |
| [`marketdata`](./marketdata) | No | REST `GetSymbolInfo` / `GetMarketTicker` / `GetOrderBook` + WS `WatchOrderbook` (top-of-book printer with CRC32-validated local book). |
| [`place-order`](./place-order) | Yes | Signed REST `CreateOrder` (post-only LIMIT 5 % below ask) → `GetOrderDetail` → `CancelOrder`. Safe on a live account: the order is far out-of-market and post-only. |
| [`private-stream`](./private-stream) | Yes | Signed WS — `WatchOrders` / `WatchPositions` / `WatchAccount` for one symbol, prints every push for `-duration`. |

## Credentials

The two signed examples read credentials from environment variables:

```bash
export BITGET_API_KEY=...
export BITGET_SECRET_KEY=...
export BITGET_PASSPHRASE=...
```

The API key MUST have:

- **Trade** permission (for `place-order`).
- **Read** permission (for `private-stream`).
- The host machine's egress IP whitelisted on the Bitget API-key
  page (otherwise the SDK returns `ErrorKindAuth` with code `40018`
  / `40038`).

## Notes on production use

- `place-order` is **safe** to run on a live account because the
  order is post-only and priced 5 % below the best ask — it cannot
  fill before the example cancels it. Still, **always** review the
  command before running and consider using the `BITGET_DEMO_API`
  endpoints if your account supports them.

- `private-stream` is read-only. To see pushes you can run
  `place-order` (or any manual order on the Bitget UI) in a second
  terminal.

- `marketdata` uses zero credentials and is the safest first run when
  you want to verify network reachability and SDK install.
