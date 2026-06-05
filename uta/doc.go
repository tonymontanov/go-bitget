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
	  └── Strategy() plan (TP/SL) orders        (/api/v3/trade/*-strategy-*)

NOTES:

  - V3 signing reuses the V2 ACCESS-KEY / ACCESS-SIGN / ACCESS-PASSPHRASE
    scheme — the existing signer works unchanged.
  - DEMO TRADING: set bitget.Config.Demo = true (and use a Demo API Key) to
    add the `paptrading: 1` header on every request. Demo runs on the
    production host; the WS demo endpoints are a later concern.
  - HOLD MODE: futures support one-way and hedge mode (Account.SetHoldMode);
    in hedge mode orders / positions carry a posSide (long / short).

This first cut is REST-only and covers the UTA CORE: Public + Account +
Trade + Position + Strategy. The V3 re-issues of wallet / sub-accounts /
tax / broker / loan / earn-elite / copy and the V3 WebSocket are separate
follow-up sub-phases.

The lazy entry point bitget.Client.UTA() returns *uta.Client only after
this package is imported (init() registers the factory).
*/
package uta
