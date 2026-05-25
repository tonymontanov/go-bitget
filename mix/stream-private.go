/*
FILE: mix/stream-private.go

DESCRIPTION:
Private WebSocket sub-client for Bitget MIX. M1 ships ONLY the type
+ Watch* signatures; M5 wires the actual implementations. Stream and
StreamClient are the same type — splitting public vs private here
is purely for readability.

PRIVATE CHANNELS (M5):
  - "orders"     : order lifecycle events (place/fill/cancel).
  - "positions"  : position size / margin / pnl events.
  - "account"    : balance / equity events for the configured product type.
*/

package mix

import (
	"context"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// errNotImplementedStream — sentinel for stream methods that are not
// yet wired (M5).
func errNotImplementedStream(method, milestone string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "",
		"mix.Stream."+method+": not implemented yet ("+milestone+")", nil)
}

// WatchOrders — placeholder (M5).
func (s *StreamClient) WatchOrders(
	ctx context.Context,
	symbol string,
	handler func(mixtypes.OrderInfo),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchOrders", "M5")
}

// WatchPositions — placeholder (M5).
func (s *StreamClient) WatchPositions(
	ctx context.Context,
	symbol string,
	handler func(mixtypes.PositionInfo),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchPositions", "M5")
}

// WatchAccount — placeholder (M5). Bitget pushes balance updates per
// margin coin (USDT for USDT-FUTURES, etc.). The handler receives the
// reusable cross-profile Balance type from the root types package.
func (s *StreamClient) WatchAccount(
	ctx context.Context,
	handler func(roottypes.Balance),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchAccount", "M5")
}
