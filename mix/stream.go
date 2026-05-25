/*
FILE: mix/stream.go

DESCRIPTION:
Public WebSocket sub-client for Bitget MIX. M1 ships ONLY the type
+ Watch* signatures; every method returns ErrorKindInvalidRequest
with "not implemented yet" until the corresponding milestone wires
the real WS plumbing:

  - M4: WatchOrderbook (+ orderbook engine snapshot/delta/seq/resync),
    WatchTicker, WatchTrades, WatchKline.
  - M5: WatchOrders, WatchPositions, WatchAccount (private channels).

CHANNEL NOTES (informational, used during M4):

  - "books"        : full-depth snapshot + incremental updates with
    CRC32 checksum on every frame.
  - "books5"       : 5-level snapshot (no diff), no checksum.
  - "books15"      : 15-level snapshot.
  - "ticker"       : last/mark/index price + funding.
  - "trade"        : public trade tape.
  - "candle{tf}"   : klines (e.g. "candle1m").

CONTRACT KEPT STABLE:
The Watch* signatures land here in M1 so the desk's BitgetMixConnector
can be written against them; M4/M5 fill the bodies without changing
the interface.
*/

package mix

import (
	"context"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// StreamClient — WebSocket subscription sub-client.
type StreamClient struct {
	c *Client
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// errNotImplementedStream — sentinel for skeleton stream methods.
func errNotImplementedStream(method, milestone string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Stream."+method+": not implemented yet ("+milestone+")", nil)
}

// ---------------------------------------------------------------------
// Public channels (M4).
// ---------------------------------------------------------------------

// WatchOrderbook — placeholder (M4).
func (s *StreamClient) WatchOrderbook(
	ctx context.Context,
	symbol string,
	handler func(roottypes.OrderBookSnapshot),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchOrderbook", "M4")
}

// WatchTicker — placeholder (M4).
func (s *StreamClient) WatchTicker(
	ctx context.Context,
	symbol string,
	handler func(mixtypes.MarketTicker),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchTicker", "M4")
}

// WatchTrades — placeholder (M4).
func (s *StreamClient) WatchTrades(
	ctx context.Context,
	symbol string,
	handler func(roottypes.TradeUpdate),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchTrades", "M4")
}

// WatchKline — placeholder (M4).
func (s *StreamClient) WatchKline(
	ctx context.Context,
	symbol string,
	timeframe roottypes.Timeframe,
	handler func(roottypes.KlineUpdate),
	errHandler func(error),
) error {
	return errNotImplementedStream("WatchKline", "M4")
}
