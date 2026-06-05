/*
FILE: earn/loan_contract_test.go

DESCRIPTION:
Contract tests for the EARN Crypto Loan sub-client. Fixtures hand-derived
from the Bitget V2 earn/loan reference request/response types.

KEY INVARIANTS:

  - public/coinInfos + public/hour-interest are UNSIGNED (no ACCESS-SIGN);
    every other call is signed and sends no productType;
  - borrow requires exactly one of pledgeAmount / loanAmount;
  - the history endpoints require a [startTime,endTime] window and walk
    the pageNo/pageSize pages, stitching until a short page.
*/

package earn

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

func TestContract_Loan_GetCurrencies_Public(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"loanInfos":[{"coin":"USDT","hourRate7D":"0.0001","rate7D":"0.05","hourRate30D":"0.00008",
			"rate30D":"0.04","minUsdt":"10","maxUsdt":"100000","min":"10","max":"100000"}],
			"pledgeInfos":[{"coin":"BTC","initRate":"0.6","supRate":"0.75","forceRate":"0.85",
				"minUsdt":"10","maxUsdt":"500000"}]}}`

	var sawSign, sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/public/coinInfos": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var cur, err = NewClient(client).Loan().GetCurrencies(context.Background(), "")
	if err != nil {
		t.Fatalf("GetCurrencies: %v", err)
	}
	if sawSign {
		t.Error("loan/public/coinInfos must be unsigned (no ACCESS-SIGN)")
	}
	if sawProductType {
		t.Error("earn must not send productType")
	}
	if len(cur.LoanInfos) != 1 || cur.LoanInfos[0].Coin != "USDT" || !cur.LoanInfos[0].Rate7D.Equal(decimal.RequireFromString("0.05")) {
		t.Fatalf("loanInfos: got %v", cur.LoanInfos)
	}
	if len(cur.PledgeInfos) != 1 || !cur.PledgeInfos[0].ForceRate.Equal(decimal.RequireFromString("0.85")) {
		t.Errorf("pledgeInfos: got %v", cur.PledgeInfos)
	}
}

func TestContract_Loan_GetEstInterest_Public(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"hourInterest":"0.012","loanAmount":"100"}}`

	var sawSign bool
	var gotDaily string
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/public/hour-interest": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
		gotDaily = r.URL.Query().Get("daily")
	})

	var est, err = NewClient(client).Loan().GetEstInterest(context.Background(), "USDT", "BTC", "SEVEN", "0.01")
	if err != nil {
		t.Fatalf("GetEstInterest: %v", err)
	}
	if sawSign {
		t.Error("loan/public/hour-interest must be unsigned")
	}
	if gotDaily != "SEVEN" {
		t.Errorf("daily: got %q", gotDaily)
	}
	if !est.HourInterest.Equal(decimal.RequireFromString("0.012")) || !est.LoanAmount.Equal(decimal.RequireFromString("100")) {
		t.Errorf("est: got %+v", est)
	}

	if _, err = NewClient(client).Loan().GetEstInterest(context.Background(), "USDT", "BTC", "", "1"); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty daily: want InvalidRequest, got %v", err)
	}
}

func TestContract_Loan_Borrow(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"7001"}}`

	var body map[string]any
	var sawSign bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/borrow": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		if r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
		_ = json.Unmarshal(raw, &body)
	})

	var orderID, err = NewClient(client).Loan().Borrow(context.Background(), earntypes.LoanBorrowRequest{
		LoanCoin: "USDT", PledgeCoin: "BTC", Daily: "SEVEN", LoanAmount: "100",
	})
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	if !sawSign {
		t.Error("loan/borrow must be signed")
	}
	if body["loanCoin"] != "USDT" || body["daily"] != "SEVEN" || body["loanAmount"] != "100" {
		t.Errorf("body: got %v", body)
	}
	if _, present := body["pledgeAmount"]; present {
		t.Errorf("pledgeAmount must be omitted: %v", body)
	}
	if orderID != "7001" {
		t.Errorf("orderId: got %q", orderID)
	}

	// exactly-one-of guard: both empty / both set.
	if _, err = NewClient(client).Loan().Borrow(context.Background(), earntypes.LoanBorrowRequest{LoanCoin: "USDT", PledgeCoin: "BTC", Daily: "SEVEN"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("no amount: want InvalidRequest, got %v", err)
	}
	if _, err = NewClient(client).Loan().Borrow(context.Background(), earntypes.LoanBorrowRequest{LoanCoin: "USDT", PledgeCoin: "BTC", Daily: "SEVEN", LoanAmount: "1", PledgeAmount: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("both amounts: want InvalidRequest, got %v", err)
	}
}

func TestContract_Loan_GetOngoingOrders(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"orderId":"7001","loanCoin":"USDT","loanAmount":"100","interestAmount":"0.5",
			"hourInterestRate":"0.0001","pledgeCoin":"BTC","pledgeAmount":"0.005","pledgeRate":"0.6",
			"supRate":"0.75","forceRate":"0.85","borrowTime":"1700000000000","expireTime":"1700700000000"}]}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/ongoing-orders": fixture}, nil)

	var orders, err = NewClient(client).Loan().GetOngoingOrders(context.Background(), "", "", "")
	if err != nil {
		t.Fatalf("GetOngoingOrders: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != "7001" || orders[0].BorrowTimeMs != 1700000000000 {
		t.Fatalf("orders: got %v", orders)
	}
	if !orders[0].PledgeRate.Equal(decimal.RequireFromString("0.6")) || !orders[0].ForceRate.Equal(decimal.RequireFromString("0.85")) {
		t.Errorf("rates: got %+v", orders[0])
	}
}

func TestContract_Loan_Repay(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"loanCoin":"USDT","pledgeCoin":"BTC","repayAmount":"50","payInterest":"0.5",
			"repayLoanAmount":"49.5","repayUnlockAmount":"0.002"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/repay": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var res, err = NewClient(client).Loan().Repay(context.Background(), earntypes.LoanRepayRequest{OrderID: "7001", RepayAll: "false", Amount: "50"})
	if err != nil {
		t.Fatalf("Repay: %v", err)
	}
	if body["orderId"] != "7001" || body["repayAll"] != "false" || body["amount"] != "50" {
		t.Errorf("body: got %v", body)
	}
	if !res.RepayAmount.Equal(decimal.RequireFromString("50")) || !res.RepayUnlockAmount.Equal(decimal.RequireFromString("0.002")) {
		t.Errorf("result: got %+v", res)
	}

	if _, err = NewClient(client).Loan().Repay(context.Background(), earntypes.LoanRepayRequest{OrderID: "7001"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty repayAll: want InvalidRequest, got %v", err)
	}
}

func TestContract_Loan_RevisePledge(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"loanCoin":"USDT","pledgeCoin":"BTC","afterPledgeRate":"0.55"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/revise-pledge": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var res, err = NewClient(client).Loan().RevisePledge(context.Background(), earntypes.LoanRevisePledgeRequest{
		OrderID: "7001", Amount: "0.001", PledgeCoin: "BTC", ReviseType: "in",
	})
	if err != nil {
		t.Fatalf("RevisePledge: %v", err)
	}
	if body["reviseType"] != "in" || body["pledgeCoin"] != "BTC" {
		t.Errorf("body: got %v", body)
	}
	if !res.AfterPledgeRate.Equal(decimal.RequireFromString("0.55")) {
		t.Errorf("afterPledgeRate: got %s", res.AfterPledgeRate)
	}

	if _, err = NewClient(client).Loan().RevisePledge(context.Background(), earntypes.LoanRevisePledgeRequest{OrderID: "7001", Amount: "1", PledgeCoin: "BTC"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty reviseType: want InvalidRequest, got %v", err)
	}
}

func TestContract_Loan_GetDebts(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"pledgeInfos":[{"coin":"BTC","amount":"0.01","amountUsdt":"600"}],
			"loanInfos":[{"coin":"USDT","amount":"100","amountUsdt":"100"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/debts": fixture}, nil)

	var debts, err = NewClient(client).Loan().GetDebts(context.Background())
	if err != nil {
		t.Fatalf("GetDebts: %v", err)
	}
	if len(debts.PledgeInfos) != 1 || debts.PledgeInfos[0].Coin != "BTC" || !debts.PledgeInfos[0].AmountUsdt.Equal(decimal.RequireFromString("600")) {
		t.Fatalf("pledge: got %v", debts.PledgeInfos)
	}
	if len(debts.LoanInfos) != 1 || debts.LoanInfos[0].Coin != "USDT" {
		t.Errorf("loan: got %v", debts.LoanInfos)
	}
}

func TestContract_Loan_GetRepayHistory_Guard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if _, err := NewClient(client).Loan().GetRepayHistory(context.Background(), LoanHistoryQuery{}); !bitget.IsInvalidRequest(err) {
		t.Errorf("no window: want InvalidRequest, got %v", err)
	}
}

func TestContract_Loan_GetLoanHistory_Paged(t *testing.T) {
	t.Parallel()
	// Page 1 returns a full page (loanPageSize rows); page 2 returns one
	// row → loop stops. Verify pageNo increments and rows stitch.
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/earn/loan/borrow-history" {
			return `{"code":"40404","msg":"no fixture","data":null,"requestTime":0}`
		}
		var pageNo, _ = strconv.Atoi(r.URL.Query().Get("pageNo"))
		var rows []string
		switch pageNo {
		case 1:
			var i int
			for i = 0; i < loanPageSize; i++ {
				rows = append(rows, `{"orderId":"`+strconv.Itoa(i)+`","loanCoin":"USDT","pledgeCoin":"BTC",
					"initPledgeAmount":"0.01","initLoanAmount":"100","hourRate":"0.0001","daily":"SEVEN",
					"borrowTime":"1700000000000","status":"repay"}`)
			}
		case 2:
			rows = append(rows, `{"orderId":"last","loanCoin":"USDT","pledgeCoin":"BTC",
				"initPledgeAmount":"0.02","initLoanAmount":"200","hourRate":"0.0001","daily":"THIRTY",
				"borrowTime":"1700100000000","status":"force"}`)
		}
		var data string
		var i int
		for i = 0; i < len(rows); i++ {
			if i > 0 {
				data += ","
			}
			data += rows[i]
		}
		return `{"code":"00000","msg":"success","requestTime":1,"data":[` + data + `]}`
	})

	var hist, err = NewClient(client).Loan().GetLoanHistory(context.Background(), LoanHistoryQuery{
		StartTimeMs: 1700000000000, EndTimeMs: 1700700000000,
	})
	if err != nil {
		t.Fatalf("GetLoanHistory: %v", err)
	}
	if len(hist) != loanPageSize+1 {
		t.Fatalf("stitched rows: got %d, want %d", len(hist), loanPageSize+1)
	}
	if hist[loanPageSize].OrderID != "last" || hist[loanPageSize].Daily != "THIRTY" {
		t.Errorf("last row: got %+v", hist[loanPageSize])
	}
}

func TestContract_Loan_GetLiquidationRecords(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"orderId":"7001","loanCoin":"USDT","pledgeCoin":"BTC","reduceTime":"1700500000000",
			"pledgeRate":"0.86","pledgePrice":"60000","status":"done","pledgeAmount":"0.002","reduceFee":"0.1",
			"residueAmount":"0.001","runlockAmount":"0.001","repayLoanAmount":"50"}]}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/loan/reduces": fixture}, nil)

	var recs, err = NewClient(client).Loan().GetLiquidationRecords(context.Background(), LoanHistoryQuery{
		StartTimeMs: 1700000000000, EndTimeMs: 1700700000000,
	})
	if err != nil {
		t.Fatalf("GetLiquidationRecords: %v", err)
	}
	if len(recs) != 1 || recs[0].OrderID != "7001" || recs[0].ReduceTimeMs != 1700500000000 {
		t.Fatalf("records: got %v", recs)
	}
	if !recs[0].PledgePrice.Equal(decimal.RequireFromString("60000")) || !recs[0].RepayLoanAmount.Equal(decimal.RequireFromString("50")) {
		t.Errorf("row: got %+v", recs[0])
	}
}
