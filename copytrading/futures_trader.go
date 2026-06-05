/*
FILE: copytrading/futures_trader.go

DESCRIPTION:
Futures TRADER (lead) sub-client — the lead side of futures copy
trading (/api/v2/copy/mix-trader/...). A trader broadcasts futures
positions that followers mirror.

WIRED IN M3a (orders):

	GET  mix-trader/order-current-track   — GetCurrentOrders (paged)
	GET  mix-trader/order-history-track   — GetHistoryOrders (paged)
	GET  mix-trader/order-total-detail    — GetOrderSummary
	POST mix-trader/order-modify-tpsl     — ModifyTPSL
	POST mix-trader/order-close-positions — ClosePositions

The config surface (M3b) and profit surface (M3c) live in
futures_trader_config.go / futures_trader_profit.go.

ELIGIBILITY:

Every mix-trader call requires the account to be an approved Bitget
elite (lead) trader; non-trader accounts get a venue 4xx. The pinned
futures productType is sent on every call.
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

// FuturesTraderClient — futures lead-trader sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type FuturesTraderClient struct {
	c *Client
}

func newFuturesTraderClient(c *Client) *FuturesTraderClient {
	return &FuturesTraderClient{c: c}
}

func (t *FuturesTraderClient) productType() string {
	return string(t.c.productType)
}

// ---------------------------------------------------------------------
// GetCurrentOrders — order-current-track.
// ---------------------------------------------------------------------

type traderCurrentRow struct {
	TrackingNo             string `json:"trackingNo"`
	OpenOrderID            string `json:"openOrderId"`
	Symbol                 string `json:"symbol"`
	PosSide                string `json:"posSide"`
	OpenLeverage           string `json:"openLeverage"`
	OpenPriceAvg           string `json:"openPriceAvg"`
	OpenTime               string `json:"openTime"`
	OpenSize               string `json:"openSize"`
	PresetStopSurplusPrice string `json:"presetStopSurplusPrice"`
	PresetStopLossPrice    string `json:"presetStopLossPrice"`
	OpenFee                string `json:"openFee"`
	FollowCount            string `json:"followCount"`
}

type traderCurrentEnvelope struct {
	TrackingList []traderCurrentRow `json:"trackingList"`
	EndID        string             `json:"endId"`
}

// GetCurrentOrders returns the lead trader's live tracked positions for
// the pinned futures product type. `symbol` is an optional filter (pass
// "" to omit). Walks the idLessThan / endId cursor.
func (t *FuturesTraderClient) GetCurrentOrders(ctx context.Context, symbol string) ([]copytypes.TraderCurrentOrder, error) {
	var rows []traderCurrentRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.FuturesTrader.GetCurrentOrders",
		func(idLessThan string, limit int) ([]traderCurrentRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("productType", t.productType())
			query.Set("limit", strconv.Itoa(limit))
			if symbol != "" {
				query.Set("symbol", symbol)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = t.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/mix-trader/order-current-track",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env traderCurrentEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("FuturesTrader.GetCurrentOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.TraderCurrentOrder = make([]copytypes.TraderCurrentOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.TraderCurrentOrder
		o, err = convertTraderCurrentRow(rows[i])
		if err != nil {
			return nil, errParse("FuturesTrader.GetCurrentOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertTraderCurrentRow(row traderCurrentRow) (copytypes.TraderCurrentOrder, error) {
	var out copytypes.TraderCurrentOrder = copytypes.TraderCurrentOrder{
		TrackingNo:  row.TrackingNo,
		OpenOrderID: row.OpenOrderID,
		Symbol:      row.Symbol,
		PosSide:     row.PosSide,
	}
	var err error
	if out.OpenLeverage, err = bgcommon.ParseDecimalOrZero(row.OpenLeverage); err != nil {
		return copytypes.TraderCurrentOrder{}, err
	}
	if out.OpenPriceAvg, err = bgcommon.ParseDecimalOrZero(row.OpenPriceAvg); err != nil {
		return copytypes.TraderCurrentOrder{}, err
	}
	if out.OpenSize, err = bgcommon.ParseDecimalOrZero(row.OpenSize); err != nil {
		return copytypes.TraderCurrentOrder{}, err
	}
	if out.PresetStopSurplusPrice, err = bgcommon.ParseDecimalOrZero(row.PresetStopSurplusPrice); err != nil {
		return copytypes.TraderCurrentOrder{}, err
	}
	if out.PresetStopLossPrice, err = bgcommon.ParseDecimalOrZero(row.PresetStopLossPrice); err != nil {
		return copytypes.TraderCurrentOrder{}, err
	}
	if out.OpenFee, err = bgcommon.ParseDecimalOrZero(row.OpenFee); err != nil {
		return copytypes.TraderCurrentOrder{}, err
	}
	out.OpenTimeMs, _ = bgcommon.ParseInt64OrZero(row.OpenTime)
	out.FollowCount, _ = bgcommon.ParseInt64OrZero(row.FollowCount)
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistoryOrders — order-history-track.
// ---------------------------------------------------------------------

type traderHistoryRow struct {
	TrackingNo   string `json:"trackingNo"`
	Symbol       string `json:"symbol"`
	OpenOrderID  string `json:"openOrderId"`
	CloseOrderID string `json:"closeOrderId"`
	ProductType  string `json:"productType"`
	PosSide      string `json:"posSide"`
	StopType     string `json:"stopType"`
	OpenLeverage string `json:"openLeverage"`
	// Bitget ships both spellings; prefer *PriceAvg, fall back to *AvgPrice.
	OpenPriceAvg  string `json:"openPriceAvg"`
	OpenAvgPrice  string `json:"openAvgPrice"`
	OpenTime      string `json:"openTime"`
	OpenSize      string `json:"openSize"`
	CloseSize     string `json:"closeSize"`
	CloseTime     string `json:"closeTime"`
	ClosePriceAvg string `json:"closePriceAvg"`
	CloseAvgPrice string `json:"closeAvgPrice"`
	AchievedPL    string `json:"achievedPL"`
	OpenFee       string `json:"openFee"`
	CloseFee      string `json:"closeFee"`
	CTime         string `json:"cTime"`
}

type traderHistoryEnvelope struct {
	TrackingList []traderHistoryRow `json:"trackingList"`
	EndID        string             `json:"endId"`
}

// GetHistoryOrders returns the lead trader's closed tracked positions
// for the pinned futures product type in the optional
// [startTimeMs, endTimeMs] window. Walks the idLessThan / endId cursor.
func (t *FuturesTraderClient) GetHistoryOrders(ctx context.Context, symbol string, startTimeMs, endTimeMs int64) ([]copytypes.TraderHistoryOrder, error) {
	var rows []traderHistoryRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.FuturesTrader.GetHistoryOrders",
		func(idLessThan string, limit int) ([]traderHistoryRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("productType", t.productType())
			query.Set("limit", strconv.Itoa(limit))
			if symbol != "" {
				query.Set("symbol", symbol)
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
				Path:   "/api/v2/copy/mix-trader/order-history-track",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env traderHistoryEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("FuturesTrader.GetHistoryOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.TraderHistoryOrder = make([]copytypes.TraderHistoryOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.TraderHistoryOrder
		o, err = convertTraderHistoryRow(rows[i])
		if err != nil {
			return nil, errParse("FuturesTrader.GetHistoryOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertTraderHistoryRow(row traderHistoryRow) (copytypes.TraderHistoryOrder, error) {
	var out copytypes.TraderHistoryOrder = copytypes.TraderHistoryOrder{
		TrackingNo:   row.TrackingNo,
		Symbol:       row.Symbol,
		OpenOrderID:  row.OpenOrderID,
		CloseOrderID: row.CloseOrderID,
		ProductType:  row.ProductType,
		PosSide:      row.PosSide,
		StopType:     row.StopType,
	}
	var openPrice string = row.OpenPriceAvg
	if openPrice == "" {
		openPrice = row.OpenAvgPrice
	}
	var closePrice string = row.ClosePriceAvg
	if closePrice == "" {
		closePrice = row.CloseAvgPrice
	}
	var err error
	if out.OpenLeverage, err = bgcommon.ParseDecimalOrZero(row.OpenLeverage); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	if out.OpenPriceAvg, err = bgcommon.ParseDecimalOrZero(openPrice); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	if out.OpenSize, err = bgcommon.ParseDecimalOrZero(row.OpenSize); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	if out.OpenFee, err = bgcommon.ParseDecimalOrZero(row.OpenFee); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	out.OpenTimeMs, _ = bgcommon.ParseInt64OrZero(row.OpenTime)
	if out.ClosePriceAvg, err = bgcommon.ParseDecimalOrZero(closePrice); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	if out.CloseSize, err = bgcommon.ParseDecimalOrZero(row.CloseSize); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	if out.CloseFee, err = bgcommon.ParseDecimalOrZero(row.CloseFee); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	out.CloseTimeMs, _ = bgcommon.ParseInt64OrZero(row.CloseTime)
	if out.AchievedPL, err = bgcommon.ParseDecimalOrZero(row.AchievedPL); err != nil {
		return copytypes.TraderHistoryOrder{}, err
	}
	out.CTimeMs, _ = bgcommon.ParseInt64OrZero(row.CTime)
	return out, nil
}

// ---------------------------------------------------------------------
// GetOrderSummary — order-total-detail.
// ---------------------------------------------------------------------

type ratePointRow struct {
	Rate  string `json:"rate"`
	CTime string `json:"ctime"`
}

type profitPointRow struct {
	Amount string `json:"amount"`
	CTime  string `json:"ctime"`
}

type traderSummaryRow struct {
	ROI                   string           `json:"roi"`
	TradingOrderNum       string           `json:"tradingOrderNum"`
	TotalFollowerNum      string           `json:"totalFollowerNum"`
	CurrentFollowerNum    string           `json:"currentFollowerNum"`
	TotalPL               string           `json:"totalpl"`
	GainNum               string           `json:"gainNum"`
	LossNum               string           `json:"lossNum"`
	WinRate               string           `json:"winRate"`
	TotalEquity           string           `json:"totalEquity"`
	TradingPairsAvailable []string         `json:"tradingPairsAvailableList"`
	LastWeekRoiList       []ratePointRow   `json:"lastWeekRoiList"`
	LastWeekProfitList    []profitPointRow `json:"lastWeekProfitList"`
	LastMonthRoiList      []ratePointRow   `json:"lastMonthRoiList"`
	LastMonthProfitList   []profitPointRow `json:"lastMonthProfitList"`
}

// GetOrderSummary returns the lead trader's headline statistics (ROI,
// follower counts, win rate, equity, and the weekly / monthly series).
// Takes no parameters.
func (t *FuturesTraderClient) GetOrderSummary(ctx context.Context) (copytypes.TraderOrderSummary, error) {
	var out copytypes.TraderOrderSummary

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-trader/order-total-detail",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row traderSummaryRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}

	out.TotalPL = row.TotalPL
	out.TradingPairsAvailable = row.TradingPairsAvailable
	if out.ROI, err = bgcommon.ParseDecimalOrZero(row.ROI); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	if out.WinRate, err = bgcommon.ParseDecimalOrZero(row.WinRate); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	if out.TotalEquity, err = bgcommon.ParseDecimalOrZero(row.TotalEquity); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	out.TradingOrderNum, _ = bgcommon.ParseInt64OrZero(row.TradingOrderNum)
	out.TotalFollowerNum, _ = bgcommon.ParseInt64OrZero(row.TotalFollowerNum)
	out.CurrentFollowerNum, _ = bgcommon.ParseInt64OrZero(row.CurrentFollowerNum)
	out.GainNum, _ = bgcommon.ParseInt64OrZero(row.GainNum)
	out.LossNum, _ = bgcommon.ParseInt64OrZero(row.LossNum)

	if out.LastWeekROI, err = convertRatePoints(row.LastWeekRoiList); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	if out.LastMonthROI, err = convertRatePoints(row.LastMonthRoiList); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	if out.LastWeekProfit, err = convertProfitPoints(row.LastWeekProfitList); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	if out.LastMonthProfit, err = convertProfitPoints(row.LastMonthProfitList); err != nil {
		return out, errParse("FuturesTrader.GetOrderSummary", err)
	}
	return out, nil
}

func convertRatePoints(rows []ratePointRow) ([]copytypes.RatePoint, error) {
	var out []copytypes.RatePoint = make([]copytypes.RatePoint, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var p copytypes.RatePoint
		var err error
		if p.Rate, err = bgcommon.ParseDecimalOrZero(rows[i].Rate); err != nil {
			return nil, err
		}
		p.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		out = append(out, p)
	}
	return out, nil
}

func convertProfitPoints(rows []profitPointRow) ([]copytypes.ProfitPoint, error) {
	var out []copytypes.ProfitPoint = make([]copytypes.ProfitPoint, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var p copytypes.ProfitPoint
		var err error
		if p.Amount, err = bgcommon.ParseDecimalOrZero(rows[i].Amount); err != nil {
			return nil, err
		}
		p.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// ModifyTPSL — order-modify-tpsl.
// ---------------------------------------------------------------------

type traderModifyTPSLBody struct {
	ProductType      string `json:"productType"`
	TrackingNo       string `json:"trackingNo"`
	Symbol           string `json:"symbol,omitempty"`
	StopSurplusPrice string `json:"stopSurplusPrice,omitempty"`
	StopLossPrice    string `json:"stopLossPrice,omitempty"`
}

// ModifyTPSL sets / updates / cancels the take-profit and stop-loss on a
// tracked lead position. TrackingNo is required and at least one of the
// prices must be set. Price semantics: empty = leave unchanged, "0" =
// cancel existing, > 0 = set/update.
func (t *FuturesTraderClient) ModifyTPSL(ctx context.Context, req copytypes.TraderModifyTPSLRequest) error {
	if req.TrackingNo == "" {
		return errInvalid("FuturesTrader.ModifyTPSL", "trackingNo is empty")
	}
	if req.StopSurplusPrice == "" && req.StopLossPrice == "" {
		return errInvalid("FuturesTrader.ModifyTPSL", "one of stopSurplusPrice / stopLossPrice is required")
	}
	var _, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-trader/order-modify-tpsl",
		Body: traderModifyTPSLBody{
			ProductType:      t.productType(),
			TrackingNo:       req.TrackingNo,
			Symbol:           req.Symbol,
			StopSurplusPrice: req.StopSurplusPrice,
			StopLossPrice:    req.StopLossPrice,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// ClosePositions — order-close-positions.
// ---------------------------------------------------------------------

type traderCloseBody struct {
	ProductType string `json:"productType"`
	TrackingNo  string `json:"trackingNo,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
}

type traderClosedRow struct {
	TrackingNo  string `json:"trackingNo"`
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
}

// ClosePositions closes one, some, or all of the lead trader's tracked
// positions on the pinned product type. An empty request (no TrackingNo
// / Symbol) closes every position on that product line. Returns the
// tracking orders the close actually targeted.
func (t *FuturesTraderClient) ClosePositions(ctx context.Context, req copytypes.TraderCloseRequest) ([]copytypes.TraderClosedOrder, error) {
	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-trader/order-close-positions",
		Body: traderCloseBody{
			ProductType: t.productType(),
			TrackingNo:  req.TrackingNo,
			Symbol:      req.Symbol,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []traderClosedRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("FuturesTrader.ClosePositions", err)
	}
	var out []copytypes.TraderClosedOrder = make([]copytypes.TraderClosedOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		out = append(out, copytypes.TraderClosedOrder{
			TrackingNo:  rows[i].TrackingNo,
			Symbol:      rows[i].Symbol,
			ProductType: rows[i].ProductType,
		})
	}
	return out, nil
}
