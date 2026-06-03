/*
FILE: examples/spot-private-stream/main.go

DESCRIPTION:
End-to-end demo of the signed WebSocket surface (M5) for the SPOT
profile. Mirrors examples/private-stream (MIX) minus the positions
channel — spot is cash-only, so the private channels are orders /
account / fills. Subscribes to all three and prints every push for
`duration` seconds. Useful for:

  - Verifying that API credentials work for the signed WS endpoint.
  - Smoke-testing per-symbol filtering (instId on orders/fills).
  - Sanity-checking that per-asset balance pushes arrive.

USAGE (env-vars are mandatory):

	export BITGET_SPOT_API_KEY=...
	export BITGET_SPOT_SECRET_KEY=...
	export BITGET_SPOT_PASSPHRASE=...
	go run ./examples/spot-private-stream                 # defaults: BTCUSDT, 60 seconds
	go run ./examples/spot-private-stream -symbol ETHUSDT -duration 5m

The example falls back to the generic BITGET_API_KEY / BITGET_SECRET_KEY /
BITGET_PASSPHRASE triple when the BITGET_SPOT_* variables are unset.

This example is read-only — it never places or cancels orders. To see
push frames, place a manual order on the Bitget UI / via
examples/spot-place-order in a second terminal while this one runs.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/spot"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
)

func main() {
	var symbol string
	var duration time.Duration
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "SPOT symbol to filter orders / fills on")
	flag.DurationVar(&duration, "duration", 60*time.Second, "how long to keep the WS subscription open")
	flag.Parse()

	var apiKey, secretKey, passphrase string = resolveCreds()
	if apiKey == "" || secretKey == "" || passphrase == "" {
		log.Fatal("BITGET_SPOT_API_KEY / BITGET_SPOT_SECRET_KEY / BITGET_SPOT_PASSPHRASE " +
			"(or the generic BITGET_* triple) env-vars are required")
	}

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = apiKey
	cfg.SecretKey = secretKey
	cfg.Passphrase = passphrase

	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		log.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	var sc *spot.Client = c.Spot().(*spot.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var sigCh = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("interrupt received — closing private stream...")
		cancel()
	}()

	var errHandler = func(streamErr error) {
		log.Printf("private stream error: %v", streamErr)
	}

	// 1) WatchOrders — pushed for every order lifecycle transition
	//    (live / partially_filled / filled / cancelled) on the
	//    requested symbol. The SDK subscribes globally (instId=
	//    "default") and filters by symbol on its side, so the handler
	//    only sees pushes for `symbol`.
	err = sc.Stream().WatchOrders(ctx, symbol,
		func(o spottypes.OrderInfo) {
			fmt.Printf("[order] %s status=%s side=%s qty=%s filled=%s avgFill=%s clientOID=%s\n",
				o.OrderID, o.Status, o.Side,
				o.Quantity, o.FilledQuantity, o.AvgFilledPrice,
				o.ClientOrderID)
		},
		errHandler,
	)
	if err != nil {
		switch {
		case bitget.IsAuth(err):
			log.Fatalf("WatchOrders: auth failed (check creds + IP whitelist): %v", err)
		default:
			log.Fatalf("WatchOrders: %v", err)
		}
	}

	// 2) WatchAccount — per-asset balance snapshots. Filter on "USDT"
	//    so we only print the quote-coin row; pass "default" to see
	//    every asset on the account.
	err = sc.Stream().WatchAccount(ctx, "USDT",
		func(a spottypes.AccountUpdate) {
			fmt.Printf("[account] coin=%s available=%s frozen=%s locked=%s\n",
				a.Coin, a.Available, a.Frozen, a.Locked)
		},
		errHandler,
	)
	if err != nil {
		log.Fatalf("WatchAccount: %v", err)
	}

	// 3) WatchFills — per-execution rows for the requested symbol.
	//    Spot fills do NOT carry clientOid (venue invariant); join on
	//    OrderID against the orders channel above if you need it.
	err = sc.Stream().WatchFills(ctx, symbol,
		func(f spottypes.FillUpdate) {
			fmt.Printf("[fill] order=%s trade=%s side=%s price=%s size=%s amount=%s scope=%s\n",
				f.OrderID, f.TradeID, f.Side,
				f.PriceAvg, f.Size, f.Amount, f.TradeScope)
		},
		errHandler,
	)
	if err != nil {
		log.Fatalf("WatchFills: %v", err)
	}

	fmt.Printf("subscribed to orders/account/fills for %s; waiting %s for pushes...\n",
		symbol, duration)
	select {
	case <-time.After(duration):
		fmt.Println("duration elapsed — closing stream")
	case <-ctx.Done():
		fmt.Println("context cancelled — closing stream")
	}
	if err = sc.Stream().Close(); err != nil {
		log.Printf("Stream.Close: %v", err)
	}
}

// resolveCreds reads the section-specific BITGET_SPOT_* credentials,
// falling back to the generic BITGET_* triple when they are unset.
func resolveCreds() (apiKey, secretKey, passphrase string) {
	apiKey = firstNonEmpty(os.Getenv("BITGET_SPOT_API_KEY"), os.Getenv("BITGET_API_KEY"))
	secretKey = firstNonEmpty(os.Getenv("BITGET_SPOT_SECRET_KEY"), os.Getenv("BITGET_SECRET_KEY"))
	passphrase = firstNonEmpty(os.Getenv("BITGET_SPOT_PASSPHRASE"), os.Getenv("BITGET_PASSPHRASE"))
	return apiKey, secretKey, passphrase
}

// firstNonEmpty returns the first non-empty string of its arguments, or
// "" when all are empty.
func firstNonEmpty(values ...string) string {
	var i int
	for i = 0; i < len(values); i++ {
		if values[i] != "" {
			return values[i]
		}
	}
	return ""
}
