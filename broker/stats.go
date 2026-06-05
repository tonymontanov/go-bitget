/*
FILE: broker/stats.go

DESCRIPTION:
Stats sub-client — institutional-broker reporting
(/api/v2/broker/{subaccounts,commissions,trade-volume,total-commission,
order-commission,rebate-info}). All reads; require the account to be an
approved Bitget broker (a non-broker account gets a 4xx).

subaccounts / commissions / trade-volume are pageNo/pageSize paginated and
fully walked; order-commission walks the idLessThan/endId cursor;
total-commission returns a daily slice; rebate-info needs a uid.

Request params verified against the Bitget V2 broker docs and the
tiagosiebler reference client.
*/

package broker

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	brokertypes "github.com/tonymontanov/go-bitget/v2/broker/types"
)

// StatsClient — institutional-broker reporting sub-client.
type StatsClient struct {
	c *Client
}

func newStatsClient(c *Client) *StatsClient {
	return &StatsClient{c: c}
}

const statsPageSize = 100

// BrokerReportQuery — optional time-window for the page-number reports
// (subaccounts / trade-volume) and total-commission.
type BrokerReportQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
}

func (q BrokerReportQuery) apply(v url.Values) {
	if q.StartTimeMs > 0 {
		v.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	}
	if q.EndTimeMs > 0 {
		v.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	}
}

// ---------------------------------------------------------------------
// GetSubaccounts — broker/subaccounts (pageNo/pageSize paged).
// ---------------------------------------------------------------------

type brokerSubInfoRow struct {
	UID              string `json:"uid"`
	Asset            string `json:"asset"`
	FirstTimeDeposit string `json:"firstTimeDeposit"`
	FirstTimeTrade   string `json:"firstTimeTrade"`
	RegisterTime     string `json:"registerTime"`
}

// GetSubaccounts lists the broker's tracked sub-accounts with first-deposit
// / first-trade / register markers. Fully walks pageNo/pageSize.
func (s *StatsClient) GetSubaccounts(ctx context.Context, q BrokerReportQuery) ([]brokertypes.BrokerSubaccountInfo, error) {
	var rows []brokerSubInfoRow
	var err error
	rows, err = paginateByPageNo(ctx, statsPageSize,
		func(pageNo, pageSize int) ([]brokerSubInfoRow, error) {
			var query url.Values = url.Values{}
			q.apply(query)
			query.Set("pageNo", strconv.Itoa(pageNo))
			query.Set("pageSize", strconv.Itoa(pageSize))
			var page []brokerSubInfoRow
			if ferr := s.getList(ctx, "/api/v2/broker/subaccounts", query, &page, "Stats.GetSubaccounts"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.BrokerSubaccountInfo = make([]brokertypes.BrokerSubaccountInfo, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r brokertypes.BrokerSubaccountInfo = brokertypes.BrokerSubaccountInfo{UID: rows[i].UID}
		if r.Asset, err = bgcommon.ParseDecimalOrZero(rows[i].Asset); err != nil {
			return nil, errParse("Stats.GetSubaccounts", err)
		}
		r.FirstTimeDepositMs, _ = bgcommon.ParseInt64OrZero(rows[i].FirstTimeDeposit)
		r.FirstTimeTradeMs, _ = bgcommon.ParseInt64OrZero(rows[i].FirstTimeTrade)
		r.RegisterTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].RegisterTime)
		out = append(out, r)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCommissions — broker/commissions (pageNo/pageSize paged).
// ---------------------------------------------------------------------

// BrokerCommissionsQuery — filters for GetCommissions.
//
//	BizType:    spot | futures
//	SubBizType: spot_trade | spot_margin | usdt_futures | usdc_futures | coin_futures
type BrokerCommissionsQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
	BizType     string
	SubBizType  string
}

type brokerCommissionRow struct {
	UID             string `json:"uid"`
	Coin            string `json:"coin"`
	Symbol          string `json:"symbol"`
	DealtAmount     string `json:"dealtAmount"`
	TotalFee        string `json:"totalFee"`
	DeductedFee     string `json:"deductedFee"`
	PaidFee         string `json:"paidFee"`
	MarkUpFee       string `json:"markUpFee"`
	TotalCommission string `json:"totalCommission"`
}

// GetCommissions lists per-sub-account commission rows. Fully walks
// pageNo/pageSize.
func (s *StatsClient) GetCommissions(ctx context.Context, q BrokerCommissionsQuery) ([]brokertypes.BrokerCommission, error) {
	var rows []brokerCommissionRow
	var err error
	rows, err = paginateByPageNo(ctx, statsPageSize,
		func(pageNo, pageSize int) ([]brokerCommissionRow, error) {
			var query url.Values = url.Values{}
			if q.StartTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			}
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if q.BizType != "" {
				query.Set("bizType", q.BizType)
			}
			if q.SubBizType != "" {
				query.Set("subBizType", q.SubBizType)
			}
			query.Set("pageNo", strconv.Itoa(pageNo))
			query.Set("pageSize", strconv.Itoa(pageSize))
			var page []brokerCommissionRow
			if ferr := s.getList(ctx, "/api/v2/broker/commissions", query, &page, "Stats.GetCommissions"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.BrokerCommission = make([]brokertypes.BrokerCommission, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var c brokertypes.BrokerCommission = brokertypes.BrokerCommission{UID: r.UID, Coin: r.Coin, Symbol: r.Symbol}
		var scope = "Stats.GetCommissions"
		if c.DealtAmount, err = bgcommon.ParseDecimalOrZero(r.DealtAmount); err != nil {
			return nil, errParse(scope, err)
		}
		if c.TotalFee, err = bgcommon.ParseDecimalOrZero(r.TotalFee); err != nil {
			return nil, errParse(scope, err)
		}
		if c.DeductedFee, err = bgcommon.ParseDecimalOrZero(r.DeductedFee); err != nil {
			return nil, errParse(scope, err)
		}
		if c.PaidFee, err = bgcommon.ParseDecimalOrZero(r.PaidFee); err != nil {
			return nil, errParse(scope, err)
		}
		if c.MarkUpFee, err = bgcommon.ParseDecimalOrZero(r.MarkUpFee); err != nil {
			return nil, errParse(scope, err)
		}
		if c.TotalCommission, err = bgcommon.ParseDecimalOrZero(r.TotalCommission); err != nil {
			return nil, errParse(scope, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetTradeVolume — broker/trade-volume (pageNo/pageSize paged).
// ---------------------------------------------------------------------

type brokerTradeVolumeRow struct {
	UID          string `json:"uid"`
	Volume       string `json:"volume"`
	SpotVolume   string `json:"spotVolume"`
	FutureVolume string `json:"futureVolume"`
}

// GetTradeVolume lists per-sub-account trade volume. Fully walks
// pageNo/pageSize.
func (s *StatsClient) GetTradeVolume(ctx context.Context, q BrokerReportQuery) ([]brokertypes.BrokerTradeVolume, error) {
	var rows []brokerTradeVolumeRow
	var err error
	rows, err = paginateByPageNo(ctx, statsPageSize,
		func(pageNo, pageSize int) ([]brokerTradeVolumeRow, error) {
			var query url.Values = url.Values{}
			q.apply(query)
			query.Set("pageNo", strconv.Itoa(pageNo))
			query.Set("pageSize", strconv.Itoa(pageSize))
			var page []brokerTradeVolumeRow
			if ferr := s.getList(ctx, "/api/v2/broker/trade-volume", query, &page, "Stats.GetTradeVolume"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.BrokerTradeVolume = make([]brokertypes.BrokerTradeVolume, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var v brokertypes.BrokerTradeVolume = brokertypes.BrokerTradeVolume{UID: rows[i].UID}
		if v.Volume, err = bgcommon.ParseDecimalOrZero(rows[i].Volume); err != nil {
			return nil, errParse("Stats.GetTradeVolume", err)
		}
		if v.SpotVolume, err = bgcommon.ParseDecimalOrZero(rows[i].SpotVolume); err != nil {
			return nil, errParse("Stats.GetTradeVolume", err)
		}
		if v.FutureVolume, err = bgcommon.ParseDecimalOrZero(rows[i].FutureVolume); err != nil {
			return nil, errParse("Stats.GetTradeVolume", err)
		}
		out = append(out, v)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetTotalCommission — broker/total-commission (daily slice).
// ---------------------------------------------------------------------

type brokerSegmentRow struct {
	SpotTradingVolume     string `json:"spotTradingVolume"`
	SpotTradingFee        string `json:"spotTradingFee"`
	SpotPureTradingFee    string `json:"spotPureTradingFee"`
	SpotCommission        string `json:"spotCommission"`
	FuturesTradingVolume  string `json:"futuresTradingVolume"`
	FuturesTradingFee     string `json:"futuresTradingFee"`
	FuturesPureTradingFee string `json:"futuresPureTradingFee"`
	FuturesCommission     string `json:"futuresCommission"`
}

type brokerTotalCommissionRow struct {
	Date               string           `json:"date"`
	TotalTradingVolume string           `json:"totalTradingVolume"`
	TotalActiveTraders string           `json:"totalActiveTraders"`
	TotalCommission    string           `json:"totalCommission"`
	Spot               brokerSegmentRow `json:"spot"`
	Futures            brokerSegmentRow `json:"futures"`
}

// GetTotalCommission returns the daily broker totals. startTime / endTime
// must be both set or both unset; unset defaults to yesterday (UTC+8).
func (s *StatsClient) GetTotalCommission(ctx context.Context, q BrokerReportQuery) ([]brokertypes.BrokerTotalCommission, error) {
	var query url.Values = url.Values{}
	q.apply(query)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/total-commission",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []brokerTotalCommissionRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Stats.GetTotalCommission", err)
	}
	var out []brokertypes.BrokerTotalCommission = make([]brokertypes.BrokerTotalCommission, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var tc brokertypes.BrokerTotalCommission = brokertypes.BrokerTotalCommission{Date: r.Date}
		if tc.TotalTradingVolume, err = bgcommon.ParseDecimalOrZero(r.TotalTradingVolume); err != nil {
			return nil, errParse("Stats.GetTotalCommission", err)
		}
		tc.TotalActiveTraders, _ = bgcommon.ParseInt64OrZero(r.TotalActiveTraders)
		if tc.TotalCommission, err = bgcommon.ParseDecimalOrZero(r.TotalCommission); err != nil {
			return nil, errParse("Stats.GetTotalCommission", err)
		}
		if tc.Spot, err = parseSegment(r.Spot, true); err != nil {
			return nil, errParse("Stats.GetTotalCommission", err)
		}
		if tc.Futures, err = parseSegment(r.Futures, false); err != nil {
			return nil, errParse("Stats.GetTotalCommission", err)
		}
		out = append(out, tc)
	}
	return out, nil
}

// parseSegment maps the spot or futures sub-object (the field prefixes
// differ) into the shared BrokerSegmentCommission.
func parseSegment(r brokerSegmentRow, spot bool) (brokertypes.BrokerSegmentCommission, error) {
	var seg brokertypes.BrokerSegmentCommission
	var vol, fee, pure, comm string
	if spot {
		vol, fee, pure, comm = r.SpotTradingVolume, r.SpotTradingFee, r.SpotPureTradingFee, r.SpotCommission
	} else {
		vol, fee, pure, comm = r.FuturesTradingVolume, r.FuturesTradingFee, r.FuturesPureTradingFee, r.FuturesCommission
	}
	var err error
	if seg.TradingVolume, err = bgcommon.ParseDecimalOrZero(vol); err != nil {
		return seg, err
	}
	if seg.TradingFee, err = bgcommon.ParseDecimalOrZero(fee); err != nil {
		return seg, err
	}
	if seg.PureTradingFee, err = bgcommon.ParseDecimalOrZero(pure); err != nil {
		return seg, err
	}
	if seg.Commission, err = bgcommon.ParseDecimalOrZero(comm); err != nil {
		return seg, err
	}
	return seg, nil
}

// ---------------------------------------------------------------------
// GetOrderCommission — broker/order-commission (idLessThan/endId cursor).
// ---------------------------------------------------------------------

// BrokerOrderCommissionQuery — filters for GetOrderCommission.
type BrokerOrderCommissionQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
	UID         string
	OrderID     string
}

type brokerOrderCommissionRow struct {
	FillID       string `json:"fillId"`
	OrderID      string `json:"orderId"`
	TS           string `json:"ts"`
	ClientOid    string `json:"clientOid"`
	BizType      string `json:"bizType"`
	SubBizType   string `json:"subBizType"`
	Symbol       string `json:"symbol"`
	Volume       string `json:"volume"`
	Fee          string `json:"fee"`
	PureFee      string `json:"pureFee"`
	RebateAmount string `json:"rebateAmount"`
}

type brokerOrderCommissionEnvelope struct {
	CommissionList []brokerOrderCommissionRow `json:"commissionlist"`
	EndID          string                     `json:"endId"`
}

// GetOrderCommission lists per-fill broker commissions. Walks the
// idLessThan/endId cursor.
func (s *StatsClient) GetOrderCommission(ctx context.Context, q BrokerOrderCommissionQuery) ([]brokertypes.BrokerOrderCommissionItem, error) {
	var rows []brokerOrderCommissionRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.Stats.GetOrderCommission",
		func(idLessThan string, limit int) ([]brokerOrderCommissionRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if q.StartTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			}
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if q.UID != "" {
				query.Set("uid", q.UID)
			}
			if q.OrderID != "" {
				query.Set("orderId", q.OrderID)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/broker/order-commission",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env brokerOrderCommissionEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Stats.GetOrderCommission", ferr)
			}
			return env.CommissionList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.BrokerOrderCommissionItem = make([]brokertypes.BrokerOrderCommissionItem, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var it brokertypes.BrokerOrderCommissionItem = brokertypes.BrokerOrderCommissionItem{
			FillID:     r.FillID,
			OrderID:    r.OrderID,
			ClientOid:  r.ClientOid,
			BizType:    r.BizType,
			SubBizType: r.SubBizType,
			Symbol:     r.Symbol,
		}
		it.TimeMs, _ = bgcommon.ParseInt64OrZero(r.TS)
		if it.Volume, err = bgcommon.ParseDecimalOrZero(r.Volume); err != nil {
			return nil, errParse("Stats.GetOrderCommission", err)
		}
		if it.Fee, err = bgcommon.ParseDecimalOrZero(r.Fee); err != nil {
			return nil, errParse("Stats.GetOrderCommission", err)
		}
		if it.PureFee, err = bgcommon.ParseDecimalOrZero(r.PureFee); err != nil {
			return nil, errParse("Stats.GetOrderCommission", err)
		}
		if it.RebateAmount, err = bgcommon.ParseDecimalOrZero(r.RebateAmount); err != nil {
			return nil, errParse("Stats.GetOrderCommission", err)
		}
		out = append(out, it)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetRebateInfo — broker/rebate-info.
// ---------------------------------------------------------------------

type brokerRebateInfoRow struct {
	AffiliationType          string `json:"affiliationType"`
	UserLevel                string `json:"userLevel"`
	ClientSpotRebateRatio    string `json:"clientSpotRebateRatio"`
	ClientFuturesRebateRatio string `json:"clientFuturesRebateRatio"`
}

// GetRebateInfo returns the rebate config for a customer uid (required).
func (s *StatsClient) GetRebateInfo(ctx context.Context, uid string) (brokertypes.BrokerRebateInfo, error) {
	var out brokertypes.BrokerRebateInfo
	if uid == "" {
		return out, errInvalid("Stats.GetRebateInfo", "uid is required")
	}

	var query url.Values = url.Values{}
	query.Set("uid", uid)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/rebate-info",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row brokerRebateInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Stats.GetRebateInfo", err)
	}
	out.AffiliationType = row.AffiliationType
	out.UserLevel = row.UserLevel
	if out.ClientSpotRebateRatio, err = bgcommon.ParseDecimalOrZero(row.ClientSpotRebateRatio); err != nil {
		return out, errParse("Stats.GetRebateInfo", err)
	}
	if out.ClientFuturesRebateRatio, err = bgcommon.ParseDecimalOrZero(row.ClientFuturesRebateRatio); err != nil {
		return out, errParse("Stats.GetRebateInfo", err)
	}
	return out, nil
}

// getList is the shared GET → JSON-array decode used by the page-number
// reports. dst must be a pointer to a slice of row structs.
func (s *StatsClient) getList(ctx context.Context, path string, query url.Values, dst any, scope string) error {
	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return err
	}
	if err = resp.UnmarshalData(dst); err != nil {
		return errParse(scope, err)
	}
	return nil
}
