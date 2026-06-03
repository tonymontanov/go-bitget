/*
FILE: examples/spot-place-order/main.go

DESCRIPTION:
End-to-end demo of the signed REST trading surface of the SPOT profile.
Mirrors examples/place-order (MIX). The example places a deeply post-only
LIMIT BUY order (priced 5% below the best ask), inspects it via
GetOrderDetail, then cancels it. Designed to be safe on a live Bitget
account: the order is post-only and far out-of-market, so it should
never fill. It still reserves quote balance (USDT) while it rests, so a
funded account is required for the placement to succeed.

USAGE (env-vars are mandatory):

	export BITGET_SPOT_API_KEY=...
	export BITGET_SPOT_SECRET_KEY=...
	export BITGET_SPOT_PASSPHRASE=...
	go run ./examples/spot-place-order                 # defaults: BTCUSDT 0.0001 @ 0.95*ask
	go run ./examples/spot-place-order -symbol ETHUSDT -qty 0.01

The example falls back to the generic BITGET_API_KEY / BITGET_SECRET_KEY /
BITGET_PASSPHRASE triple when the BITGET_SPOT_* variables are unset, so a
single-key setup also works.

ANY non-success exit code indicates an exchange-level failure that the
caller should investigate (insufficient balance, symbol halted, etc.).
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
	"github.com/tonymontanov/go-bitget/v2/spot"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func main() {
	var symbol string
	var qty string
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "SPOT symbol (e.g. BTCUSDT, ETHUSDT)")
	flag.StringVar(&qty, "qty", "0.0001", "order quantity in BASE coin (must be ≥ MinTradeAmount)")
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
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1) Resolve a deep post-only price = best_ask * 0.95. Far enough
	//    that the order can NOT cross before we cancel it. We use a
	//    LIMIT BUY so the size stays denominated in BASE coin — the
	//    market-BUY quote-denomination quirk does not apply here.
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

	// 2) Place the post-only order. The clientOID is owned by the
	//    desk — the SDK does NOT auto-generate one (mirrors the mix /
	//    OKX / Bybit SDKs). Using the unix-nano timestamp keeps the
	//    example self-contained.
	var clientOID string = fmt.Sprintf("spot-example-%d", time.Now().UnixNano())
	var placed spottypes.OrderInfo
	placed, err = sc.Trading().CreateOrder(ctx, spottypes.CreateOrderRequest{
		Symbol:        symbol,
		Side:          roottypes.SideTypeBuy,
		OrderType:     roottypes.OrderTypeLimit,
		TimeInForce:   roottypes.TimeInForcePostOnly,
		Quantity:      quantity,
		Price:         price,
		ClientOrderID: clientOID,
	})
	if err != nil {
		switch {
		case bitget.IsAuth(err):
			log.Fatalf("CreateOrder: auth failed (check key/secret/passphrase + IP whitelist): %v", err)
		case bitget.IsRateLimit(err):
			log.Fatalf("CreateOrder: rate-limited — back off and retry: %v", err)
		case bitget.IsInvalidRequest(err):
			log.Fatalf("CreateOrder: invalid request (qty step? min notional?): %v", err)
		default:
			log.Fatalf("CreateOrder: %v", err)
		}
	}
	fmt.Printf("placed: orderID=%s clientOID=%s symbol=%s side=%s type=%s qty=%s price=%s status=%s\n",
		placed.OrderID, placed.ClientOrderID, placed.Symbol,
		placed.Side, placed.OrderType, placed.Quantity, placed.Price, placed.Status)

	// 3) Read it back through GetOrderDetail. orderID and clientOID
	//    are mutually exclusive; we pass orderID since the exchange
	//    has now assigned one.
	var detail spottypes.OrderInfo
	detail, err = sc.Account().GetOrderDetail(ctx, symbol, placed.OrderID, "")
	if err != nil {
		log.Printf("GetOrderDetail: %v (order is still placed; will continue to cancel)", err)
	} else {
		fmt.Printf("detail: status=%s filledQty=%s avgFillPrice=%s cumFee=%s\n",
			detail.Status, detail.FilledQuantity, detail.AvgFilledPrice, detail.CumFee)
	}

	// 4) Cancel the order. A placement script MUST always cancel
	//    before exiting — an orphaned post-only buy keeps quote
	//    balance frozen until it is cleared.
	err = sc.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{
		Symbol:  symbol,
		OrderID: placed.OrderID,
	})
	if err != nil {
		log.Fatalf("CancelOrder: %v", err)
	}
	fmt.Printf("cancelled: orderID=%s\n", placed.OrderID)
}

// resolveCreds reads the section-specific BITGET_SPOT_* credentials,
// falling back to the generic BITGET_* triple when they are unset. This
// lets the example run both in a dedicated-spot-key setup (recommended)
// and a single-key dev setup.
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
