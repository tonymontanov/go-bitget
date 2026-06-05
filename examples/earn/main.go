/*
FILE: examples/earn/main.go

DESCRIPTION:
Read-only demo of the EARN and CONVERT profiles. It lists the Earn
account overview, the savings / shark-fin / on-chain-elite products and
the caller's holdings, the crypto-loan currencies and ongoing loans, and
the convert (flash-swap) currency list plus a sample quote.

The example is deliberately READ-ONLY: it never subscribes, redeems,
borrows, repays, or converts anything. Write endpoints (Subscribe /
Redeem / Borrow / Repay / Trade / ...) move real funds and are out of
scope for a demo.

The blank imports of the earn and convert packages register their lazy
factories so bitget.Client.Earn() / .Convert() resolve.

USAGE (env-vars are mandatory):

	export BITGET_API_KEY=...
	export BITGET_SECRET_KEY=...
	export BITGET_PASSPHRASE=...
	go run ./examples/earn

Section-specific BITGET_EARN_* variables take precedence over the
generic BITGET_* triple when set.
*/

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/convert"
	convtypes "github.com/tonymontanov/go-bitget/v2/convert/types"
	"github.com/tonymontanov/go-bitget/v2/earn"
)

func main() {
	var apiKey, secretKey, passphrase string = resolveCreds()
	if apiKey == "" || secretKey == "" || passphrase == "" {
		log.Fatal("BITGET_EARN_API_KEY / BITGET_EARN_SECRET_KEY / BITGET_EARN_PASSPHRASE " +
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

	var en *earn.Client = c.Earn().(*earn.Client)
	var cv *convert.Client = c.Convert().(*convert.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	earnSection(ctx, en)
	convertSection(ctx, cv)
}

func earnSection(ctx context.Context, en *earn.Client) {
	fmt.Println("== EARN ==")

	// Account overview across all earn products.
	if assets, err := en.Account().GetAssets(ctx, ""); err != nil {
		logCallError("Earn.Account.GetAssets", err)
	} else {
		fmt.Printf("earn account: %d coin(s) held\n", len(assets))
		for i := 0; i < len(assets) && i < 3; i++ {
			fmt.Printf("  %s amount=%s\n", assets[i].Coin, assets[i].Amount)
		}
	}

	// Savings: flexible products + held positions.
	if prods, err := en.Savings().GetProducts(ctx, "", "available"); err != nil {
		logCallError("Earn.Savings.GetProducts", err)
	} else {
		fmt.Printf("savings: %d product(s) available\n", len(prods))
		if len(prods) > 0 {
			fmt.Printf("  e.g. %s (%s) periodType=%s tiers=%d\n",
				prods[0].Coin, prods[0].ProductID, prods[0].PeriodType, len(prods[0].ApyList))
		}
	}
	if held, err := en.Savings().GetAssets(ctx, "flexible"); err != nil {
		logCallError("Earn.Savings.GetAssets", err)
	} else {
		fmt.Printf("savings: %d flexible position(s) held\n", len(held))
	}

	// Shark Fin: products on offer.
	if prods, err := en.SharkFin().GetProducts(ctx, ""); err != nil {
		logCallError("Earn.SharkFin.GetProducts", err)
	} else {
		fmt.Printf("sharkfin: %d product(s)\n", len(prods))
	}

	// On-Chain Elite: products + held assets.
	if prods, err := en.Elite().GetProducts(ctx); err != nil {
		logCallError("Earn.Elite.GetProducts", err)
	} else {
		fmt.Printf("elite: %d product(s)\n", len(prods))
	}

	// Crypto Loan: public currency table + ongoing orders.
	if cur, err := en.Loan().GetCurrencies(ctx, ""); err != nil {
		logCallError("Earn.Loan.GetCurrencies", err)
	} else {
		fmt.Printf("loan: %d borrowable / %d collateral coin(s)\n", len(cur.LoanInfos), len(cur.PledgeInfos))
	}
	if orders, err := en.Loan().GetOngoingOrders(ctx, "", "", ""); err != nil {
		logCallError("Earn.Loan.GetOngoingOrders", err)
	} else {
		fmt.Printf("loan: %d ongoing order(s)\n", len(orders))
	}
}

func convertSection(ctx context.Context, cv *convert.Client) {
	fmt.Println("== CONVERT ==")

	var coins []convtypes.ConvertCurrency
	var err error
	if coins, err = cv.GetCurrencies(ctx); err != nil {
		logCallError("Convert.GetCurrencies", err)
		return
	}
	fmt.Printf("convert: %d coin(s) available\n", len(coins))
	if len(coins) == 0 {
		return
	}

	// Sample RFQ: quote converting 1 unit of the first listed coin to USDT
	// (skipped when the coin already is USDT). This only requests a price;
	// it does NOT execute the swap.
	var from string = coins[0].Coin
	if from == "USDT" && len(coins) > 1 {
		from = coins[1].Coin
	}
	if from == "USDT" {
		return
	}
	if q, qerr := cv.GetQuotedPrice(ctx, from, "USDT", "1", ""); qerr != nil {
		logCallError("Convert.GetQuotedPrice", qerr)
	} else {
		fmt.Printf("convert quote: 1 %s -> %s USDT (price=%s, traceId=%s)\n",
			from, q.ToCoinSize, q.CnvtPrice, q.TraceID)
	}
}

// logCallError classifies a read failure with the usual SDK predicates.
func logCallError(op string, err error) {
	switch {
	case bitget.IsAuth(err):
		log.Printf("%s: auth failed (check key/secret/passphrase + IP whitelist + permissions): %v", op, err)
	case bitget.IsRateLimit(err):
		log.Printf("%s: rate-limited — back off and retry: %v", op, err)
	default:
		log.Printf("%s: %v", op, err)
	}
}

// resolveCreds reads the section-specific BITGET_EARN_* credentials,
// falling back to the generic BITGET_* triple when they are unset.
func resolveCreds() (apiKey, secretKey, passphrase string) {
	apiKey = firstNonEmpty(os.Getenv("BITGET_EARN_API_KEY"), os.Getenv("BITGET_API_KEY"))
	secretKey = firstNonEmpty(os.Getenv("BITGET_EARN_SECRET_KEY"), os.Getenv("BITGET_SECRET_KEY"))
	passphrase = firstNonEmpty(os.Getenv("BITGET_EARN_PASSPHRASE"), os.Getenv("BITGET_PASSPHRASE"))
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
