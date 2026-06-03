/*
FILE: examples/spot-smoke/main.go

DESCRIPTION:
Production-readiness smoke harness for the SPOT profile. Runs the full
go-live checklist against the LIVE Bitget API in one shot and prints a
PASS / FAIL / SKIP summary, exiting non-zero if any required check fails.
Intended to be run by an operator with real credentials before enabling
the spot connector in production — the SDK itself can never run live
trades on the operator's behalf, so this harness is the hand-off point.

CHECKS (in order):

	[public REST]   GetSymbolInfo / GetMarketTicker / GetOrderBook
	[public WS]     WatchOrderbook — at least one book frame within -ws-wait
	[signed REST]   GetAccountInfo (creds + "spot" authority) / GetAccount
	[private WS]    WatchOrders + WatchAccount + WatchFills subscribe (login)
	[trading]       place post-only BUY @ 0.95*ask → GetOrderDetail → Cancel

SAFETY:
The trading check places a post-only LIMIT BUY priced 5% below the best
ask (cannot cross) and cancels it immediately. It still reserves quote
balance while it rests, so a funded account is required. Pass -read-only
to skip the trading check (and run signed checks only if creds are set).

USAGE:

	export BITGET_SPOT_API_KEY=...
	export BITGET_SPOT_SECRET_KEY=...
	export BITGET_SPOT_PASSPHRASE=...
	go run ./examples/spot-smoke                          # full checklist, BTCUSDT
	go run ./examples/spot-smoke -symbol ETHUSDT -qty 0.01
	go run ./examples/spot-smoke -read-only               # no order placed

When credentials are unset the signed/private/trading checks are marked
SKIP and only the public surface is verified (a useful connectivity
probe in CI).
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/spot"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// checkStatus enumerates the outcome of a single smoke check.
type checkStatus string

const (
	statusPass checkStatus = "PASS"
	statusFail checkStatus = "FAIL"
	statusSkip checkStatus = "SKIP"
)

// result captures the outcome of one named check for the final summary.
type result struct {
	name   string
	status checkStatus
	detail string
}

// recorder accumulates check results and tracks whether any required
// check failed.
type recorder struct {
	results   []result
	anyFailed bool
}

// pass records a successful check.
func (r *recorder) pass(name, detail string) {
	r.results = append(r.results, result{name: name, status: statusPass, detail: detail})
	fmt.Printf("  [PASS] %s — %s\n", name, detail)
}

// fail records a failed check and flips the overall exit state.
func (r *recorder) fail(name, detail string) {
	r.results = append(r.results, result{name: name, status: statusFail, detail: detail})
	r.anyFailed = true
	fmt.Printf("  [FAIL] %s — %s\n", name, detail)
}

// skip records a skipped check (e.g. no credentials supplied).
func (r *recorder) skip(name, detail string) {
	r.results = append(r.results, result{name: name, status: statusSkip, detail: detail})
	fmt.Printf("  [SKIP] %s — %s\n", name, detail)
}

func main() {
	var symbol string
	var qty string
	var readOnly bool
	var wsWait time.Duration
	flag.StringVar(&symbol, "symbol", "BTCUSDT", "SPOT symbol to probe (e.g. BTCUSDT)")
	flag.StringVar(&qty, "qty", "0.0001", "trading-check order quantity in BASE coin")
	flag.BoolVar(&readOnly, "read-only", false, "skip the order placement / cancel check")
	flag.DurationVar(&wsWait, "ws-wait", 15*time.Second, "how long to wait for the first WS book frame")
	flag.Parse()

	var apiKey, secretKey, passphrase string = resolveCreds()
	var haveCreds bool = apiKey != "" && secretKey != "" && passphrase != ""

	var cfg bitget.Config = bitget.DefaultConfig()
	if haveCreds {
		cfg.APIKey = apiKey
		cfg.SecretKey = secretKey
		cfg.Passphrase = passphrase
	}

	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		fmt.Printf("FATAL: bitget.NewClient: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = c.Close() }()

	var sc *spot.Client = c.Spot().(*spot.Client)
	var rec recorder = recorder{}

	fmt.Printf("=== Bitget SPOT smoke harness (symbol=%s, creds=%v, read-only=%v) ===\n",
		symbol, haveCreds, readOnly)

	fmt.Println("\n-- public REST --")
	runPublicREST(sc, symbol, &rec)

	fmt.Println("\n-- public WebSocket --")
	runPublicWS(sc, symbol, wsWait, &rec)

	fmt.Println("\n-- signed REST --")
	if haveCreds {
		runSignedREST(sc, &rec)
	} else {
		rec.skip("signed REST", "no BITGET_SPOT_* credentials in env")
	}

	fmt.Println("\n-- private WebSocket --")
	if haveCreds {
		runPrivateWS(sc, symbol, &rec)
	} else {
		rec.skip("private WS", "no BITGET_SPOT_* credentials in env")
	}

	fmt.Println("\n-- trading lifecycle --")
	switch {
	case readOnly:
		rec.skip("trading lifecycle", "-read-only set")
	case !haveCreds:
		rec.skip("trading lifecycle", "no BITGET_SPOT_* credentials in env")
	default:
		runTradingLifecycle(sc, symbol, qty, &rec)
	}

	printSummary(&rec)
	if rec.anyFailed {
		os.Exit(1)
	}
}

// runPublicREST exercises the three unauthenticated market-data calls.
func runPublicREST(sc *spot.Client, symbol string, rec *recorder) {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var info spottypes.SymbolInfo
	var err error
	info, err = sc.MarketData().GetSymbolInfo(ctx, symbol)
	if err != nil {
		rec.fail("GetSymbolInfo", err.Error())
	} else {
		rec.pass("GetSymbolInfo", fmt.Sprintf("base=%s quote=%s tick=%s step=%s status=%s",
			info.BaseCoin, info.QuoteCoin, info.PriceTick, info.SizeStep, info.Status))
	}

	var ticker spottypes.MarketTicker
	ticker, err = sc.MarketData().GetMarketTicker(ctx, symbol)
	if err != nil {
		rec.fail("GetMarketTicker", err.Error())
	} else if ticker.AskPrice.IsZero() && ticker.BidPrice.IsZero() {
		rec.fail("GetMarketTicker", "ticker has no bid/ask (delisted symbol?)")
	} else {
		rec.pass("GetMarketTicker", fmt.Sprintf("last=%s bid=%s ask=%s",
			ticker.LastPrice, ticker.BidPrice, ticker.AskPrice))
	}

	var book roottypes.OrderBookSnapshot
	book, err = sc.MarketData().GetOrderBook(ctx, symbol, 50)
	if err != nil {
		rec.fail("GetOrderBook", err.Error())
	} else if len(book.Bids) == 0 || len(book.Asks) == 0 {
		rec.fail("GetOrderBook", "empty book")
	} else {
		rec.pass("GetOrderBook", fmt.Sprintf("depth=%d/%d best_bid=%s best_ask=%s",
			len(book.Bids), len(book.Asks), book.Bids[0].Price, book.Asks[0].Price))
	}
}

// runPublicWS subscribes to the depth stream and waits for the first
// frame, validating the public WS transport + orderbook engine.
func runPublicWS(sc *spot.Client, symbol string, wsWait time.Duration, rec *recorder) {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var frames int64
	var err error
	err = sc.Stream().WatchOrderbook(ctx, symbol,
		func(_ roottypes.OrderBookSnapshot) {
			atomic.AddInt64(&frames, 1)
		},
		func(streamErr error) {
			fmt.Printf("    (ws error: %v)\n", streamErr)
		},
	)
	if err != nil {
		rec.fail("WatchOrderbook subscribe", err.Error())
		return
	}
	defer func() { _ = sc.Stream().Close() }()

	var deadline time.Time = time.Now().Add(wsWait)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&frames) > 0 {
			rec.pass("WatchOrderbook", fmt.Sprintf("received %d book frame(s)", atomic.LoadInt64(&frames)))
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	rec.fail("WatchOrderbook", fmt.Sprintf("no book frame within %s", wsWait))
}

// runSignedREST validates credentials + spot authority via a signed GET.
func runSignedREST(sc *spot.Client, rec *recorder) {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var acc spottypes.AccountInfo
	var err error
	acc, err = sc.Account().GetAccountInfo(ctx)
	if err != nil {
		rec.fail("GetAccountInfo", classifyErr(err))
	} else {
		rec.pass("GetAccountInfo", fmt.Sprintf("userID=%s authorities=%v", acc.UserID, acc.Authorities))
	}

	var bal roottypes.Balance
	bal, err = sc.Account().GetAccount(ctx)
	if err != nil {
		rec.fail("GetAccount", classifyErr(err))
	} else {
		rec.pass("GetAccount", fmt.Sprintf("funded coins=%d", len(bal.Coins)))
	}
}

// runPrivateWS subscribes to all three private channels — a successful
// subscribe implies the signed WS login handshake passed.
func runPrivateWS(sc *spot.Client, symbol string, rec *recorder) {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	defer func() { _ = sc.Stream().Close() }()

	var errHandler = func(streamErr error) { fmt.Printf("    (private ws error: %v)\n", streamErr) }

	var err error
	err = sc.Stream().WatchOrders(ctx, symbol, func(_ spottypes.OrderInfo) {}, errHandler)
	if err != nil {
		rec.fail("WatchOrders subscribe (login)", classifyErr(err))
		return
	}
	err = sc.Stream().WatchAccount(ctx, "default", func(_ spottypes.AccountUpdate) {}, errHandler)
	if err != nil {
		rec.fail("WatchAccount subscribe", classifyErr(err))
		return
	}
	err = sc.Stream().WatchFills(ctx, symbol, func(_ spottypes.FillUpdate) {}, errHandler)
	if err != nil {
		rec.fail("WatchFills subscribe", classifyErr(err))
		return
	}
	rec.pass("private WS", "orders/account/fills subscribed (login OK)")
}

// runTradingLifecycle places a safe post-only order, reads it back, and
// cancels it — the end-to-end signed trading round-trip.
func runTradingLifecycle(sc *spot.Client, symbol, qty string, rec *recorder) {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var ticker spottypes.MarketTicker
	var err error
	ticker, err = sc.MarketData().GetMarketTicker(ctx, symbol)
	if err != nil || ticker.AskPrice.IsZero() {
		rec.fail("trading: ticker", "cannot resolve a safe post-only price")
		return
	}
	var price decimal.Decimal = ticker.AskPrice.Mul(decimal.NewFromFloat(0.95))
	var quantity decimal.Decimal = decimal.RequireFromString(qty)
	var clientOID string = fmt.Sprintf("smoke-%d", time.Now().UnixNano())

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
		rec.fail("CreateOrder", classifyErr(err))
		return
	}
	rec.pass("CreateOrder", fmt.Sprintf("orderID=%s status=%s", placed.OrderID, placed.Status))

	var detail spottypes.OrderInfo
	detail, err = sc.Account().GetOrderDetail(ctx, symbol, placed.OrderID, "")
	if err != nil {
		rec.fail("GetOrderDetail", classifyErr(err))
	} else {
		rec.pass("GetOrderDetail", fmt.Sprintf("status=%s filled=%s", detail.Status, detail.FilledQuantity))
	}

	err = sc.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{Symbol: symbol, OrderID: placed.OrderID})
	if err != nil {
		rec.fail("CancelOrder", classifyErr(err)+" — WARNING: order may still be resting, cancel it manually")
		return
	}
	rec.pass("CancelOrder", fmt.Sprintf("orderID=%s cancelled", placed.OrderID))
}

// printSummary renders the final PASS/FAIL/SKIP table.
func printSummary(rec *recorder) {
	fmt.Println("\n=== SUMMARY ===")
	var passN, failN, skipN int
	var i int
	for i = 0; i < len(rec.results); i++ {
		var r result = rec.results[i]
		fmt.Printf("  %-4s %s\n", r.status, r.name)
		switch r.status {
		case statusPass:
			passN++
		case statusFail:
			failN++
		case statusSkip:
			skipN++
		}
	}
	fmt.Printf("\n%d passed, %d failed, %d skipped\n", passN, failN, skipN)
	if failN == 0 {
		fmt.Println("RESULT: spot smoke checks OK")
	} else {
		fmt.Println("RESULT: spot smoke checks FAILED — see [FAIL] rows above")
	}
}

// classifyErr returns a human-readable prefix for a *bitget.Error so the
// operator can tell auth problems from rate-limit / request issues at a
// glance.
func classifyErr(err error) string {
	switch {
	case bitget.IsAuth(err):
		return "AUTH: " + err.Error()
	case bitget.IsRateLimit(err):
		return "RATE_LIMIT: " + err.Error()
	case bitget.IsInvalidRequest(err):
		return "INVALID_REQUEST: " + err.Error()
	default:
		return err.Error()
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
