/*
FILE: examples/marketdata/main.go

DESCRIPTION:
End-to-end demo of the public market-data surface of the MIX profile —
no API credentials required. Walks through:

  1. REST: GetSymbolInfo → GetMarketTicker → GetOrderBook (top-50 snapshot).
  2. WebSocket: WatchOrderbook (full L2 book maintained locally with
     CRC32 validation) for ~10 seconds, printing the best bid/ask on
     every depth change.

USAGE:

	go run ./examples/marketdata             # defaults: BTCUSDT, 10 seconds
	go run ./examples/marketdata -symbol ETHUSDT -duration 30s

The example prints to stdout and never exits non-zero unless the
network is unreachable or the SDK reports a hard error.
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
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func main() {
	var symbol string
	var duration time.Duration
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "MIX symbol (e.g. BTCUSDT, ETHUSDT)")
	flag.DurationVar(&duration, "duration", 10*time.Second, "how long to keep the WS subscription open")
	flag.Parse()

	var cfg bitget.Config = bitget.DefaultConfig()
	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		log.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	var mc *mix.Client = c.Mix().(*mix.Client)
	if mc == nil {
		log.Fatal("mix factory is not registered (this should not happen — the package is imported)")
	}

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("=== REST market-data for %s ===\n", symbol)
	runREST(ctx, mc, symbol)

	fmt.Println()
	fmt.Printf("=== WebSocket book stream for %s (%s) ===\n", symbol, duration)
	runStream(mc, symbol, duration)
}

// runREST fetches and prints the three REST market-data endpoints. Errors
// are logged but do not stop the program — partial output is still useful
// for triage.
func runREST(ctx context.Context, mc *mix.Client, symbol string) {
	var info, errInfo = mc.MarketData().GetSymbolInfo(ctx, symbol)
	if errInfo != nil {
		log.Printf("GetSymbolInfo: %v", errInfo)
	} else {
		fmt.Printf("symbol=%s base=%s quote=%s priceTick=%s sizeStep=%s minQty=%s minNotional=%s maxLev=%d\n",
			info.Symbol, info.BaseCoin, info.QuoteCoin,
			info.PriceTick, info.SizeStep,
			info.MinTradeNum, info.MinTradeUSDT, info.MaxLever)
	}

	var ticker, errTicker = mc.MarketData().GetMarketTicker(ctx, symbol)
	if errTicker != nil {
		log.Printf("GetMarketTicker: %v", errTicker)
	} else {
		fmt.Printf("ticker last=%s mark=%s index=%s bid=%s ask=%s\n",
			ticker.LastPrice, ticker.MarkPrice, ticker.IndexPrice,
			ticker.BidPrice, ticker.AskPrice)
	}

	var book, errBook = mc.MarketData().GetOrderBook(ctx, symbol, 50)
	if errBook != nil {
		log.Printf("GetOrderBook: %v", errBook)
		return
	}
	if len(book.Bids) > 0 && len(book.Asks) > 0 {
		fmt.Printf("book best_bid=%s@%s best_ask=%s@%s (depth=%d/%d)\n",
			book.Bids[0].Price, book.Bids[0].Size,
			book.Asks[0].Price, book.Asks[0].Size,
			len(book.Bids), len(book.Asks))
	}
}

// runStream subscribes to the WS depth stream and prints best-bid/ask on
// every snapshot. Press Ctrl+C to exit early; otherwise the example
// returns after `duration`.
func runStream(mc *mix.Client, symbol string, duration time.Duration) {
	var streamCtx context.Context
	var streamCancel context.CancelFunc
	streamCtx, streamCancel = context.WithCancel(context.Background())
	defer streamCancel()

	var sigCh = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("interrupt received — closing stream...")
		streamCancel()
	}()

	var book roottypes.OrderBookSnapshot
	var bookErr error
	bookErr = mc.Stream().WatchOrderbook(streamCtx, symbol,
		func(s roottypes.OrderBookSnapshot) {
			book = s
			if len(book.Bids) > 0 && len(book.Asks) > 0 {
				fmt.Printf("[%d] bid=%s@%s | ask=%s@%s\n",
					book.TsMs,
					book.Bids[0].Price, book.Bids[0].Size,
					book.Asks[0].Price, book.Asks[0].Size,
				)
			}
		},
		func(streamErr error) {
			log.Printf("stream error: %v", streamErr)
		},
	)
	if bookErr != nil {
		log.Fatalf("WatchOrderbook: %v", bookErr)
	}

	fmt.Printf("subscribed; collecting frames for %s...\n", duration)
	select {
	case <-time.After(duration):
		fmt.Println("duration elapsed — closing stream")
	case <-streamCtx.Done():
		fmt.Println("context cancelled — closing stream")
	}
	if err := mc.Stream().Close(); err != nil {
		log.Printf("Stream.Close: %v", err)
	}
}
