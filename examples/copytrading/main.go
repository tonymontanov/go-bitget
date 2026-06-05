/*
FILE: examples/copytrading/main.go

DESCRIPTION:
Read-only demo of the COPY-TRADING profile across all four product×role
sub-clients: FuturesFollower, SpotFollower (follower side, available to
any account) and — behind the -trader flag — FuturesTrader, SpotTrader
(lead side, which requires the account to be an approved Bitget elite
trader after KYC).

The example is deliberately READ-ONLY: it never follows, unfollows,
opens, closes, or reconfigures anything. It lists the caller's followed
traders (futures + spot) and, with -trader, prints the lead-trader order
summaries. Trader-side calls are guarded: a non-elite account gets a
friendly note instead of a hard failure, because the eligibility 4xx is
expected and not a bug.

The futures product type is pinned at construction (USDT-FUTURES by
default); spot copy trading ignores it. Pass -product-type to exercise
COIN / USDC futures.

USAGE (env-vars are mandatory):

	export BITGET_COPY_API_KEY=...
	export BITGET_COPY_SECRET_KEY=...
	export BITGET_COPY_PASSPHRASE=...
	go run ./examples/copytrading                       # follower reads only
	go run ./examples/copytrading -trader               # + lead-trader summaries
	go run ./examples/copytrading -product-type USDC-FUTURES

Falls back to the generic BITGET_API_KEY / BITGET_SECRET_KEY /
BITGET_PASSPHRASE triple when the BITGET_COPY_* variables are unset.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/copytrading"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func main() {
	var productTypeFlag string
	var withTrader bool
	flag.StringVar(&productTypeFlag, "product-type", "USDT-FUTURES", "futures venue: USDT-FUTURES | COIN-FUTURES | USDC-FUTURES (spot ignores it)")
	flag.BoolVar(&withTrader, "trader", false, "also read the lead-trader summaries (requires an approved elite-trader account)")
	flag.Parse()

	var apiKey, secretKey, passphrase string = resolveCreds()
	if apiKey == "" || secretKey == "" || passphrase == "" {
		log.Fatal("BITGET_COPY_API_KEY / BITGET_COPY_SECRET_KEY / BITGET_COPY_PASSPHRASE " +
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

	// Pin the futures product type explicitly so the demo can exercise
	// COIN / USDC venues; spot sub-clients ignore it.
	var cp *copytrading.Client = copytrading.NewClientWithProductType(c, roottypes.ProductType(productTypeFlag))

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1) Futures follower — my traders. Available to any account; an
	//    account that follows nobody simply gets an empty list.
	var futTraders, ferr = cp.FuturesFollower().GetMyTraders(ctx)
	if ferr != nil {
		logCallError("FuturesFollower.GetMyTraders", ferr)
	} else {
		fmt.Printf("futures: following %d trader(s)\n", len(futTraders))
		if len(futTraders) > 0 {
			fmt.Printf("  e.g. %s (%s) totalProfit=%s pairs=%v\n",
				futTraders[0].TraderName, futTraders[0].TraderID,
				futTraders[0].TraceTotalProfit, futTraders[0].CurrentTradingPairs)
		}
	}

	// 2) Spot follower — my traders.
	var spotTraders, serr = cp.SpotFollower().GetMyTraders(ctx)
	if serr != nil {
		logCallError("SpotFollower.GetMyTraders", serr)
	} else {
		fmt.Printf("spot: following %d trader(s)\n", len(spotTraders))
		if len(spotTraders) > 0 {
			fmt.Printf("  e.g. %s (%s) totalProfit=%s\n",
				spotTraders[0].TraderName, spotTraders[0].TraderID, spotTraders[0].TraceTotalProfit)
		}
	}

	if !withTrader {
		fmt.Println("(pass -trader to also read the lead-trader summaries)")
		return
	}

	// 3) Lead-trader summaries — only meaningful for an approved elite
	//    trader. A non-trader account gets an eligibility 4xx, which we
	//    treat as an informational note rather than a failure.
	var futSummary, fserr = cp.FuturesTrader().GetOrderSummary(ctx)
	if fserr != nil {
		logTraderEligibility("FuturesTrader.GetOrderSummary", fserr)
	} else {
		fmt.Printf("futures trader: followers=%d openOrders=%d totalPL=%s\n",
			futSummary.CurrentFollowerNum, futSummary.TradingOrderNum, futSummary.TotalPL)
	}

	var spotSummary, sserr = cp.SpotTrader().GetOrderSummary(ctx)
	if sserr != nil {
		logTraderEligibility("SpotTrader.GetOrderSummary", sserr)
	} else {
		fmt.Printf("spot trader: followers=%d/%d openOrders=%d totalPL=%s\n",
			spotSummary.CurrentFollowerNum, spotSummary.MaxFollowerNum,
			spotSummary.TradingOrderNum, spotSummary.TotalPL)
	}
}

// logCallError classifies a read failure with the usual SDK predicates.
func logCallError(op string, err error) {
	switch {
	case bitget.IsAuth(err):
		log.Printf("%s: auth failed (check key/secret/passphrase + IP whitelist + copy-trading permission): %v", op, err)
	case bitget.IsRateLimit(err):
		log.Printf("%s: rate-limited — back off and retry: %v", op, err)
	default:
		log.Printf("%s: %v", op, err)
	}
}

// logTraderEligibility treats the lead-trader eligibility 4xx as an
// expected informational note (the account is simply not an elite
// trader), and classifies everything else normally.
func logTraderEligibility(op string, err error) {
	if bitget.IsInvalidRequest(err) {
		fmt.Printf("%s: skipped — this account is not an approved elite trader (expected): %v\n", op, err)
		return
	}
	logCallError(op, err)
}

// resolveCreds reads the section-specific BITGET_COPY_* credentials,
// falling back to the generic BITGET_* triple when they are unset.
func resolveCreds() (apiKey, secretKey, passphrase string) {
	apiKey = firstNonEmpty(os.Getenv("BITGET_COPY_API_KEY"), os.Getenv("BITGET_API_KEY"))
	secretKey = firstNonEmpty(os.Getenv("BITGET_COPY_SECRET_KEY"), os.Getenv("BITGET_SECRET_KEY"))
	passphrase = firstNonEmpty(os.Getenv("BITGET_COPY_PASSPHRASE"), os.Getenv("BITGET_PASSPHRASE"))
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
