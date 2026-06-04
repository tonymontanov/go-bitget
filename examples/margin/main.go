/*
FILE: examples/margin/main.go

DESCRIPTION:
End-to-end demo of the signed REST trading surface of the MARGIN
profile (crossed by default). Mirrors examples/spot-place-order, but
adds the margin-specific bits:

  - the client is pinned to a margin MODE (crossed / isolated) at
    construction time — the mode drives the URL path segment on every
    call;
  - market data comes from the SPOT profile (margin trades spot
    instruments — there is NO margin price endpoint);
  - the order carries a loanType (here: normal — trade against existing
    collateral, no auto-borrow).

The example places a deeply post-only LIMIT BUY (priced 5% below the
best ask), lists open margin orders to read it back, then cancels it.
The order is post-only and far out-of-market, so it should never fill —
but it still reserves quote balance while it rests, so a funded margin
account is required for the placement to succeed.

USAGE (env-vars are mandatory):

	export BITGET_MARGIN_API_KEY=...
	export BITGET_MARGIN_SECRET_KEY=...
	export BITGET_MARGIN_PASSPHRASE=...
	go run ./examples/margin                       # defaults: crossed BTCUSDT 0.0001
	go run ./examples/margin -mode isolated -symbol ETHUSDT -qty 0.01

Falls back to the generic BITGET_API_KEY / BITGET_SECRET_KEY /
BITGET_PASSPHRASE triple when the BITGET_MARGIN_* variables are unset.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/margin"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	"github.com/tonymontanov/go-bitget/v2/spot"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func main() {
	var symbol, qty, modeFlag string
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "SPOT symbol traded on margin (e.g. BTCUSDT)")
	flag.StringVar(&qty, "qty", "0.0001", "order quantity in BASE coin (must be ≥ MinTradeAmount)")
	flag.StringVar(&modeFlag, "mode", "crossed", "margin mode: crossed | isolated")
	flag.Parse()

	var mode roottypes.MarginMode
	switch modeFlag {
	case "crossed":
		mode = roottypes.MarginModeCrossed
	case "isolated":
		mode = roottypes.MarginModeIsolated
	default:
		log.Fatalf("invalid -mode %q (want crossed | isolated)", modeFlag)
	}

	var apiKey, secretKey, passphrase string = resolveCreds()
	if apiKey == "" || secretKey == "" || passphrase == "" {
		log.Fatal("BITGET_MARGIN_API_KEY / BITGET_MARGIN_SECRET_KEY / BITGET_MARGIN_PASSPHRASE " +
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

	// Margin() returns the SDK default (crossed); for an explicit mode
	// we construct the client directly off the same parent so the
	// shared REST pool / signer are reused.
	var mc *margin.Client = margin.NewClientWithMode(c, mode)
	var sc *spot.Client = c.Spot().(*spot.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1) Price discovery via the SPOT profile — margin has no market
	//    data of its own (it trades spot instruments).
	var ticker spottypes.MarketTicker
	ticker, err = sc.MarketData().GetMarketTicker(ctx, symbol)
	if err != nil {
		log.Fatalf("GetMarketTicker: %v", err)
	}
	if ticker.AskPrice.IsZero() {
		log.Fatalf("ticker for %s has no ask price (delisted / halted symbol?)", symbol)
	}
	var price decimal.Decimal = ticker.AskPrice.Mul(decimal.NewFromFloat(0.95))
	var quantity decimal.Decimal = decimal.RequireFromString(qty)

	// 2) Place a deep post-only LIMIT BUY. LoanType=normal means "trade
	//    against existing collateral, do NOT auto-borrow". A LIMIT order
	//    denominates the size in BASE coin (BaseSize).
	var clientOID string = fmt.Sprintf("margin-example-%d", time.Now().UnixNano())
	var placed margintypes.OrderInfo
	placed, err = mc.Trading().CreateOrder(ctx, margintypes.CreateOrderRequest{
		Symbol:        symbol,
		Side:          roottypes.SideTypeBuy,
		OrderType:     roottypes.OrderTypeLimit,
		TimeInForce:   roottypes.TimeInForcePostOnly,
		BaseSize:      quantity,
		Price:         price,
		LoanType:      margintypes.LoanTypeNormal,
		ClientOrderID: clientOID,
	})
	if err != nil {
		switch {
		case bitget.IsAuth(err):
			log.Fatalf("CreateOrder: auth failed (check key/secret/passphrase + IP whitelist + margin permission): %v", err)
		case bitget.IsRateLimit(err):
			log.Fatalf("CreateOrder: rate-limited — back off and retry: %v", err)
		case bitget.IsInvalidRequest(err):
			log.Fatalf("CreateOrder: invalid request (qty step? min notional?): %v", err)
		default:
			log.Fatalf("CreateOrder: %v", err)
		}
	}
	fmt.Printf("placed [%s]: orderID=%s clientOID=%s symbol=%s side=%s type=%s qty=%s price=%s loanType=%s status=%s\n",
		mode, placed.OrderID, placed.ClientOrderID, placed.Symbol, placed.Side,
		placed.OrderType, placed.Quantity, placed.Price, placed.LoanType, placed.Status)

	// 3) Read it back through the open-orders query (margin requires the
	//    symbol; there is no per-id GetOrderDetail on this profile).
	var open []margintypes.OrderInfo
	open, err = mc.Account().GetOpenOrders(ctx, symbol)
	if err != nil {
		log.Printf("GetOpenOrders: %v (order is still placed; will continue to cancel)", err)
	} else {
		var i int
		for i = 0; i < len(open); i++ {
			if open[i].OrderID == placed.OrderID {
				fmt.Printf("open: status=%s filledQty=%s avgFillPrice=%s\n",
					open[i].Status, open[i].FilledQuantity, open[i].AvgFilledPrice)
			}
		}
	}

	// 4) Cancel before exit — an orphaned post-only buy keeps quote
	//    balance frozen until cleared.
	err = mc.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{
		Symbol:  symbol,
		OrderID: placed.OrderID,
	})
	if err != nil {
		log.Fatalf("CancelOrder: %v", err)
	}
	fmt.Printf("cancelled: orderID=%s\n", placed.OrderID)
}

// resolveCreds reads the section-specific BITGET_MARGIN_* credentials,
// falling back to the generic BITGET_* triple when they are unset.
func resolveCreds() (apiKey, secretKey, passphrase string) {
	apiKey = firstNonEmpty(os.Getenv("BITGET_MARGIN_API_KEY"), os.Getenv("BITGET_API_KEY"))
	secretKey = firstNonEmpty(os.Getenv("BITGET_MARGIN_SECRET_KEY"), os.Getenv("BITGET_SECRET_KEY"))
	passphrase = firstNonEmpty(os.Getenv("BITGET_MARGIN_PASSPHRASE"), os.Getenv("BITGET_PASSPHRASE"))
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
