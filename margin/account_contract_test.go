/*
FILE: margin/account_contract_test.go

DESCRIPTION:
Contract tests for the MARGIN profile account / assets / borrow-repay /
records sub-client and the public currencies endpoint. Fixtures are
hand-derived from the Bitget V2 margin docs; no network calls (the
shared mockBitget harness from trading_contract_test.go backs them).

KEY INVARIANTS:

  - crossed / isolated mode drives the URL path segment;
  - isolated borrow / repay REQUIRE symbol; crossed omits it;
  - assets / max-borrowable parse both the crossed (single-coin) and
    isolated (base/quote, per-symbol) response shapes;
  - margin order queries REQUIRE symbol (unlike spot);
  - paged order / fill / record queries parse the row shape and map
    loanType / fee fields correctly.
*/

package margin

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ---------------------------------------------------------------------
// GetAccountAssets.
// ---------------------------------------------------------------------

func TestContract_Margin_GetAccountAssets_Isolated(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{
			"symbol":"BTCUSDT","coin":"USDT","totalAmount":"1000","available":"800",
			"frozen":"50","borrow":"150","interest":"0.5","net":"849.5","coupon":"0","uTime":"1700000000000"
		}]
	}`

	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/isolated/account/assets": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenPath = r.URL.Path
	})

	var assets, err = marginWithMode(client, roottypes.MarginModeIsolated).Account().GetAccountAssets(context.Background(), "", "BTCUSDT")
	if err != nil {
		t.Fatalf("GetAccountAssets: %v", err)
	}
	if seenPath != "/api/v2/margin/isolated/account/assets" {
		t.Errorf("path: got %q", seenPath)
	}
	if len(assets) != 1 {
		t.Fatalf("assets: want 1, got %d", len(assets))
	}
	if assets[0].Symbol != "BTCUSDT" || assets[0].Coin != "USDT" {
		t.Errorf("symbol/coin: got %q/%q", assets[0].Symbol, assets[0].Coin)
	}
	if !assets[0].Borrow.Equal(decimal.RequireFromString("150")) {
		t.Errorf("Borrow: got %s", assets[0].Borrow)
	}
	if !assets[0].Net.Equal(decimal.RequireFromString("849.5")) {
		t.Errorf("Net: got %s", assets[0].Net)
	}
	if assets[0].UpdatedAtMs != 1700000000000 {
		t.Errorf("UpdatedAtMs: got %d", assets[0].UpdatedAtMs)
	}
}

// ---------------------------------------------------------------------
// Borrow / Repay.
// ---------------------------------------------------------------------

func TestContract_Margin_Borrow_CrossedOmitsSymbol(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"loanId":"L1","coin":"USDT","borrowAmount":"100"}}`

	var body map[string]any
	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/account/borrow": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		seenPath = r.URL.Path
		body = decodeBody(t, raw)
	})

	var res, err = marginOf(client).Account().Borrow(context.Background(), "USDT", "100", "")
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	if seenPath != "/api/v2/margin/crossed/account/borrow" {
		t.Errorf("path: got %q", seenPath)
	}
	assertField(t, body, "coin", "USDT")
	assertField(t, body, "borrowAmount", "100")
	if _, present := body["symbol"]; present {
		t.Errorf("symbol: must be omitted on crossed, got %v", body["symbol"])
	}
	if res.LoanID != "L1" || !res.BorrowAmount.Equal(decimal.RequireFromString("100")) {
		t.Errorf("result: %+v", res)
	}
}

func TestContract_Margin_Borrow_IsolatedRequiresSymbol(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var _, err = marginWithMode(client, roottypes.MarginModeIsolated).Account().Borrow(context.Background(), "USDT", "100", "")
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("isolated borrow without symbol: want ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_Margin_Borrow_IsolatedSendsSymbol(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"loanId":"L2","coin":"USDT","symbol":"BTCUSDT","borrowAmount":"50"}}`

	var body map[string]any
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/isolated/account/borrow": fixture}, func(t *testing.T, _ *http.Request, raw []byte) {
		body = decodeBody(t, raw)
	})

	var _, err = marginWithMode(client, roottypes.MarginModeIsolated).Account().Borrow(context.Background(), "USDT", "50", "BTCUSDT")
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	assertField(t, body, "symbol", "BTCUSDT")
}

func TestContract_Margin_Repay_RemainDebt(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"repayId":"R1","coin":"USDT","symbol":"BTCUSDT","repayAmount":"50","remainDebtAmount":"0"}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/account/repay": fixture}, nil)

	var res, err = marginOf(client).Account().Repay(context.Background(), "USDT", "50", "", "")
	if err != nil {
		t.Fatalf("Repay: %v", err)
	}
	if res.RepayID != "R1" {
		t.Errorf("RepayID: got %q", res.RepayID)
	}
	if !res.RepayAmount.Equal(decimal.RequireFromString("50")) {
		t.Errorf("RepayAmount: got %s", res.RepayAmount)
	}
	if !res.RemainDebtAmount.Equal(decimal.Zero) {
		t.Errorf("RemainDebtAmount: got %s", res.RemainDebtAmount)
	}
}

// ---------------------------------------------------------------------
// GetMaxBorrowable.
// ---------------------------------------------------------------------

func TestContract_Margin_GetMaxBorrowable_Crossed(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"coin":"USDT","maxBorrowableAmount":"5000"}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/account/max-borrowable-amount": fixture}, nil)

	var mb, err = marginOf(client).Account().GetMaxBorrowable(context.Background(), "USDT", "")
	if err != nil {
		t.Fatalf("GetMaxBorrowable: %v", err)
	}
	if mb.Coin != "USDT" || !mb.MaxBorrowableAmount.Equal(decimal.RequireFromString("5000")) {
		t.Errorf("result: %+v", mb)
	}
}

func TestContract_Margin_GetMaxBorrowable_Isolated(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"symbol":"BTCUSDT","baseCoin":"BTC","baseCoinMaxBorrowAmount":"1.5","quoteCoin":"USDT","quoteCoinMaxBorrowAmount":"50000"}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/isolated/account/max-borrowable-amount": fixture}, nil)

	var mb, err = marginWithMode(client, roottypes.MarginModeIsolated).Account().GetMaxBorrowable(context.Background(), "", "BTCUSDT")
	if err != nil {
		t.Fatalf("GetMaxBorrowable: %v", err)
	}
	if mb.Symbol != "BTCUSDT" || mb.BaseCoin != "BTC" || mb.QuoteCoin != "USDT" {
		t.Errorf("symbols: %+v", mb)
	}
	if !mb.BaseCoinMaxBorrowAmount.Equal(decimal.RequireFromString("1.5")) {
		t.Errorf("BaseCoinMaxBorrowAmount: %s", mb.BaseCoinMaxBorrowAmount)
	}
	if !mb.QuoteCoinMaxBorrowAmount.Equal(decimal.RequireFromString("50000")) {
		t.Errorf("QuoteCoinMaxBorrowAmount: %s", mb.QuoteCoinMaxBorrowAmount)
	}
}

// ---------------------------------------------------------------------
// Order / fill queries.
// ---------------------------------------------------------------------

func TestContract_Margin_GetOpenOrders_RequiresSymbol(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var _, err = marginOf(client).Account().GetOpenOrders(context.Background(), "")
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetOpenOrders(\"\"): want ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_Margin_GetOpenOrders_ParsesRow(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{
			"orderId":"o-1","symbol":"BTCUSDT","orderType":"limit","clientOid":"c-1","loanType":"autoLoan",
			"price":"43000","side":"buy","status":"live","baseSize":"0.01","quoteSize":"",
			"priceAvg":"0","size":"0.01","amount":"430","force":"gtc","cTime":"1700000000000","uTime":"1700000000001"
		}]
	}`

	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/open-orders": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenPath = r.URL.Path
	})

	var orders, err = marginOf(client).Account().GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if seenPath != "/api/v2/margin/crossed/open-orders" {
		t.Errorf("path: got %q", seenPath)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if orders[0].OrderID != "o-1" || orders[0].ClientOrderID != "c-1" {
		t.Errorf("ids: %+v", orders[0])
	}
	if orders[0].LoanType != margintypes.LoanTypeAutoLoan {
		t.Errorf("LoanType: want autoLoan, got %q", orders[0].LoanType)
	}
	if !orders[0].Quantity.Equal(decimal.RequireFromString("0.01")) {
		t.Errorf("Quantity: got %s", orders[0].Quantity)
	}
	if orders[0].Status != roottypes.OrderStatusLive {
		t.Errorf("Status: got %q", orders[0].Status)
	}
}

func TestContract_Margin_GetFills_ParsesFee(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{
			"orderId":"o-1","tradeId":"t-1","orderType":"limit","side":"buy","priceAvg":"43000",
			"size":"0.01","amount":"430","tradeScope":"maker",
			"feeDetail":{"deduction":"no","feeCoin":"USDT","totalDeductionFee":"0","totalFee":"-0.43"},
			"cTime":"1700000000000","uTime":"1700000000001"
		}]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/fills": fixture}, nil)

	var fills, err = marginOf(client).Account().GetFills(context.Background(), "BTCUSDT", "", 0, 0)
	if err != nil {
		t.Fatalf("GetFills: %v", err)
	}
	if len(fills) != 1 {
		t.Fatalf("fills: want 1, got %d", len(fills))
	}
	if fills[0].TradeID != "t-1" || fills[0].FeeCoin != "USDT" {
		t.Errorf("fill: %+v", fills[0])
	}
	if !fills[0].TotalFee.Equal(decimal.RequireFromString("-0.43")) {
		t.Errorf("TotalFee: got %s", fills[0].TotalFee)
	}
	if !fills[0].FillPrice.Equal(decimal.RequireFromString("43000")) {
		t.Errorf("FillPrice: got %s", fills[0].FillPrice)
	}
}

// ---------------------------------------------------------------------
// History records.
// ---------------------------------------------------------------------

func TestContract_Margin_GetBorrowHistory(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{"loanId":"L1","coin":"USDT","borrowAmount":"100","borrowType":"manual","cTime":"1700000000000","uTime":"1700000000001"}]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/borrow-history": fixture}, nil)

	var recs, err = marginOf(client).Account().GetBorrowHistory(context.Background(), "USDT", 0, 0)
	if err != nil {
		t.Fatalf("GetBorrowHistory: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("records: want 1, got %d", len(recs))
	}
	if recs[0].LoanID != "L1" || recs[0].BorrowType != "manual" {
		t.Errorf("record: %+v", recs[0])
	}
	if !recs[0].BorrowAmount.Equal(decimal.RequireFromString("100")) {
		t.Errorf("BorrowAmount: got %s", recs[0].BorrowAmount)
	}
}

// ---------------------------------------------------------------------
// Public — Currencies.
// ---------------------------------------------------------------------

func TestContract_Margin_Currencies(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{
			"symbol":"BTCUSDT","baseCoin":"BTC","quoteCoin":"USDT","maxCrossedLeverage":"3","maxIsolatedLeverage":"10",
			"minTradeAmount":"0.0001","maxTradeAmount":"100","takerFeeRate":"0.001","makerFeeRate":"0.001",
			"pricePrecision":"2","quantityPrecision":"6","minTradeUSDT":"5","isBorrowable":true,"userMinBorrow":"0",
			"status":"online","isIsolatedBaseBorrowable":true,"isIsolatedQuoteBorrowable":true,"isCrossBorrowable":true
		}]
	}`

	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/currencies": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenPath = r.URL.Path
	})

	var curs, err = marginOf(client).Public().Currencies(context.Background(), "BTCUSDT", "")
	if err != nil {
		t.Fatalf("Currencies: %v", err)
	}
	if seenPath != "/api/v2/margin/currencies" {
		t.Errorf("path: got %q (must be mode-agnostic, no crossed/isolated segment)", seenPath)
	}
	if len(curs) != 1 {
		t.Fatalf("currencies: want 1, got %d", len(curs))
	}
	if curs[0].Symbol != "BTCUSDT" || curs[0].BaseCoin != "BTC" {
		t.Errorf("currency: %+v", curs[0])
	}
	if curs[0].PricePrecision != 2 || curs[0].QuantityPrecision != 6 {
		t.Errorf("precision: %d/%d", curs[0].PricePrecision, curs[0].QuantityPrecision)
	}
	if !curs[0].IsCrossBorrowable || !curs[0].IsIsolatedBaseBorrowable {
		t.Errorf("borrowable flags: %+v", curs[0])
	}
	if !curs[0].MakerFeeRate.Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("MakerFeeRate: %s", curs[0].MakerFeeRate)
	}
}
