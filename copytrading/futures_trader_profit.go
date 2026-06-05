/*
FILE: copytrading/futures_trader_profit.go

DESCRIPTION:
Futures TRADER profit / profit-share surface (M3c).

WIRED:

	GET mix-trader/profit-history-summarys — GetProfitSummary
	GET mix-trader/profit-history-details  — GetProfitShareHistory (cursor)
	GET mix-trader/profit-details          — GetPendingProfitShare (page-no)
	GET mix-trader/profits-group-coin-date — GetProfitByCoinDate (page-no)

These endpoints are NOT product-type scoped (profit share is settled per
coin across the account), so they do not send productType.
*/

package copytrading

import (
	"context"
	"net/url"
	"strconv"

	copytypes "github.com/tonymontanov/go-bitget/v2/copytrading/types"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// pageNoMaxPages bounds the page-number paginators below (defensive
// against a buggy page echo).
const pageNoMaxPages = 50

// paginateByPageNo walks the classic pageNo / pageSize protocol used by
// several copy-trading list endpoints: call page 1, 2, ... until a short
// page or the page ceiling. `fetch` issues one page and returns its
// rows.
func paginateByPageNo[T any](
	ctx context.Context,
	pageSize int,
	fetch func(pageNo, pageSize int) ([]T, error),
) ([]T, error) {
	var out []T
	var page int
	for page = 1; page <= pageNoMaxPages; page++ {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}
		var rows []T
		var err error
		rows, err = fetch(page, pageSize)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		out = append(out, rows...)
		if len(rows) < pageSize {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetProfitSummary — profit-history-summarys.
// ---------------------------------------------------------------------

type profitSummaryObj struct {
	YesterdayProfit string `json:"yesterdayProfit"`
	SumProfit       string `json:"sumProfit"`
	WaitProfit      string `json:"waitProfit"`
	YesterdayTime   string `json:"yesterdayTime"`
}

type profitHistoryCoinRow struct {
	Coin           string `json:"coin"`
	ProfitCount    string `json:"profitCount"`
	LastProfitTime string `json:"lastProfitTime"`
}

type profitSummaryEnvelope struct {
	ProfitSummary     profitSummaryObj       `json:"profitSummary"`
	ProfitHistoryList []profitHistoryCoinRow `json:"profitHistoryList"`
}

// GetProfitSummary returns the lead trader's headline profit-share
// figures plus the per-currency breakdown. Takes no parameters.
func (t *FuturesTraderClient) GetProfitSummary(ctx context.Context) (copytypes.ProfitSummary, error) {
	var out copytypes.ProfitSummary

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-trader/profit-history-summarys",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env profitSummaryEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("FuturesTrader.GetProfitSummary", err)
	}

	if out.YesterdayProfit, err = bgcommon.ParseDecimalOrZero(env.ProfitSummary.YesterdayProfit); err != nil {
		return out, errParse("FuturesTrader.GetProfitSummary", err)
	}
	if out.SumProfit, err = bgcommon.ParseDecimalOrZero(env.ProfitSummary.SumProfit); err != nil {
		return out, errParse("FuturesTrader.GetProfitSummary", err)
	}
	if out.WaitProfit, err = bgcommon.ParseDecimalOrZero(env.ProfitSummary.WaitProfit); err != nil {
		return out, errParse("FuturesTrader.GetProfitSummary", err)
	}
	out.YesterdayTimeMs, _ = bgcommon.ParseInt64OrZero(env.ProfitSummary.YesterdayTime)

	out.History = make([]copytypes.ProfitHistoryCoin, 0, len(env.ProfitHistoryList))
	var i int
	for i = 0; i < len(env.ProfitHistoryList); i++ {
		var h copytypes.ProfitHistoryCoin = copytypes.ProfitHistoryCoin{Coin: env.ProfitHistoryList[i].Coin}
		if h.ProfitCount, err = bgcommon.ParseDecimalOrZero(env.ProfitHistoryList[i].ProfitCount); err != nil {
			return out, errParse("FuturesTrader.GetProfitSummary", err)
		}
		h.LastProfitTimeMs, _ = bgcommon.ParseInt64OrZero(env.ProfitHistoryList[i].LastProfitTime)
		out.History = append(out.History, h)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetProfitShareHistory — profit-history-details.
// ---------------------------------------------------------------------

type profitShareRow struct {
	ProfitID   string `json:"profitId"`
	Coin       string `json:"coin"`
	Profit     string `json:"profit"`
	NickName   string `json:"nickName"`
	ProfitTime string `json:"profitTime"`
}

type profitShareEnvelope struct {
	ProfitList []profitShareRow `json:"profitList"`
	EndID      string           `json:"endId"`
}

// GetProfitShareHistory returns distributed profit-share events,
// optionally filtered by settlement `coin` and the [startTimeMs,
// endTimeMs] window. Walks the idLessThan / endId cursor.
func (t *FuturesTraderClient) GetProfitShareHistory(ctx context.Context, coin string, startTimeMs, endTimeMs int64) ([]copytypes.ProfitShareRecord, error) {
	var rows []profitShareRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.FuturesTrader.GetProfitShareHistory",
		func(idLessThan string, limit int) ([]profitShareRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if coin != "" {
				query.Set("coin", coin)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			if startTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
			}
			if endTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = t.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/mix-trader/profit-history-details",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env profitShareEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("FuturesTrader.GetProfitShareHistory", ferr)
			}
			return env.ProfitList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.ProfitShareRecord = make([]copytypes.ProfitShareRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec copytypes.ProfitShareRecord = copytypes.ProfitShareRecord{
			ProfitID: rows[i].ProfitID,
			Coin:     rows[i].Coin,
			NickName: rows[i].NickName,
		}
		if rec.Profit, err = bgcommon.ParseDecimalOrZero(rows[i].Profit); err != nil {
			return nil, errParse("FuturesTrader.GetProfitShareHistory", err)
		}
		rec.ProfitTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].ProfitTime)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetPendingProfitShare — profit-details.
// ---------------------------------------------------------------------

type pendingProfitRow struct {
	Coin     string `json:"coin"`
	Profit   string `json:"profit"`
	NickName string `json:"nickName"`
}

// GetPendingProfitShare returns the to-be-distributed profit shares (per
// follower), optionally filtered by settlement `coin`. Uses page-number
// pagination.
func (t *FuturesTraderClient) GetPendingProfitShare(ctx context.Context, coin string) ([]copytypes.PendingProfitShare, error) {
	return paginateByPageNo(ctx, 100, func(pageNo, pageSize int) ([]copytypes.PendingProfitShare, error) {
		var query url.Values = url.Values{}
		query.Set("pageNo", strconv.Itoa(pageNo))
		query.Set("pageSize", strconv.Itoa(pageSize))
		if coin != "" {
			query.Set("coin", coin)
		}

		var resp rest.Response
		var err error
		resp, _, err = t.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/copy/mix-trader/profit-details",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}

		var rows []pendingProfitRow
		if err = resp.UnmarshalData(&rows); err != nil {
			return nil, errParse("FuturesTrader.GetPendingProfitShare", err)
		}
		var out []copytypes.PendingProfitShare = make([]copytypes.PendingProfitShare, 0, len(rows))
		var i int
		for i = 0; i < len(rows); i++ {
			var p copytypes.PendingProfitShare = copytypes.PendingProfitShare{
				Coin:     rows[i].Coin,
				NickName: rows[i].NickName,
			}
			if p.Profit, err = bgcommon.ParseDecimalOrZero(rows[i].Profit); err != nil {
				return nil, errParse("FuturesTrader.GetPendingProfitShare", err)
			}
			out = append(out, p)
		}
		return out, nil
	})
}

// ---------------------------------------------------------------------
// GetProfitByCoinDate — profits-group-coin-date.
// ---------------------------------------------------------------------

type profitByCoinDateRow struct {
	Coin       string `json:"coin"`
	Profit     string `json:"profit"`
	ProfitTime string `json:"profitTime"`
}

// GetProfitByCoinDate returns realised profit aggregated by currency and
// date. Uses page-number pagination (pageSize capped at 50 per the
// venue).
func (t *FuturesTraderClient) GetProfitByCoinDate(ctx context.Context) ([]copytypes.ProfitByCoinDate, error) {
	return paginateByPageNo(ctx, 50, func(pageNo, pageSize int) ([]copytypes.ProfitByCoinDate, error) {
		var query url.Values = url.Values{}
		query.Set("pageNo", strconv.Itoa(pageNo))
		query.Set("pageSize", strconv.Itoa(pageSize))

		var resp rest.Response
		var err error
		resp, _, err = t.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/copy/mix-trader/profits-group-coin-date",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}

		var rows []profitByCoinDateRow
		if err = resp.UnmarshalData(&rows); err != nil {
			return nil, errParse("FuturesTrader.GetProfitByCoinDate", err)
		}
		var out []copytypes.ProfitByCoinDate = make([]copytypes.ProfitByCoinDate, 0, len(rows))
		var i int
		for i = 0; i < len(rows); i++ {
			var p copytypes.ProfitByCoinDate = copytypes.ProfitByCoinDate{Coin: rows[i].Coin}
			if p.Profit, err = bgcommon.ParseDecimalOrZero(rows[i].Profit); err != nil {
				return nil, errParse("FuturesTrader.GetProfitByCoinDate", err)
			}
			p.ProfitTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].ProfitTime)
			out = append(out, p)
		}
		return out, nil
	})
}
