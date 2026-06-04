/*
FILE: margin/doc.go

DESCRIPTION:
Package margin implements the Bitget V2 MARGIN profile — leveraged
spot trading with borrowed funds, in both CROSSED and ISOLATED modes.

WIRE NAMING — crossed vs isolated:

Bitget V2 selects the margin mode through the URL PATH SEGMENT, not a
body/query parameter:

	/api/v2/margin/crossed/place-order
	/api/v2/margin/isolated/place-order
	/api/v2/margin/crossed/account/borrow
	/api/v2/margin/isolated/account/assets
	... (every margin endpoint follows /api/v2/margin/<mode>/...)

The literal segment is "crossed" / "isolated" (NOT "cross"/"iso") —
verified against the live docs and the cross-checked third-party SDKs.
The SDK pins the mode at construction time (see ClientSettings.Mode)
so callers never spell the segment themselves; the value is the same
roottypes.MarginMode the mix profile already uses for its body-level
marginMode field.

MARGIN TRADES SPOT INSTRUMENTS:

Margin orders are placed on SPOT symbols (BTCUSDT, ETHUSDT, ...). There
are NO margin-specific price / orderbook / candle endpoints — those
live on the spot profile. For market data, callers use spot.MarketData()
(or the spot Stream for live books / tickers); the margin profile does
NOT duplicate them. The only public margin reference endpoint is
/api/v2/margin/currencies (supported margin coins + per-coin limits),
exposed via Public().

PROFILE SURFACE (rolled out in milestones, mirroring mix/spot):

  - Trading  — place / batch-place / cancel / batch-cancel. There is NO
    amend endpoint on margin (Bitget ships none): a re-price is a
    cancel followed by a fresh place, owned by the caller.
  - Account  — assets, borrow / repay / flash-repay, risk-rate,
    max-borrowable / max-transfer-out, interest-rate-and-limit,
    tier-data, plus the paged records (open / history orders, fills,
    borrow / repay / interest / liquidation / financial history).
  - Public   — currencies (margin-coin reference data).
  - Stream   — PRIVATE WS only: account-<mode> + orders-<mode> on
    instType="MARGIN". There is no public margin WS (use the spot
    Stream for books / tickers).

LOAN TYPE:

Every margin order carries a loanType (normal / autoLoan / autoRepay /
autoLoanAndRepay) that tells Bitget whether to auto-borrow the shortfall
and/or auto-repay the debt on fill. The SDK defaults an empty value to
"normal" (place a plain order against existing collateral) so the field
is always present on the wire (Bitget marks it required).

SIZE DENOMINATION:

Like spot, margin uses a side-dependent size: BaseSize for limit orders
and market sells (left coin), QuoteSize for market buys (right coin).
The SDK ships the caller's values verbatim — no base/quote conversion.
*/
package margin
