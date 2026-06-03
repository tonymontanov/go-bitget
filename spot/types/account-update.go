/*
FILE: spot/types/account-update.go

DESCRIPTION:
AccountUpdate — one row from the V2 spot `account` private WS channel.
Bitget streams per-asset balance snapshots (one row per coin); the
account-wide rollup must be computed by the caller (sum coin values
across the relevant denominations).

WIRE SHAPE (per Bitget docs, October 2024):

	{
	    "coin":           "USDT",
	    "available":      "100000",
	    "frozen":         "0",          // reserved by open orders
	    "locked":         "0",          // KYC / fiat-merchant locks
	    "limitAvailable": "0",          // copy-trading restriction
	    "uTime":          "1697092295506"
	}

WHY A SEPARATE TYPE FROM mix.WatchAccount's roottypes.Balance:

mix's `account` channel ships per-margin-coin rows that bundle
TotalEquity / AvailableBalance / UnrealizedPnL / margin-coin breakdown
— a derivatives-account abstraction. Spot does not have positions or
unrealized PnL; conflating the two would either always-zero half of
mix's fields or force spot users to ignore unrealized PnL columns
that are structurally meaningless on cash-only spot.

The chosen shape mirrors the wire field-by-field; the desk-side core
connector adapts it to its own balance representation.

`coin` SUBSCRIPTION ARG:

Bitget V2 spot's `account` channel REQUIRES `coin="default"` at
subscribe time — only the wildcard is supported (verified in the
docs). Per-coin filters are NOT possible at the venue level. The SDK
preserves a per-coin caller filter client-side: pass any concrete
coin to receive only its rows; pass empty string or "default" to
receive every asset on the account.
*/

package types

import (
	"github.com/shopspring/decimal"
)

// AccountUpdate — one per-asset balance row from the spot `account`
// private channel.
type AccountUpdate struct {
	// Coin — e.g. "USDT", "BTC".
	Coin string

	// Available — amount free for trading. New orders draw from this.
	Available decimal.Decimal
	// Frozen — held by open orders (cancel returns it to Available).
	Frozen decimal.Decimal
	// Locked — held by KYC / fiat-merchant / risk holds. Cancellation
	// does not return this; resolution requires venue-side action.
	Locked decimal.Decimal
	// LimitAvailable — restricted-availability balance for spot copy-
	// trading. Zero on regular accounts.
	LimitAvailable decimal.Decimal

	// UpdatedAtMs — venue's last-touched timestamp for this row.
	UpdatedAtMs int64
}
