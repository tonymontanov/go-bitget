/*
FILE: mix/types/position-info.go

DESCRIPTION:
PositionInfo — view of one open position, returned by
GET /api/v2/mix/position/single-position and
GET /api/v2/mix/position/all-position.

M1 ships the SHAPE so the desk adapter compiles against the final type;
M3 wires the account sub-client.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// PositionInfo — open-position snapshot for one symbol.
type PositionInfo struct {
	Symbol           string
	HoldSide         HoldSide
	MarginMode       roottypes.MarginMode
	MarginCoin       string
	Quantity         decimal.Decimal
	Available        decimal.Decimal
	Locked           decimal.Decimal
	AvgOpenPrice     decimal.Decimal
	MarkPrice        decimal.Decimal
	LiquidationPrice decimal.Decimal
	Leverage         int
	UnrealizedPnL    decimal.Decimal
	RealizedPnL      decimal.Decimal
	CreatedAtMs      int64
	UpdatedAtMs      int64
}
