/*
FILE: convert/doc.go

DESCRIPTION:
Package convert implements the Bitget V2 CONVERT (flash-swap) profile
(/api/v2/convert/...): instant coin-to-coin conversion at a quoted RFQ
price, plus the BGB small-balance conversion.

PROFILE SHAPE:

	convert.Client
	  ├── GetCurrencies      GET  /currencies
	  ├── GetQuotedPrice     GET  /quoted-price        (RFQ; traceId valid ~8s)
	  ├── Trade              POST /trade               (consumes the RFQ)
	  ├── GetHistory         GET  /convert-record      (cursor paged)
	  ├── GetBGBCoins        GET  /bgb-convert-coin-list
	  ├── ConvertBGB         POST /bgb-convert
	  └── GetBGBHistory      GET  /bgb-convert-records

NOTES:

  - Account-level and REST-only: convert is NOT product-type scoped and
    ships no WebSocket.
  - Two-step swap: GetQuotedPrice returns a traceId + cnvtPrice with a
    short TTL; Trade echoes them back to execute. The SDK does not cache
    the quote — the caller passes the quote fields straight through.
  - All calls are signed (private).

The lazy entry point bitget.Client.Convert() returns *convert.Client only
after this package is imported (init() registers the factory).
*/
package convert
