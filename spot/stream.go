/*
FILE: spot/stream.go

DESCRIPTION:
WebSocket subscription sub-client for the Bitget V2 SPOT profile.
M1 ships the struct + constructor + state fields the M4 / M5 wiring
will need. Watch* methods are introduced in M4 (public) and M5
(private), mirroring the mix/ rollout under v1.0.

CHANNELS (instType="SPOT"):

  Public  /v2/ws/public:
    - books / books5 / books15 / books50  (M4, with CRC32 resync via
                                           internal/bgcommon/orderbook
                                           — the engine is shared with
                                           mix and uta).
    - ticker                              (M4)
    - trade                               (M4)
    - candle{interval}                    (M4)

  Private /v2/ws/private:
    - account                             (M5)
    - orders                              (M5)
    - fills                               (M5)

The orderbook engine, FlexString, RestDoer interface and numeric
parsers all come from internal/bgcommon — see spot/doc.go for the
two-layer architecture rule. The spot StreamClient never re-implements
any of those; it only owns:

  - the *ws.Conn pair (public / private — lazily constructed),
  - the per-symbol orderbook engine map (one *orderbook.Engine per
    subscribed `books*` arg key),
  - the orderbookSubs map kept so resync can re-Subscribe the SAME
    Subscription object (with handler + Reset hook intact) after
    Unsubscribe wipes the registry,
  - the resyncing flag preventing back-to-back resync goroutines from
    racing each other,
  - a private-state bundle (constructed lazily on the first private
    Watch* call) for the login + auto-resub lifecycle. M5 fills it.

CLOSE SEMANTICS:

closeOnce ensures that a Close() call (added in M4) cancels both the
public and private contexts exactly once even under concurrent shut-
down from multiple watchers.
*/

package spot

import (
	"context"
	"sync"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon/orderbook"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
)

// StreamClient — WebSocket subscription sub-client. Built once per
// spot.Client (see client.go) and safe for concurrent use.
type StreamClient struct {
	c *Client

	mu         sync.Mutex
	publicConn *ws.Conn
	publicCtx  context.Context
	closeOnce  sync.Once

	// engines holds one orderbook engine per subscribed symbol. Indexed
	// by ws.SubscriptionArg.Key() so the same key the registry uses
	// also reaches the engine. M4 populates this map; M1 keeps it
	// allocated so concurrent Watch* calls do not need to re-Lock for
	// initialisation.
	engines map[string]*orderbook.Engine

	// orderbookSubs retains the books-channel Subscription per arg so
	// scheduleResync can re-Subscribe the SAME object (with its handler
	// and Reset hook intact) after Unsubscribe wipes it from the
	// ws.Conn registry. M4 wires this; M1 only allocates.
	orderbookSubs map[string]*ws.Subscription

	// resyncing is the per-arg flag preventing back-to-back resync
	// goroutines from racing each other. M4 wires this; M1 only
	// allocates.
	resyncing map[string]struct{}
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{
		c:             c,
		engines:       make(map[string]*orderbook.Engine, 16),
		orderbookSubs: make(map[string]*ws.Subscription, 16),
		resyncing:     make(map[string]struct{}, 8),
	}
}
