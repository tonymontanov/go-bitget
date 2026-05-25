/*
FILE: types/balance.go

DESCRIPTION:
Wallet state — protocol-common across every Bitget profile, sourced from
GET /api/v2/{mix,spot}/account/* endpoints and the corresponding WebSocket
"account" channel.

The struct flattens Bitget's two-level structure (account totals +
per-coin breakdown) into a Balance + []CoinBalance pair. The MIX
endpoints expose a single coin per request (USDT for USDT-FUTURES, USDC
for USDC-FUTURES, etc.); the spot endpoints return a list. The shared
struct fits both — a single-coin response simply yields a Balance with
one CoinBalance.
*/

package types

import "github.com/shopspring/decimal"

// Balance — account-level wallet state.
type Balance struct {
	// MarginCoin — settlement coin of the account (USDT / USDC / BTC / ...).
	// Empty on aggregated views.
	MarginCoin string
	// TotalEquity — total account equity, in MarginCoin.
	TotalEquity decimal.Decimal
	// AvailableBalance — funds available to open new positions or place
	// new orders (MIX) / available spot balance (spot).
	AvailableBalance decimal.Decimal
	// LockedBalance — balance reserved by open orders / position margin.
	LockedBalance decimal.Decimal
	// UnrealizedPnL — unrealised PnL across MIX positions; zero for spot.
	UnrealizedPnL decimal.Decimal
	// MaintenanceMargin — sum of maintenance margin across MIX positions.
	MaintenanceMargin decimal.Decimal
	// Coins — per-currency breakdown. For MIX endpoints the slice holds
	// at most one entry corresponding to MarginCoin; for spot it lists
	// every funded coin.
	Coins []CoinBalance
}

// CoinBalance — wallet state for a single asset within Balance.
type CoinBalance struct {
	Coin             string
	Equity           decimal.Decimal
	Available        decimal.Decimal
	Frozen           decimal.Decimal
	Locked           decimal.Decimal
	UsdValue         decimal.Decimal
	UnrealizedPnL    decimal.Decimal
	CumRealizedPnL   decimal.Decimal
	UsdtEquity       decimal.Decimal
	BtcEquity        decimal.Decimal
}
