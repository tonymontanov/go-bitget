/*
FILE: earn/doc.go

DESCRIPTION:
Package earn implements the Bitget V2 EARN profile (/api/v2/earn/...):
on-platform yield products. It is organised as category sub-clients off
earn.Client, mirroring Bitget's own grouping:

	earn.Client
	  ├── Account()   GET earn/account/assets         (overview)
	  ├── Savings()   earn/savings/*                  (flexible / fixed)
	  ├── SharkFin()  earn/sharkfin/*                 (structured)
	  ├── Elite()     earn/elite/*                    (on-chain elite)
	  └── Loan()      earn/loan/*                      (crypto loan)

NOTES:

  - Account-level and REST-only: earn is NOT product-type scoped and
    ships no WebSocket.
  - Most queries are signed; the two loan/public/* endpoints are public.
  - Subscribe / redeem / borrow / repay move real funds — callers own the
    risk; the SDK only validates the obvious client-side preconditions.

The lazy entry point bitget.Client.Earn() returns *earn.Client only after
this package is imported (init() registers the factory).
*/
package earn
