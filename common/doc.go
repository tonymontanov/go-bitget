/*
FILE: common/doc.go

DESCRIPTION:
Package common implements the Bitget V2 COMMON / PUBLIC utility surface —
the market-agnostic, account-level endpoints that do not belong to a
single trading profile:

	common.Client
	  ├── Public()  unsigned: server time + announcements
	  │              (/api/v2/public/*)
	  ├── Account() account-wide assets + trade-rate
	  │              (/api/v2/account/* + /api/v2/common/trade-rate)
	  ├── Tax()     tax transaction records
	  │              (/api/v2/tax/*)
	  ├── P2P()     P2P merchant info / orders / ads
	  │              (/api/v2/p2p/*)
	  └── Users()   virtual sub-account + API-key management
	                 (/api/v2/user/*)

NOTES:

  - REST-only, account-level: not product-type scoped, no WebSocket.
  - Public() calls are UNSIGNED (no ACCESS-SIGN header); everything else
    is signed.
  - Virtual sub-accounts here are main-account user management, distinct
    from the broker sub-accounts in the broker/ package.
  - create / modify virtual sub-account (+ API key) change account state —
    the SDK validates the obvious client-side preconditions only.

The lazy entry point bitget.Client.Common() returns *common.Client only
after this package is imported (init() registers the factory).
*/
package common
