/*
FILE: earn/loan.go

DESCRIPTION:
Crypto Loan sub-client — /api/v2/earn/loan/... The two public/* endpoints
(coin infos, hour-interest) are unsigned; the rest are signed. Borrow /
repay / revise-pledge move real collateral and debt. The four history
endpoints (repay-history, revise-history, borrow-history, reduces) are
page-number paginated and require a [startTime,endTime] window.

Request params + response shapes verified against the Bitget V2 earn/loan
reference request/response types.
*/

package earn

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

// loanPageSize is the per-page row count for the loan history endpoints.
const loanPageSize = 100

// LoanClient — earn crypto-loan sub-client. Built once per earn.Client
// and safe for concurrent use.
type LoanClient struct {
	c *Client
}

func newLoanClient(c *Client) *LoanClient {
	return &LoanClient{c: c}
}

// dec is a tiny helper that parses a batch of string fields into their
// decimal destinations, returning the first parse error tagged to scope.
func (l *LoanClient) dec(scope string, pairs []struct {
	dst *decimal.Decimal
	src string
}) error {
	var i int
	var err error
	for i = 0; i < len(pairs); i++ {
		if *pairs[i].dst, err = bgcommon.ParseDecimalOrZero(pairs[i].src); err != nil {
			return errParse(scope, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------
// GetCurrencies — loan/public/coinInfos (public).
// ---------------------------------------------------------------------

type loanLoanInfoRow struct {
	Coin        string `json:"coin"`
	HourRate7D  string `json:"hourRate7D"`
	Rate7D      string `json:"rate7D"`
	HourRate30D string `json:"hourRate30D"`
	Rate30D     string `json:"rate30D"`
	MinUsdt     string `json:"minUsdt"`
	MaxUsdt     string `json:"maxUsdt"`
	Min         string `json:"min"`
	Max         string `json:"max"`
}

type loanPledgeInfoRow struct {
	Coin      string `json:"coin"`
	InitRate  string `json:"initRate"`
	SupRate   string `json:"supRate"`
	ForceRate string `json:"forceRate"`
	MinUsdt   string `json:"minUsdt"`
	MaxUsdt   string `json:"maxUsdt"`
}

type loanCurrenciesEnvelope struct {
	LoanInfos   []loanLoanInfoRow   `json:"loanInfos"`
	PledgeInfos []loanPledgeInfoRow `json:"pledgeInfos"`
}

// GetCurrencies lists the borrowable / collateral coins and their rate
// and amount bounds. Public (unsigned). coin is an optional filter.
func (l *LoanClient) GetCurrencies(ctx context.Context, coin string) (earntypes.LoanCurrencies, error) {
	var out earntypes.LoanCurrencies
	var query url.Values
	if coin != "" {
		query = url.Values{}
		query.Set("coin", coin)
	}

	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/loan/public/coinInfos",
		Query:  query,
		Signed: false,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env loanCurrenciesEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("Loan.GetCurrencies", err)
	}
	var i int
	out.LoanInfos = make([]earntypes.LoanCurrencyLoanInfo, 0, len(env.LoanInfos))
	for i = 0; i < len(env.LoanInfos); i++ {
		var li earntypes.LoanCurrencyLoanInfo = earntypes.LoanCurrencyLoanInfo{Coin: env.LoanInfos[i].Coin}
		if err = l.dec("Loan.GetCurrencies", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&li.HourRate7D, env.LoanInfos[i].HourRate7D},
			{&li.Rate7D, env.LoanInfos[i].Rate7D},
			{&li.HourRate30D, env.LoanInfos[i].HourRate30D},
			{&li.Rate30D, env.LoanInfos[i].Rate30D},
			{&li.MinUsdt, env.LoanInfos[i].MinUsdt},
			{&li.MaxUsdt, env.LoanInfos[i].MaxUsdt},
			{&li.Min, env.LoanInfos[i].Min},
			{&li.Max, env.LoanInfos[i].Max},
		}); err != nil {
			return out, err
		}
		out.LoanInfos = append(out.LoanInfos, li)
	}
	out.PledgeInfos = make([]earntypes.LoanCurrencyPledgeInfo, 0, len(env.PledgeInfos))
	for i = 0; i < len(env.PledgeInfos); i++ {
		var pi earntypes.LoanCurrencyPledgeInfo = earntypes.LoanCurrencyPledgeInfo{Coin: env.PledgeInfos[i].Coin}
		if err = l.dec("Loan.GetCurrencies", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&pi.InitRate, env.PledgeInfos[i].InitRate},
			{&pi.SupRate, env.PledgeInfos[i].SupRate},
			{&pi.ForceRate, env.PledgeInfos[i].ForceRate},
			{&pi.MinUsdt, env.PledgeInfos[i].MinUsdt},
			{&pi.MaxUsdt, env.PledgeInfos[i].MaxUsdt},
		}); err != nil {
			return out, err
		}
		out.PledgeInfos = append(out.PledgeInfos, pi)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetEstInterest — loan/public/hour-interest (public).
// ---------------------------------------------------------------------

type loanEstInterestRow struct {
	HourInterest string `json:"hourInterest"`
	LoanAmount   string `json:"loanAmount"`
}

// GetEstInterest estimates the hourly interest + borrowable loan amount.
// loanCoin, pledgeCoin and daily ("SEVEN" | "THIRTY") are required;
// pledgeAmount is optional. Public (unsigned).
func (l *LoanClient) GetEstInterest(ctx context.Context, loanCoin, pledgeCoin, daily, pledgeAmount string) (earntypes.LoanEstInterest, error) {
	var out earntypes.LoanEstInterest
	switch {
	case loanCoin == "" || pledgeCoin == "":
		return out, errInvalid("Loan.GetEstInterest", "loanCoin and pledgeCoin are required")
	case daily == "":
		return out, errInvalid("Loan.GetEstInterest", "daily is required (SEVEN | THIRTY)")
	}

	var query url.Values = url.Values{}
	query.Set("loanCoin", loanCoin)
	query.Set("pledgeCoin", pledgeCoin)
	query.Set("daily", daily)
	if pledgeAmount != "" {
		query.Set("pledgeAmount", pledgeAmount)
	}

	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/loan/public/hour-interest",
		Query:  query,
		Signed: false,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row loanEstInterestRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Loan.GetEstInterest", err)
	}
	if err = l.dec("Loan.GetEstInterest", []struct {
		dst *decimal.Decimal
		src string
	}{
		{&out.HourInterest, row.HourInterest},
		{&out.LoanAmount, row.LoanAmount},
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Borrow — loan/borrow.
// ---------------------------------------------------------------------

type loanBorrowBody struct {
	LoanCoin     string `json:"loanCoin"`
	PledgeCoin   string `json:"pledgeCoin"`
	Daily        string `json:"daily"`
	PledgeAmount string `json:"pledgeAmount,omitempty"`
	LoanAmount   string `json:"loanAmount,omitempty"`
}

// Borrow opens a loan. LoanCoin, PledgeCoin and Daily ("SEVEN" |
// "THIRTY") are required; provide exactly one of PledgeAmount /
// LoanAmount. Returns the new order id. NOTE: moves real collateral.
func (l *LoanClient) Borrow(ctx context.Context, req earntypes.LoanBorrowRequest) (string, error) {
	switch {
	case req.LoanCoin == "" || req.PledgeCoin == "":
		return "", errInvalid("Loan.Borrow", "loanCoin and pledgeCoin are required")
	case req.Daily == "":
		return "", errInvalid("Loan.Borrow", "daily is required (SEVEN | THIRTY)")
	case (req.PledgeAmount == "") == (req.LoanAmount == ""):
		return "", errInvalid("Loan.Borrow", "exactly one of pledgeAmount / loanAmount is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/loan/borrow",
		Body: loanBorrowBody{
			LoanCoin:     req.LoanCoin,
			PledgeCoin:   req.PledgeCoin,
			Daily:        req.Daily,
			PledgeAmount: req.PledgeAmount,
			LoanAmount:   req.LoanAmount,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row orderIDRow
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("Loan.Borrow", err)
	}
	return row.OrderID, nil
}

// ---------------------------------------------------------------------
// GetOngoingOrders — loan/ongoing-orders.
// ---------------------------------------------------------------------

type loanOrderRow struct {
	OrderID          string `json:"orderId"`
	LoanCoin         string `json:"loanCoin"`
	LoanAmount       string `json:"loanAmount"`
	InterestAmount   string `json:"interestAmount"`
	HourInterestRate string `json:"hourInterestRate"`
	PledgeCoin       string `json:"pledgeCoin"`
	PledgeAmount     string `json:"pledgeAmount"`
	PledgeRate       string `json:"pledgeRate"`
	SupRate          string `json:"supRate"`
	ForceRate        string `json:"forceRate"`
	BorrowTime       string `json:"borrowTime"`
	ExpireTime       string `json:"expireTime"`
}

// GetOngoingOrders lists the open loan orders, optionally filtered by
// orderId / loanCoin / pledgeCoin (pass "" to skip a filter).
func (l *LoanClient) GetOngoingOrders(ctx context.Context, orderID, loanCoin, pledgeCoin string) ([]earntypes.LoanOrder, error) {
	var query url.Values = url.Values{}
	if orderID != "" {
		query.Set("orderId", orderID)
	}
	if loanCoin != "" {
		query.Set("loanCoin", loanCoin)
	}
	if pledgeCoin != "" {
		query.Set("pledgeCoin", pledgeCoin)
	}

	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/loan/ongoing-orders",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []loanOrderRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Loan.GetOngoingOrders", err)
	}
	var out []earntypes.LoanOrder = make([]earntypes.LoanOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o earntypes.LoanOrder = earntypes.LoanOrder{
			OrderID:    rows[i].OrderID,
			LoanCoin:   rows[i].LoanCoin,
			PledgeCoin: rows[i].PledgeCoin,
		}
		if err = l.dec("Loan.GetOngoingOrders", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&o.LoanAmount, rows[i].LoanAmount},
			{&o.InterestAmount, rows[i].InterestAmount},
			{&o.HourInterestRate, rows[i].HourInterestRate},
			{&o.PledgeAmount, rows[i].PledgeAmount},
			{&o.PledgeRate, rows[i].PledgeRate},
			{&o.SupRate, rows[i].SupRate},
			{&o.ForceRate, rows[i].ForceRate},
		}); err != nil {
			return nil, err
		}
		o.BorrowTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].BorrowTime)
		o.ExpireTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].ExpireTime)
		out = append(out, o)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Repay — loan/repay.
// ---------------------------------------------------------------------

type loanRepayBody struct {
	OrderID     string `json:"orderId"`
	Amount      string `json:"amount,omitempty"`
	RepayUnlock string `json:"repayUnlock,omitempty"`
	RepayAll    string `json:"repayAll"`
}

type loanRepayResultRow struct {
	LoanCoin          string `json:"loanCoin"`
	PledgeCoin        string `json:"pledgeCoin"`
	RepayAmount       string `json:"repayAmount"`
	PayInterest       string `json:"payInterest"`
	RepayLoanAmount   string `json:"repayLoanAmount"`
	RepayUnlockAmount string `json:"repayUnlockAmount"`
}

// Repay repays a loan order. OrderID and RepayAll ("true" | "false") are
// required; Amount / RepayUnlock are optional. NOTE: moves real funds.
func (l *LoanClient) Repay(ctx context.Context, req earntypes.LoanRepayRequest) (earntypes.LoanRepayResult, error) {
	var out earntypes.LoanRepayResult
	switch {
	case req.OrderID == "":
		return out, errInvalid("Loan.Repay", "orderId is required")
	case req.RepayAll == "":
		return out, errInvalid("Loan.Repay", "repayAll is required (true | false)")
	}

	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/loan/repay",
		Body: loanRepayBody{
			OrderID:     req.OrderID,
			Amount:      req.Amount,
			RepayUnlock: req.RepayUnlock,
			RepayAll:    req.RepayAll,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row loanRepayResultRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Loan.Repay", err)
	}
	out.LoanCoin = row.LoanCoin
	out.PledgeCoin = row.PledgeCoin
	if err = l.dec("Loan.Repay", []struct {
		dst *decimal.Decimal
		src string
	}{
		{&out.RepayAmount, row.RepayAmount},
		{&out.PayInterest, row.PayInterest},
		{&out.RepayLoanAmount, row.RepayLoanAmount},
		{&out.RepayUnlockAmount, row.RepayUnlockAmount},
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// RevisePledge — loan/revise-pledge.
// ---------------------------------------------------------------------

type loanRevisePledgeBody struct {
	OrderID    string `json:"orderId"`
	Amount     string `json:"amount"`
	PledgeCoin string `json:"pledgeCoin"`
	ReviseType string `json:"reviseType"`
}

type loanRevisePledgeResultRow struct {
	LoanCoin        string `json:"loanCoin"`
	PledgeCoin      string `json:"pledgeCoin"`
	AfterPledgeRate string `json:"afterPledgeRate"`
}

// RevisePledge adds or removes collateral on a loan order. All request
// fields are required. NOTE: moves real collateral.
func (l *LoanClient) RevisePledge(ctx context.Context, req earntypes.LoanRevisePledgeRequest) (earntypes.LoanRevisePledgeResult, error) {
	var out earntypes.LoanRevisePledgeResult
	switch {
	case req.OrderID == "":
		return out, errInvalid("Loan.RevisePledge", "orderId is required")
	case req.Amount == "":
		return out, errInvalid("Loan.RevisePledge", "amount is required")
	case req.PledgeCoin == "":
		return out, errInvalid("Loan.RevisePledge", "pledgeCoin is required")
	case req.ReviseType == "":
		return out, errInvalid("Loan.RevisePledge", "reviseType is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/loan/revise-pledge",
		Body: loanRevisePledgeBody{
			OrderID:    req.OrderID,
			Amount:     req.Amount,
			PledgeCoin: req.PledgeCoin,
			ReviseType: req.ReviseType,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row loanRevisePledgeResultRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Loan.RevisePledge", err)
	}
	out.LoanCoin = row.LoanCoin
	out.PledgeCoin = row.PledgeCoin
	if out.AfterPledgeRate, err = bgcommon.ParseDecimalOrZero(row.AfterPledgeRate); err != nil {
		return out, errParse("Loan.RevisePledge", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetRepayHistory — loan/repay-history (page paged).
// ---------------------------------------------------------------------

type loanRepayHistoryRow struct {
	OrderID           string `json:"orderId"`
	LoanCoin          string `json:"loanCoin"`
	PledgeCoin        string `json:"pledgeCoin"`
	RepayAmount       string `json:"repayAmount"`
	PayInterest       string `json:"payInterest"`
	RepayLoanAmount   string `json:"repayLoanAmount"`
	RepayUnlockAmount string `json:"repayUnlockAmount"`
	RepayTime         string `json:"repayTime"`
}

// LoanHistoryQuery — common filters for the loan history endpoints.
// StartTimeMs / EndTimeMs are required (the venue caps the span at ~3
// months); the id / coin / side / status fields narrow the result.
type LoanHistoryQuery struct {
	OrderID     string
	LoanCoin    string
	PledgeCoin  string
	Status      string
	ReviseSide  string
	StartTimeMs int64
	EndTimeMs   int64
}

func (q LoanHistoryQuery) base(pageNo, pageSize int) url.Values {
	var v url.Values = url.Values{}
	v.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	v.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	v.Set("pageNo", strconv.Itoa(pageNo))
	v.Set("pageSize", strconv.Itoa(pageSize))
	if q.OrderID != "" {
		v.Set("orderId", q.OrderID)
	}
	return v
}

// GetRepayHistory returns the repay history within [StartTimeMs,
// EndTimeMs] (both required). Page-number paginated.
func (l *LoanClient) GetRepayHistory(ctx context.Context, q LoanHistoryQuery) ([]earntypes.LoanRepayHistory, error) {
	if q.StartTimeMs <= 0 || q.EndTimeMs <= 0 {
		return nil, errInvalid("Loan.GetRepayHistory", "startTimeMs and endTimeMs are required")
	}

	var rows []loanRepayHistoryRow
	var err error
	rows, err = paginateByPageNo(ctx, loanPageSize, func(pageNo, pageSize int) ([]loanRepayHistoryRow, error) {
		var query url.Values = q.base(pageNo, pageSize)
		if q.LoanCoin != "" {
			query.Set("loanCoin", q.LoanCoin)
		}
		if q.PledgeCoin != "" {
			query.Set("pledgeCoin", q.PledgeCoin)
		}

		var resp rest.Response
		var ferr error
		resp, _, ferr = l.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/earn/loan/repay-history",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if ferr != nil {
			return nil, ferr
		}
		var page []loanRepayHistoryRow
		if ferr = resp.UnmarshalData(&page); ferr != nil {
			return nil, errParse("Loan.GetRepayHistory", ferr)
		}
		return page, nil
	})
	if err != nil {
		return nil, err
	}

	var out []earntypes.LoanRepayHistory = make([]earntypes.LoanRepayHistory, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var h earntypes.LoanRepayHistory = earntypes.LoanRepayHistory{
			OrderID:    rows[i].OrderID,
			LoanCoin:   rows[i].LoanCoin,
			PledgeCoin: rows[i].PledgeCoin,
		}
		if err = l.dec("Loan.GetRepayHistory", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&h.RepayAmount, rows[i].RepayAmount},
			{&h.PayInterest, rows[i].PayInterest},
			{&h.RepayLoanAmount, rows[i].RepayLoanAmount},
			{&h.RepayUnlockAmount, rows[i].RepayUnlockAmount},
		}); err != nil {
			return nil, err
		}
		h.RepayTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].RepayTime)
		out = append(out, h)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetPledgeRateHistory — loan/revise-history (page paged).
// ---------------------------------------------------------------------

type loanPledgeRateHistoryRow struct {
	LoanCoin         string `json:"loanCoin"`
	PledgeCoin       string `json:"pledgeCoin"`
	OrderID          string `json:"orderId"`
	ReviseTime       string `json:"reviseTime"`
	ReviseSide       string `json:"reviseSide"`
	ReviseAmount     string `json:"reviseAmount"`
	AfterPledgeRate  string `json:"afterPledgeRate"`
	BeforePledgeRate string `json:"beforePledgeRate"`
}

// GetPledgeRateHistory returns the collateral-revision history within
// [StartTimeMs, EndTimeMs] (both required). Page-number paginated.
func (l *LoanClient) GetPledgeRateHistory(ctx context.Context, q LoanHistoryQuery) ([]earntypes.LoanPledgeRateHistory, error) {
	if q.StartTimeMs <= 0 || q.EndTimeMs <= 0 {
		return nil, errInvalid("Loan.GetPledgeRateHistory", "startTimeMs and endTimeMs are required")
	}

	var rows []loanPledgeRateHistoryRow
	var err error
	rows, err = paginateByPageNo(ctx, loanPageSize, func(pageNo, pageSize int) ([]loanPledgeRateHistoryRow, error) {
		var query url.Values = q.base(pageNo, pageSize)
		if q.ReviseSide != "" {
			query.Set("reviseSide", q.ReviseSide)
		}
		if q.PledgeCoin != "" {
			query.Set("pledgeCoin", q.PledgeCoin)
		}

		var resp rest.Response
		var ferr error
		resp, _, ferr = l.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/earn/loan/revise-history",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if ferr != nil {
			return nil, ferr
		}
		var page []loanPledgeRateHistoryRow
		if ferr = resp.UnmarshalData(&page); ferr != nil {
			return nil, errParse("Loan.GetPledgeRateHistory", ferr)
		}
		return page, nil
	})
	if err != nil {
		return nil, err
	}

	var out []earntypes.LoanPledgeRateHistory = make([]earntypes.LoanPledgeRateHistory, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var h earntypes.LoanPledgeRateHistory = earntypes.LoanPledgeRateHistory{
			OrderID:    rows[i].OrderID,
			LoanCoin:   rows[i].LoanCoin,
			PledgeCoin: rows[i].PledgeCoin,
			ReviseSide: rows[i].ReviseSide,
		}
		if err = l.dec("Loan.GetPledgeRateHistory", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&h.ReviseAmount, rows[i].ReviseAmount},
			{&h.AfterPledgeRate, rows[i].AfterPledgeRate},
			{&h.BeforePledgeRate, rows[i].BeforePledgeRate},
		}); err != nil {
			return nil, err
		}
		h.ReviseTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].ReviseTime)
		out = append(out, h)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetLoanHistory — loan/borrow-history (page paged).
// ---------------------------------------------------------------------

type loanHistoryRow struct {
	OrderID          string `json:"orderId"`
	LoanCoin         string `json:"loanCoin"`
	PledgeCoin       string `json:"pledgeCoin"`
	InitPledgeAmount string `json:"initPledgeAmount"`
	InitLoanAmount   string `json:"initLoanAmount"`
	HourRate         string `json:"hourRate"`
	Daily            string `json:"daily"`
	BorrowTime       string `json:"borrowTime"`
	Status           string `json:"status"`
}

// GetLoanHistory returns the borrow history within [StartTimeMs,
// EndTimeMs] (both required). Page-number paginated.
func (l *LoanClient) GetLoanHistory(ctx context.Context, q LoanHistoryQuery) ([]earntypes.LoanHistory, error) {
	if q.StartTimeMs <= 0 || q.EndTimeMs <= 0 {
		return nil, errInvalid("Loan.GetLoanHistory", "startTimeMs and endTimeMs are required")
	}

	var rows []loanHistoryRow
	var err error
	rows, err = paginateByPageNo(ctx, loanPageSize, func(pageNo, pageSize int) ([]loanHistoryRow, error) {
		var query url.Values = q.base(pageNo, pageSize)
		if q.LoanCoin != "" {
			query.Set("loanCoin", q.LoanCoin)
		}
		if q.PledgeCoin != "" {
			query.Set("pledgeCoin", q.PledgeCoin)
		}
		if q.Status != "" {
			query.Set("status", q.Status)
		}

		var resp rest.Response
		var ferr error
		resp, _, ferr = l.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/earn/loan/borrow-history",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if ferr != nil {
			return nil, ferr
		}
		var page []loanHistoryRow
		if ferr = resp.UnmarshalData(&page); ferr != nil {
			return nil, errParse("Loan.GetLoanHistory", ferr)
		}
		return page, nil
	})
	if err != nil {
		return nil, err
	}

	var out []earntypes.LoanHistory = make([]earntypes.LoanHistory, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var h earntypes.LoanHistory = earntypes.LoanHistory{
			OrderID:    rows[i].OrderID,
			LoanCoin:   rows[i].LoanCoin,
			PledgeCoin: rows[i].PledgeCoin,
			Status:     rows[i].Status,
			Daily:      rows[i].Daily,
		}
		if err = l.dec("Loan.GetLoanHistory", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&h.InitPledgeAmount, rows[i].InitPledgeAmount},
			{&h.InitLoanAmount, rows[i].InitLoanAmount},
			{&h.HourRate, rows[i].HourRate},
		}); err != nil {
			return nil, err
		}
		h.BorrowTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].BorrowTime)
		out = append(out, h)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetDebts — loan/debts.
// ---------------------------------------------------------------------

type loanDebtInfoRow struct {
	Coin       string `json:"coin"`
	Amount     string `json:"amount"`
	AmountUsdt string `json:"amountUsdt"`
}

type loanDebtsEnvelope struct {
	PledgeInfos []loanDebtInfoRow `json:"pledgeInfos"`
	LoanInfos   []loanDebtInfoRow `json:"loanInfos"`
}

// GetDebts returns the current outstanding pledge + loan balances.
func (l *LoanClient) GetDebts(ctx context.Context) (earntypes.LoanDebts, error) {
	var out earntypes.LoanDebts
	var resp rest.Response
	var err error
	resp, _, err = l.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/loan/debts",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env loanDebtsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("Loan.GetDebts", err)
	}
	if out.PledgeInfos, err = l.parseDebtInfos(env.PledgeInfos); err != nil {
		return out, err
	}
	if out.LoanInfos, err = l.parseDebtInfos(env.LoanInfos); err != nil {
		return out, err
	}
	return out, nil
}

func (l *LoanClient) parseDebtInfos(rows []loanDebtInfoRow) ([]earntypes.LoanDebtInfo, error) {
	var out []earntypes.LoanDebtInfo = make([]earntypes.LoanDebtInfo, 0, len(rows))
	var i int
	var err error
	for i = 0; i < len(rows); i++ {
		var d earntypes.LoanDebtInfo = earntypes.LoanDebtInfo{Coin: rows[i].Coin}
		if d.Amount, err = bgcommon.ParseDecimalOrZero(rows[i].Amount); err != nil {
			return nil, errParse("Loan.GetDebts", err)
		}
		if d.AmountUsdt, err = bgcommon.ParseDecimalOrZero(rows[i].AmountUsdt); err != nil {
			return nil, errParse("Loan.GetDebts", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetLiquidationRecords — loan/reduces (page paged).
// ---------------------------------------------------------------------

type loanLiquidationRow struct {
	OrderID         string `json:"orderId"`
	LoanCoin        string `json:"loanCoin"`
	PledgeCoin      string `json:"pledgeCoin"`
	ReduceTime      string `json:"reduceTime"`
	PledgeRate      string `json:"pledgeRate"`
	PledgePrice     string `json:"pledgePrice"`
	Status          string `json:"status"`
	PledgeAmount    string `json:"pledgeAmount"`
	ReduceFee       string `json:"reduceFee"`
	ResidueAmount   string `json:"residueAmount"`
	RunlockAmount   string `json:"runlockAmount"`
	RepayLoanAmount string `json:"repayLoanAmount"`
}

// GetLiquidationRecords returns the collateral-liquidation records within
// [StartTimeMs, EndTimeMs] (both required). Page-number paginated.
func (l *LoanClient) GetLiquidationRecords(ctx context.Context, q LoanHistoryQuery) ([]earntypes.LoanLiquidationRecord, error) {
	if q.StartTimeMs <= 0 || q.EndTimeMs <= 0 {
		return nil, errInvalid("Loan.GetLiquidationRecords", "startTimeMs and endTimeMs are required")
	}

	var rows []loanLiquidationRow
	var err error
	rows, err = paginateByPageNo(ctx, loanPageSize, func(pageNo, pageSize int) ([]loanLiquidationRow, error) {
		var query url.Values = q.base(pageNo, pageSize)
		if q.LoanCoin != "" {
			query.Set("loanCoin", q.LoanCoin)
		}
		if q.PledgeCoin != "" {
			query.Set("pledgeCoin", q.PledgeCoin)
		}
		if q.Status != "" {
			query.Set("status", q.Status)
		}

		var resp rest.Response
		var ferr error
		resp, _, ferr = l.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/earn/loan/reduces",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if ferr != nil {
			return nil, ferr
		}
		var page []loanLiquidationRow
		if ferr = resp.UnmarshalData(&page); ferr != nil {
			return nil, errParse("Loan.GetLiquidationRecords", ferr)
		}
		return page, nil
	})
	if err != nil {
		return nil, err
	}

	var out []earntypes.LoanLiquidationRecord = make([]earntypes.LoanLiquidationRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec earntypes.LoanLiquidationRecord = earntypes.LoanLiquidationRecord{
			OrderID:    rows[i].OrderID,
			LoanCoin:   rows[i].LoanCoin,
			PledgeCoin: rows[i].PledgeCoin,
			Status:     rows[i].Status,
		}
		if err = l.dec("Loan.GetLiquidationRecords", []struct {
			dst *decimal.Decimal
			src string
		}{
			{&rec.PledgeRate, rows[i].PledgeRate},
			{&rec.PledgePrice, rows[i].PledgePrice},
			{&rec.PledgeAmount, rows[i].PledgeAmount},
			{&rec.ReduceFee, rows[i].ReduceFee},
			{&rec.ResidueAmount, rows[i].ResidueAmount},
			{&rec.RunlockAmount, rows[i].RunlockAmount},
			{&rec.RepayLoanAmount, rows[i].RepayLoanAmount},
		}); err != nil {
			return nil, err
		}
		rec.ReduceTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].ReduceTime)
		out = append(out, rec)
	}
	return out, nil
}
