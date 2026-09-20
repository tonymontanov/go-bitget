/*
FILE: uta/doc.go

DESCRIPTION:
Package uta implements the Bitget V3 UNIFIED TRADING ACCOUNT (UTA) profile
— the next-generation API that trades spot and derivatives from a single
cross-margined account. Unlike the V2 profiles, V3 is organised around a
per-call `category` parameter (SPOT / MARGIN / USDT-FUTURES /
COIN-FUTURES / USDC-FUTURES) rather than product-pinned clients.

	uta.Client
	  ├── Public()   unsigned market data (/api/v3/public,market/*)
	  ├── Account()  unified account: assets, settings, leverage,
	  │               hold-mode, fee-rate, ...   (/api/v3/account/*)
	  ├── Trade()    order flow: place / modify / cancel (+batch),
	  │               queries, fills            (/api/v3/trade/*)
	  ├── Position() positions: current / history / adl, max-open
	  │               (/api/v3/position/* + account/max-open-available)
	  ├── Strategy() plan (TP/SL) orders        (/api/v3/trade/*-strategy-*)
	  └── Stream()   V3 WebSocket: public market data + private account
	                  topics              (wss://ws.bitget.com/v3/ws/...)

NOTES:

  - V3 signing reuses the V2 ACCESS-KEY / ACCESS-SIGN / ACCESS-PASSPHRASE
    scheme — the existing signer works unchanged.
  - DEMO TRADING: set bitget.Config.Demo = true (and use a Demo API Key) to
    add the `paptrading: 1` header on signed V3 requests. REST demo runs on
    the production host; the WebSocket has dedicated demo endpoints
    (wss://wspap.bitget.com/v3/ws/...) that Stream() picks automatically
    when Demo is set and Config.WS.UTAPublicURL / UTAPrivateURL are empty.
  - HOLD MODE: futures support one-way and hedge mode (Account.SetHoldMode);
    in hedge mode orders / positions carry a posSide (long / short).

STREAMING (Stream(), V3 WebSocket):

	Public  (category + symbol; instType on the wire = lower-case category)
	  WatchTicker        ticker       last / BBO / mark / index / funding
	  WatchPublicTrades  publicTrade  one call per trade, oldest first; the
	                                  history snapshot sent on subscribe is
	                                  skipped
	  WatchOrderBook     books1|5|50  stateless venue snapshots (depth ≤ 50)
	                     books        depth > 50: local incremental book,
	                                  validated by the seq / pseq chain (V3
	                                  has no checksum); a gap drops the book,
	                                  surfaces ErrOrderBookResync once and
	                                  resubscribes
	Private (account-wide: {"instType":"UTA","topic":...}, no symbol — every
	         category / symbol is delivered, the consumer filters)
	  WatchOrders / WatchFills / WatchPositions / WatchAccount
	    deliver the REST domain types (Order / Fill / CurrentPosition /
	    AccountAssets), extended additively where the WS row carries more.

STREAM NOTES:

  - FAN-OUT: any number of Watch* calls for the same wire arg share ONE wire
    subscription; each handler detaches with its own ctx and the wire
    unsubscribe goes out with the last one. Handlers run in registration
    order on the connection's read goroutine — keep them fast.
  - RECONNECTS are transparent (relogin + resubscribe by internal/ws).
    OnPublicReconnect / OnPrivateReconnect fire after every successful
    RE-connect so a consumer can re-seed over REST what the venue does not
    replay (missed order / fill events).
  - Connections are lazy (first public / first private Watch*) and use the
    root Config.WS timeouts. Private Watch* without credentials fails with
    ErrorKindAuth before dialling. The login is the V2 one (timestamp in
    SECONDS — the V3 docs text says milliseconds, their samples and the
    reference client say seconds).
  - VERIFICATION: the public topics are verified against the live venue
    (integration/uta_stream_test.go); the private topics are implemented
    from the docs only and decode tolerantly — they still need a live run
    with a UTA / Demo key.

The REST surface covers the UTA CORE: Public + Account + Trade + Position +
Strategy. The V3 re-issues of wallet / sub-accounts / tax / broker / loan /
earn-elite / copy and WebSocket order entry (place / cancel over WS) are
separate follow-up sub-phases.

The lazy entry point bitget.Client.UTA() returns *uta.Client only after
this package is imported (init() registers the factory).
*/
package uta
