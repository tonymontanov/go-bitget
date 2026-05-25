/*
FILE: examples/place-order/main.go

DESCRIPTION:
End-to-end demo of the signed REST trading surface of the MIX profile.
The example places a deeply post-only LIMIT order (priced 5% below the
best ask), inspects it via GetOrderDetail, then cancels it. Designed
to be safe on a live Bitget account: the order is post-only and far
out-of-market, so it should never fill.

USAGE (env-vars are mandatory):

	export BITGET_API_KEY=...
	export BITGET_SECRET_KEY=...
	export BITGET_PASSPHRASE=...
	go run ./examples/place-order                 # defaults: BTCUSDT 0.001 @ 0.95*ask
	go run ./examples/place-order -symbol ETHUSDT -qty 0.01

The example prints the order ID, lifecycle state, and final cancel
status. ANY non-success exit code indicates an exchange-level failure
that the caller should investigate (insufficient margin, leverage
mismatch, etc.).
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
	"github.com/tonymontanov/go-bitget/v2/mix"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func main() {
	var symbol string
	var qty string
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "MIX symbol (e.g. BTCUSDT, ETHUSDT)")
	flag.StringVar(&qty, "qty", "0.001", "order quantity in BASE coin (must be ≥ MinTradeNum)")
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
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1) Resolve a deep post-only price = best_ask * 0.95. Far enough
	//    that the order can NOT fill before we cancel it. Reading the
	//    ticker is cheaper than the full book and gives us the best
	//    ask + sizeStep needed to round the qty.
	var ticker mixtypes.MarketTicker
	ticker, err = mc.MarketData().GetMarketTicker(ctx, symbol)
	if err != nil {
		log.Fatalf("GetMarketTicker: %v", err)
	}
	if ticker.AskPrice.IsZero() {
		log.Fatalf("ticker for %s has no ask price (pre-launch contract?)", symbol)
	}
	var price decimal.Decimal = ticker.AskPrice.Mul(decimal.NewFromFloat(0.95))
	var quantity decimal.Decimal = decimal.RequireFromString(qty)

	// 2) Place the post-only order. The clientOID is owned by the
	//    desk — the SDK does NOT auto-generate one (mirrors OKX/Bybit
	//    SDKs). Using the unix-nano timestamp keeps the example
	//    self-contained.
	var clientOID string = fmt.Sprintf("example-%d", time.Now().UnixNano())
	var placed mixtypes.OrderInfo
	placed, err = mc.Trading().CreateOrder(ctx, mixtypes.CreateOrderRequest{
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
			log.Fatalf("CreateOrder: invalid request (qty step? leverage?): %v", err)
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
	var detail mixtypes.OrderInfo
	detail, err = mc.Account().GetOrderDetail(ctx, symbol, placed.OrderID, "")
	if err != nil {
		log.Printf("GetOrderDetail: %v (order is still placed; will continue to cancel)", err)
	} else {
		fmt.Printf("detail: status=%s filledQty=%s avgFillPrice=%s\n",
			detail.Status, detail.FilledQuantity, detail.AvgFilledPrice)
	}

	// 4) Cancel the order. The desk MUST always cancel before exiting
	//    a placement script — orphaned post-only orders are still
	//    real exposure if the market falls.
	err = mc.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{
		Symbol:  symbol,
		OrderID: placed.OrderID,
	})
	if err != nil {
		log.Fatalf("CancelOrder: %v", err)
	}
	fmt.Printf("cancelled: orderID=%s\n", placed.OrderID)
}
