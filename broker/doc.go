/*
FILE: broker/doc.go

DESCRIPTION:
Package broker implements the Bitget V2 BROKER / AGENT profile
(/api/v2/broker/... plus the copy/mix-broker reads). It serves two
distinct Bitget programs that share the broker namespace:

  - the institutional BROKER program — managed sub-accounts and the
    broker commission / trade-volume / rebate reporting;
  - the AGENT (affiliate / referral) program — customer commission,
    KYC, deposit and asset reporting.

PROFILE SHAPE:

	broker.Client
	  ├── SubAccounts() account/* sub-account lifecycle, assets,
	  │                  deposit/withdrawal, address, auto-transfer + records
	  ├── APIKeys()      manage/* sub-account API-key lifecycle
	  ├── Stats()        institutional broker reporting
	  │                  (subaccounts/commissions/trade-volume/totals/rebate)
	  ├── Agent()        affiliate customer reporting (customer-*)
	  └── CopyBroker()   copy/mix-broker trader reads

NOTES:

  - Account-level and REST-only: broker is NOT product-type scoped
    (except subaccount-future-assets, which takes a productType) and
    ships no WebSocket.
  - All calls are signed. Most endpoints require the account to be an
    approved Bitget broker / agent — a non-eligible account gets a 4xx
    (treat as an expected eligibility note, not a bug).
  - create-subaccount / subaccount-withdrawal / create-subaccount-apikey
    move funds or create credentials — the SDK validates the obvious
    client-side preconditions only; callers own the risk.

The lazy entry point bitget.Client.Broker() returns *broker.Client only
after this package is imported (init() registers the factory).
*/
package broker
