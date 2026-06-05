/*
FILE: examples/uta/main.go

DESCRIPTION:
Read-only demo of the V3 UNIFIED TRADING ACCOUNT (uta) profile. It prints
the V3 server time, instruments, a ticker, the top of book, recent
candles, the current funding rate and position tiers (all UNSIGNED — they
work with no credentials), then — when credentials are present — the
unified account assets / settings / fee-rate, open orders, recent fills,
current positions and any open strategy (plan) orders.

The example is deliberately READ-ONLY: it never places, modifies or
cancels orders, never changes leverage / hold mode and never closes
positions. Those mutate live state, so only read endpoints are exercised.

DEMO TRADING: set BITGET_DEMO=1 (with a Demo API Key) to route every
request to Bitget's paper-trading environment via the `paptrading: 1`
header. The production host is used either way.

The blank import of the uta package registers its lazy factory so
bitget.Client.UTA() resolves.

USAGE:

	# public (unsigned) section works with no credentials:
	go run ./examples/uta

	# signed sections need the usual triple (optionally a Demo API Key):
	export BITGET_API_KEY=...
	export BITGET_SECRET_KEY=...
	export BITGET_PASSPHRASE=...
	export BITGET_DEMO=1   # optional: paper trading
	go run ./examples/uta
*/

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/uta"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func main() {
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = os.Getenv("BITGET_API_KEY")
	cfg.SecretKey = os.Getenv("BITGET_SECRET_KEY")
	cfg.Passphrase = os.Getenv("BITGET_PASSPHRASE")
	cfg.Demo = os.Getenv("BITGET_DEMO") == "1"

	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		log.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	var u *uta.Client = c.UTA().(*uta.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	publicSection(ctx, u)

	if cfg.APIKey == "" || cfg.SecretKey == "" || cfg.Passphrase == "" {
		fmt.Println("\n[signed sections skipped: set BITGET_API_KEY / BITGET_SECRET_KEY / BITGET_PASSPHRASE]")
		return
	}
	if cfg.Demo {
		fmt.Println("\n[demo trading: paptrading header enabled]")
	}
	accountSection(ctx, u)
	tradeSection(ctx, u)
}

const symbol = "BTCUSDT"

var futures = utatypes.CategoryUSDTFutures

func publicSection(ctx context.Context, u *uta.Client) {
	fmt.Println("== PUBLIC (unsigned) ==")

	var ms, err = u.Public().GetServerTime(ctx)
	if err != nil {
		log.Fatalf("GetServerTime: %v", err)
	}
	fmt.Printf("server time: %d (%s)\n", ms, time.UnixMilli(ms).UTC().Format(time.RFC3339))

	var insts, ierr = u.Public().GetInstruments(ctx, futures, symbol)
	if ierr != nil {
		log.Fatalf("GetInstruments: %v", ierr)
	}
	if len(insts) > 0 {
		fmt.Printf("instrument %s: pricePrec=%d qtyPrec=%d maxLev=%s\n",
			insts[0].Symbol, insts[0].PricePrecision, insts[0].QuantityPrecision, insts[0].MaxLeverage)
	}

	var tks, terr = u.Public().GetTickers(ctx, futures, symbol)
	if terr != nil {
		log.Fatalf("GetTickers: %v", terr)
	}
	if len(tks) > 0 {
		fmt.Printf("ticker %s: last=%s mark=%s funding=%s\n", tks[0].Symbol, tks[0].LastPrice, tks[0].MarkPrice, tks[0].FundingRate)
	}

	var ob, oerr = u.Public().GetOrderBook(ctx, futures, symbol, 5)
	if oerr != nil {
		log.Fatalf("GetOrderBook: %v", oerr)
	}
	if len(ob.Asks) > 0 && len(ob.Bids) > 0 {
		fmt.Printf("book %s: bestBid=%s bestAsk=%s\n", symbol, ob.Bids[0].Price, ob.Asks[0].Price)
	}

	var ks, kerr = u.Public().GetCandles(ctx, uta.CandlesQuery{Category: futures, Symbol: symbol, Interval: utatypes.Interval1H, Limit: 3})
	if kerr != nil {
		log.Fatalf("GetCandles: %v", kerr)
	}
	fmt.Printf("candles 1H: %d rows\n", len(ks))

	var fr, ferr = u.Public().GetCurrentFundingRate(ctx, symbol)
	if ferr == nil {
		fmt.Printf("funding %s: rate=%s interval=%s\n", fr.Symbol, fr.FundingRate, fr.FundingRateInterval)
	}

	var tiers, perr = u.Public().GetPositionTier(ctx, futures, symbol, "")
	if perr == nil {
		fmt.Printf("position tiers: %d\n", len(tiers))
	}
}

func accountSection(ctx context.Context, u *uta.Client) {
	fmt.Println("\n== ACCOUNT (signed) ==")

	var assets, err = u.Account().GetAssets(ctx)
	if err != nil {
		fmt.Printf("[GetAssets note] %v\n", err)
	} else {
		fmt.Printf("account equity: %s USDT (%d coins)\n", assets.USDTEquity, len(assets.Assets))
	}

	var st, serr = u.Account().GetSettings(ctx)
	if serr != nil {
		fmt.Printf("[GetSettings note] %v\n", serr)
	} else {
		fmt.Printf("settings: mode=%s level=%s holdMode=%s\n", st.AccountMode, st.AccountLevel, st.HoldMode)
	}

	var fee, ferr = u.Account().GetFeeRate(ctx, futures, symbol)
	if ferr != nil {
		fmt.Printf("[GetFeeRate note] %v\n", ferr)
	} else {
		fmt.Printf("fee %s: maker=%s taker=%s\n", symbol, fee.MakerFeeRate, fee.TakerFeeRate)
	}
}

func tradeSection(ctx context.Context, u *uta.Client) {
	fmt.Println("\n== TRADE / POSITION / STRATEGY (signed, read-only) ==")

	var open, _, err = u.Trade().GetUnfilledOrders(ctx, uta.OrdersQuery{Category: futures, Symbol: symbol})
	if err != nil {
		fmt.Printf("[GetUnfilledOrders note] %v\n", err)
	} else {
		fmt.Printf("open orders: %d\n", len(open))
	}

	var fills, _, ferr = u.Trade().GetFills(ctx, uta.FillsQuery{Limit: 5})
	if ferr != nil {
		fmt.Printf("[GetFills note] %v\n", ferr)
	} else {
		fmt.Printf("recent fills: %d\n", len(fills))
	}

	var pos, perr = u.Position().GetCurrentPositions(ctx, futures, symbol, "")
	if perr != nil {
		fmt.Printf("[GetCurrentPositions note] %v\n", perr)
	} else {
		fmt.Printf("current positions: %d\n", len(pos))
	}

	var plans, serr = u.Strategy().GetUnfilledOrders(ctx, futures, "")
	if serr != nil {
		fmt.Printf("[Strategy.GetUnfilledOrders note] %v\n", serr)
	} else {
		fmt.Printf("open plan orders: %d\n", len(plans))
	}
}
