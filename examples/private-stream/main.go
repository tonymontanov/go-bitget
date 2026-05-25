/*
FILE: examples/private-stream/main.go

DESCRIPTION:
End-to-end demo of the signed WebSocket surface (M5) for the MIX
profile. Subscribes to all three private channels — orders / positions
/ account — and prints every push for `duration` seconds. Useful for:

  - Verifying that API credentials work for the signed WS endpoint.
  - Smoke-testing per-symbol filtering (instId on orders/positions).
  - Sanity-checking that account balance pushes arrive on the
    configured marginCoin (USDT by default).

USAGE (env-vars are mandatory):

	export BITGET_API_KEY=...
	export BITGET_SECRET_KEY=...
	export BITGET_PASSPHRASE=...
	go run ./examples/private-stream                 # defaults: BTCUSDT, 60 seconds
	go run ./examples/private-stream -symbol ETHUSDT -duration 5m

This example is read-only — it never places or cancels orders. To see
push frames, place a manual order on Bitget UI / via examples/place-order
in a second terminal while this one runs.
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
	"github.com/tonymontanov/go-bitget/v2/mix"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func main() {
	var symbol string
	var duration time.Duration
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "MIX symbol to subscribe to (orders / positions)")
	flag.DurationVar(&duration, "duration", 60*time.Second, "how long to keep the WS subscription open")
	flag.Parse()

	var apiKey string = os.Getenv("BITGET_API_KEY")
	var secretKey string = os.Getenv("BITGET_SECRET_KEY")
	var passphrase string = os.Getenv("BITGET_PASSPHRASE")
	if apiKey == "" || secretKey == "" || passphrase == "" {
		log.Fatal("BITGET_API_KEY / BITGET_SECRET_KEY / BITGET_PASSPHRASE env-vars are required")
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

	var mc *mix.Client = c.Mix().(*mix.Client)

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
	//    (new / partially_filled / filled / cancelled / rejected) on
	//    the requested symbol. The SDK filters by instId on its side
	//    so the handler only sees pushes for `symbol`.
	err = mc.Stream().WatchOrders(ctx, symbol,
		func(o mixtypes.OrderInfo) {
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

	// 2) WatchPositions — size / margin / pnl / liquidation-price
	//    updates per symbol.
	err = mc.Stream().WatchPositions(ctx, symbol,
		func(p mixtypes.PositionInfo) {
			fmt.Printf("[position] %s side=%s qty=%s avgEntry=%s mark=%s unPnL=%s liqPx=%s lev=%d\n",
				p.Symbol, p.HoldSide,
				p.Quantity, p.AvgOpenPrice, p.MarkPrice,
				p.UnrealizedPnL, p.LiquidationPrice, p.Leverage)
		},
		errHandler,
	)
	if err != nil {
		log.Fatalf("WatchPositions: %v", err)
	}

	// 3) WatchAccount — wallet balance per margin coin (the SDK uses
	//    the marginCoin pinned at construction time, USDT by default).
	err = mc.Stream().WatchAccount(ctx,
		func(b roottypes.Balance) {
			fmt.Printf("[account] coin=%s equity=%s available=%s locked=%s unPnL=%s\n",
				b.MarginCoin, b.TotalEquity, b.AvailableBalance,
				b.LockedBalance, b.UnrealizedPnL)
		},
		errHandler,
	)
	if err != nil {
		log.Fatalf("WatchAccount: %v", err)
	}

	fmt.Printf("subscribed to orders/positions/account for %s; waiting %s for pushes...\n",
		symbol, duration)
	select {
	case <-time.After(duration):
		fmt.Println("duration elapsed — closing stream")
	case <-ctx.Done():
		fmt.Println("context cancelled — closing stream")
	}
	if err = mc.Stream().Close(); err != nil {
		log.Printf("Stream.Close: %v", err)
	}
}
