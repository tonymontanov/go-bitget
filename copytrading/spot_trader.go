/*
FILE: copytrading/spot_trader.go

DESCRIPTION:
Spot TRADER (lead) sub-client — the lead side of spot copy trading
(/api/v2/copy/spot-trader/...). A spot trader broadcasts spot buy/sell
tracking orders that followers mirror.

WIRED IN M4a:

	GET  order-current-track     — GetCurrentOrders (paged)
	GET  order-history-track     — GetHistoryOrders (paged)
	GET  order-total-detail      — GetOrderSummary
	POST order-modify-tpsl       — ModifyTPSL
	POST order-close-tracking    — ClosePositions (sell)
	GET  config-query-settings   — GetConfig
	POST config-setting-symbols  — SetSymbols
	GET  config-query-followers  — GetFollowers (page-no paged)
	POST config-remove-follower  — RemoveFollower
	GET  profit-summarys         — GetProfitSummary
	GET  profit-history-details  — GetProfitShareHistory (paged)
	GET  profit-details          — GetPendingProfitShare (page-no paged)

NO PRODUCT TYPE: spot copy trading is spot; none of these calls send
productType (the pinned futures product type is ignored here).

ELIGIBILITY: every call requires the account to be an approved Bitget
spot elite (lead) trader; non-trader accounts get a venue 4xx.
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

// SpotTraderClient — spot lead-trader sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type SpotTraderClient struct {
	c *Client
}

func newSpotTraderClient(c *Client) *SpotTraderClient {
	return &SpotTraderClient{c: c}
}

// ---------------------------------------------------------------------
// GetCurrentOrders — order-current-track.
// ---------------------------------------------------------------------

type spotTraderCurrentRow struct {
	TrackingNo       string `json:"trackingNo"`
	OrderID          string `json:"orderId"`
	Symbol           string `json:"symbol"`
	BuyFillSize      string `json:"buyFillSize"`
	BuyDelegateSize  string `json:"buyDelegateSize"`
	BuyPrice         string `json:"buyPrice"`
	BuyFee           string `json:"buyFee"`
	BuyTime          string `json:"buyTime"`
	UnrealizedPL     string `json:"unrealizedPL"`
	UnrealizedPLR    string `json:"unrealizedPLR"`
	StopSurplusPrice string `json:"stopSurplusPrice"`
	StopLossPrice    string `json:"stopLossPrice"`
	FollowCount      string `json:"followCount"`
}

type spotTraderCurrentEnvelope struct {
	TrackingList []spotTraderCurrentRow `json:"trackingList"`
	EndID        string                 `json:"endId"`
}

// GetCurrentOrders returns the spot lead trader's live tracking orders.
// `symbol` is an optional filter. Walks the idLessThan / endId cursor.
func (s *SpotTraderClient) GetCurrentOrders(ctx context.Context, symbol string) ([]copytypes.SpotTraderCurrentOrder, error) {
	var rows []spotTraderCurrentRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.SpotTrader.GetCurrentOrders",
		func(idLessThan string, limit int) ([]spotTraderCurrentRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if symbol != "" {
				query.Set("symbol", symbol)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/spot-trader/order-current-track",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env spotTraderCurrentEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SpotTrader.GetCurrentOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.SpotTraderCurrentOrder = make([]copytypes.SpotTraderCurrentOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.SpotTraderCurrentOrder
		o, err = convertSpotTraderCurrentRow(rows[i])
		if err != nil {
			return nil, errParse("SpotTrader.GetCurrentOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertSpotTraderCurrentRow(row spotTraderCurrentRow) (copytypes.SpotTraderCurrentOrder, error) {
	var out copytypes.SpotTraderCurrentOrder = copytypes.SpotTraderCurrentOrder{
		TrackingNo: row.TrackingNo,
		OrderID:    row.OrderID,
		Symbol:     row.Symbol,
	}
	var err error
	if out.BuyFillSize, err = bgcommon.ParseDecimalOrZero(row.BuyFillSize); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	if out.BuyDelegateSize, err = bgcommon.ParseDecimalOrZero(row.BuyDelegateSize); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	if out.BuyPrice, err = bgcommon.ParseDecimalOrZero(row.BuyPrice); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	if out.BuyFee, err = bgcommon.ParseDecimalOrZero(row.BuyFee); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	out.BuyTimeMs, _ = bgcommon.ParseInt64OrZero(row.BuyTime)
	if out.UnrealizedPL, err = bgcommon.ParseDecimalOrZero(row.UnrealizedPL); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	if out.UnrealizedPLR, err = bgcommon.ParseDecimalOrZero(row.UnrealizedPLR); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	if out.StopSurplusPrice, err = bgcommon.ParseDecimalOrZero(row.StopSurplusPrice); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	if out.StopLossPrice, err = bgcommon.ParseDecimalOrZero(row.StopLossPrice); err != nil {
		return copytypes.SpotTraderCurrentOrder{}, err
	}
	out.FollowCount, _ = bgcommon.ParseInt64OrZero(row.FollowCount)
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistoryOrders — order-history-track.
// ---------------------------------------------------------------------

type spotTraderHistoryRow struct {
	TrackingNo  string `json:"trackingNo"`
	Symbol      string `json:"symbol"`
	FillSize    string `json:"fillSize"`
	BuyPrice    string `json:"buyPrice"`
	SellPrice   string `json:"sellPrice"`
	BuyFee      string `json:"buyFee"`
	SellFee     string `json:"sellFee"`
	BuyTime     string `json:"buyTime"`
	SellTime    string `json:"sellTime"`
	AchievedPL  string `json:"achievedPL"`
	AchievedPLR string `json:"achievedPLR"`
	NetProfit   string `json:"netProfit"`
	FollowCount string `json:"followCount"`
}

type spotTraderHistoryEnvelope struct {
	TrackingList []spotTraderHistoryRow `json:"trackingList"`
	EndID        string                 `json:"endId"`
}

// GetHistoryOrders returns the spot lead trader's closed tracking orders
// in the optional [startTimeMs, endTimeMs] window. Walks the
// idLessThan / endId cursor.
func (s *SpotTraderClient) GetHistoryOrders(ctx context.Context, symbol string, startTimeMs, endTimeMs int64) ([]copytypes.SpotTraderHistoryOrder, error) {
	var rows []spotTraderHistoryRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.SpotTrader.GetHistoryOrders",
		func(idLessThan string, limit int) ([]spotTraderHistoryRow, string, error) {
			var query url.Values = url.Values{}
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
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/spot-trader/order-history-track",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env spotTraderHistoryEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SpotTrader.GetHistoryOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.SpotTraderHistoryOrder = make([]copytypes.SpotTraderHistoryOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.SpotTraderHistoryOrder
		o, err = convertSpotTraderHistoryRow(rows[i])
		if err != nil {
			return nil, errParse("SpotTrader.GetHistoryOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertSpotTraderHistoryRow(row spotTraderHistoryRow) (copytypes.SpotTraderHistoryOrder, error) {
	var out copytypes.SpotTraderHistoryOrder = copytypes.SpotTraderHistoryOrder{
		TrackingNo: row.TrackingNo,
		Symbol:     row.Symbol,
	}
	var err error
	if out.FillSize, err = bgcommon.ParseDecimalOrZero(row.FillSize); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	if out.BuyPrice, err = bgcommon.ParseDecimalOrZero(row.BuyPrice); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	if out.SellPrice, err = bgcommon.ParseDecimalOrZero(row.SellPrice); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	if out.BuyFee, err = bgcommon.ParseDecimalOrZero(row.BuyFee); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	if out.SellFee, err = bgcommon.ParseDecimalOrZero(row.SellFee); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	out.BuyTimeMs, _ = bgcommon.ParseInt64OrZero(row.BuyTime)
	out.SellTimeMs, _ = bgcommon.ParseInt64OrZero(row.SellTime)
	if out.AchievedPL, err = bgcommon.ParseDecimalOrZero(row.AchievedPL); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	if out.AchievedPLR, err = bgcommon.ParseDecimalOrZero(row.AchievedPLR); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	if out.NetProfit, err = bgcommon.ParseDecimalOrZero(row.NetProfit); err != nil {
		return copytypes.SpotTraderHistoryOrder{}, err
	}
	out.FollowCount, _ = bgcommon.ParseInt64OrZero(row.FollowCount)
	return out, nil
}

// ---------------------------------------------------------------------
// GetOrderSummary — order-total-detail.
// ---------------------------------------------------------------------

type spotTraderSummaryRow struct {
	TotalFollowerNum    string           `json:"totalFollowerNum"`
	CurrentFollowerNum  string           `json:"currentFollowerNum"`
	MaxFollowerNum      string           `json:"maxFollowerNum"`
	TradingOrderNum     string           `json:"tradingOrderNum"`
	TotalPL             string           `json:"totalpl"`
	GainNum             string           `json:"gainNum"`
	LossNum             string           `json:"lossNum"`
	TotalEquity         string           `json:"totalEquity"`
	WinRate             string           `json:"winRate"`
	LastWeekRoiList     []ratePointRow   `json:"lastWeekRoiList"`
	LastMonthRoiList    []ratePointRow   `json:"lastMonthRoiList"`
	LastWeekProfitList  []profitPointRow `json:"lastWeekProfitList"`
	LastMonthProfitList []profitPointRow `json:"lastMonthProfitList"`
}

// GetOrderSummary returns the spot lead trader's headline statistics.
// Takes no parameters.
func (s *SpotTraderClient) GetOrderSummary(ctx context.Context) (copytypes.SpotTraderSummary, error) {
	var out copytypes.SpotTraderSummary

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/spot-trader/order-total-detail",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row spotTraderSummaryRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}

	out.TotalPL = row.TotalPL
	out.TotalFollowerNum, _ = bgcommon.ParseInt64OrZero(row.TotalFollowerNum)
	out.CurrentFollowerNum, _ = bgcommon.ParseInt64OrZero(row.CurrentFollowerNum)
	out.MaxFollowerNum, _ = bgcommon.ParseInt64OrZero(row.MaxFollowerNum)
	out.TradingOrderNum, _ = bgcommon.ParseInt64OrZero(row.TradingOrderNum)
	out.GainNum, _ = bgcommon.ParseInt64OrZero(row.GainNum)
	out.LossNum, _ = bgcommon.ParseInt64OrZero(row.LossNum)
	if out.TotalEquity, err = bgcommon.ParseDecimalOrZero(row.TotalEquity); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}
	if out.WinRate, err = bgcommon.ParseDecimalOrZero(row.WinRate); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}
	if out.LastWeekROI, err = convertRatePoints(row.LastWeekRoiList); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}
	if out.LastMonthROI, err = convertRatePoints(row.LastMonthRoiList); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}
	if out.LastWeekProfit, err = convertProfitPoints(row.LastWeekProfitList); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}
	if out.LastMonthProfit, err = convertProfitPoints(row.LastMonthProfitList); err != nil {
		return out, errParse("SpotTrader.GetOrderSummary", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// ModifyTPSL — order-modify-tpsl.
// ---------------------------------------------------------------------

type spotTraderModifyTPSLBody struct {
	TrackingNo       string `json:"trackingNo"`
	StopSurplusPrice string `json:"stopSurplusPrice,omitempty"`
	StopLossPrice    string `json:"stopLossPrice,omitempty"`
}

// ModifyTPSL sets / updates / cancels the TP/SL on a spot tracking
// order. TrackingNo is required and at least one price must be set.
// Price semantics: empty = leave unchanged, "0" = cancel, > 0 = set.
func (s *SpotTraderClient) ModifyTPSL(ctx context.Context, req copytypes.SpotTraderModifyTPSLRequest) error {
	if req.TrackingNo == "" {
		return errInvalid("SpotTrader.ModifyTPSL", "trackingNo is empty")
	}
	if req.StopSurplusPrice == "" && req.StopLossPrice == "" {
		return errInvalid("SpotTrader.ModifyTPSL", "one of stopSurplusPrice / stopLossPrice is required")
	}
	var _, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-trader/order-modify-tpsl",
		Body: spotTraderModifyTPSLBody{
			TrackingNo:       req.TrackingNo,
			StopSurplusPrice: req.StopSurplusPrice,
			StopLossPrice:    req.StopLossPrice,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// ClosePositions — order-close-tracking (sell).
// ---------------------------------------------------------------------

type spotCloseTrackingBody struct {
	Symbol         string   `json:"symbol"`
	TrackingNoList []string `json:"trackingNoList"`
}

// ClosePositions sells (closes) the given spot tracking orders. Both
// symbol and a non-empty trackingNos list are required; all of the
// trackingNos must belong to `symbol`. Max 50 per call (all-or-nothing).
func (s *SpotTraderClient) ClosePositions(ctx context.Context, symbol string, trackingNos []string) error {
	if symbol == "" {
		return errInvalid("SpotTrader.ClosePositions", "symbol is empty")
	}
	if len(trackingNos) == 0 {
		return errInvalid("SpotTrader.ClosePositions", "trackingNos is empty")
	}
	if len(trackingNos) > 50 {
		return errInvalid("SpotTrader.ClosePositions", "trackingNos exceeds 50")
	}
	var _, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-trader/order-close-tracking",
		Body:   spotCloseTrackingBody{Symbol: symbol, TrackingNoList: trackingNos},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// GetConfig — config-query-settings.
// ---------------------------------------------------------------------

type spotQuoteInfoRow struct {
	MaxQuoteSize     string `json:"maxQuoteSize"`
	SurplusQuoteSize string `json:"surplusQuoteSize"`
	Symbol           string `json:"symbol"`
}

type spotLabelRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type spotTraceSymbolRow struct {
	Enable       string `json:"enable"`
	Symbol       string `json:"symbol"`
	MinOpenCount string `json:"minOpenCount"`
}

type spotTraderConfigEnvelope struct {
	RemoveLimitUsdt string               `json:"removeLimitUsdt"`
	SpotInfoList    []spotQuoteInfoRow   `json:"spotInfoList"`
	LabelList       []spotLabelRow       `json:"labelList"`
	Enable          string               `json:"enable"`
	ShowAssetsMap   string               `json:"showAssetsMap"`
	ShowEquity      string               `json:"showEquity"`
	TraceSymbolList []spotTraceSymbolRow `json:"traceSymbolList"`
}

// GetConfig returns the spot lead trader's global configuration: the
// per-symbol quote caps, labels, public-display switches, and the
// trade-able symbol list.
func (s *SpotTraderClient) GetConfig(ctx context.Context) (copytypes.SpotTraderConfig, error) {
	var out copytypes.SpotTraderConfig

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/spot-trader/config-query-settings",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env spotTraderConfigEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("SpotTrader.GetConfig", err)
	}

	out.Enable = env.Enable
	out.ShowAssetsMap = env.ShowAssetsMap
	out.ShowEquity = env.ShowEquity
	if out.RemoveLimitUsdt, err = bgcommon.ParseDecimalOrZero(env.RemoveLimitUsdt); err != nil {
		return out, errParse("SpotTrader.GetConfig", err)
	}

	out.SpotInfoList = make([]copytypes.SpotQuoteInfo, 0, len(env.SpotInfoList))
	var i int
	for i = 0; i < len(env.SpotInfoList); i++ {
		var q copytypes.SpotQuoteInfo = copytypes.SpotQuoteInfo{Symbol: env.SpotInfoList[i].Symbol}
		if q.MaxQuoteSize, err = bgcommon.ParseDecimalOrZero(env.SpotInfoList[i].MaxQuoteSize); err != nil {
			return out, errParse("SpotTrader.GetConfig", err)
		}
		if q.SurplusQuoteSize, err = bgcommon.ParseDecimalOrZero(env.SpotInfoList[i].SurplusQuoteSize); err != nil {
			return out, errParse("SpotTrader.GetConfig", err)
		}
		out.SpotInfoList = append(out.SpotInfoList, q)
	}

	out.Labels = make([]copytypes.SpotLabel, 0, len(env.LabelList))
	for i = 0; i < len(env.LabelList); i++ {
		out.Labels = append(out.Labels, copytypes.SpotLabel{ID: env.LabelList[i].ID, Name: env.LabelList[i].Name})
	}

	out.TraceSymbols = make([]copytypes.SpotTraceSymbol, 0, len(env.TraceSymbolList))
	for i = 0; i < len(env.TraceSymbolList); i++ {
		var ts copytypes.SpotTraceSymbol = copytypes.SpotTraceSymbol{
			Symbol: env.TraceSymbolList[i].Symbol,
			Enable: env.TraceSymbolList[i].Enable,
		}
		if ts.MinOpenCount, err = bgcommon.ParseDecimalOrZero(env.TraceSymbolList[i].MinOpenCount); err != nil {
			return out, errParse("SpotTrader.GetConfig", err)
		}
		out.TraceSymbols = append(out.TraceSymbols, ts)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// SetSymbols — config-setting-symbols.
// ---------------------------------------------------------------------

type spotSettingSymbolsBody struct {
	SymbolList  []string `json:"symbolList"`
	SettingType string   `json:"settingType"`
}

// SetSymbols adds or deletes the spot symbols the lead trader broadcasts
// (max 50). settingType is "add" / "delete".
func (s *SpotTraderClient) SetSymbols(ctx context.Context, symbols []string, settingType string) error {
	if len(symbols) == 0 {
		return errInvalid("SpotTrader.SetSymbols", "symbols is empty")
	}
	if len(symbols) > 50 {
		return errInvalid("SpotTrader.SetSymbols", "symbols exceeds 50")
	}
	if settingType == "" {
		return errInvalid("SpotTrader.SetSymbols", "settingType is empty")
	}
	var _, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-trader/config-setting-symbols",
		Body:   spotSettingSymbolsBody{SymbolList: symbols, SettingType: settingType},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// GetFollowers — config-query-followers.
// ---------------------------------------------------------------------

// GetFollowers returns the spot lead trader's follower roster. Reuses
// the TraderFollower row shape (shared with futures) and walks
// page-number pagination.
func (s *SpotTraderClient) GetFollowers(ctx context.Context) ([]copytypes.TraderFollower, error) {
	return paginateByPageNo(ctx, followersPageSize, func(pageNo, pageSize int) ([]copytypes.TraderFollower, error) {
		var query url.Values = url.Values{}
		query.Set("pageNo", strconv.Itoa(pageNo))
		query.Set("pageSize", strconv.Itoa(pageSize))

		var resp rest.Response
		var err error
		resp, _, err = s.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/copy/spot-trader/config-query-followers",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}

		var rows []followerRow
		if err = resp.UnmarshalData(&rows); err != nil {
			return nil, errParse("SpotTrader.GetFollowers", err)
		}
		var out []copytypes.TraderFollower = make([]copytypes.TraderFollower, 0, len(rows))
		var i int
		for i = 0; i < len(rows); i++ {
			var f copytypes.TraderFollower = copytypes.TraderFollower{
				FollowerUID:     rows[i].FollowerUID,
				FollowerName:    rows[i].FollowerName,
				FollowerHeadPic: rows[i].FollowerHeadPic,
				IsRemove:        rows[i].IsRemove,
			}
			if f.AccountEquity, err = bgcommon.ParseDecimalOrZero(rows[i].AccountEquity); err != nil {
				return nil, errParse("SpotTrader.GetFollowers", err)
			}
			f.FollowerTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].FollowerTime)
			out = append(out, f)
		}
		return out, nil
	})
}

// ---------------------------------------------------------------------
// RemoveFollower — config-remove-follower.
// ---------------------------------------------------------------------

// RemoveFollower removes the given follower. followerUID is required.
func (s *SpotTraderClient) RemoveFollower(ctx context.Context, followerUID string) error {
	if followerUID == "" {
		return errInvalid("SpotTrader.RemoveFollower", "followerUID is empty")
	}
	var _, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-trader/config-remove-follower",
		Body:   removeFollowerBody{FollowerUID: followerUID},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// GetProfitSummary — profit-summarys.
// ---------------------------------------------------------------------

type spotByDateRow struct {
	Profit     string `json:"profit"`
	ProfitTime string `json:"profitTime"`
}

type spotProfitHistoryCoinRow struct {
	Coin               string          `json:"coin"`
	ProfitCount        string          `json:"profitCount"`
	LastProfitTime     string          `json:"lastProfitTime"`
	HistorysByDateList []spotByDateRow `json:"historysByDateList"`
}

type spotProfitSummaryObj struct {
	YesterdayProfit string `json:"yesterdayProfit"`
	YesterdayTime   string `json:"yesterdayTime"`
	SumProfit       string `json:"sumProfit"`
	WaitProfit      string `json:"waitProfit"`
}

type spotProfitSummaryEnvelope struct {
	ProfitSummarys    spotProfitSummaryObj       `json:"profitSummarys"`
	ProfitHistoryList []spotProfitHistoryCoinRow `json:"profitHistoryList"`
}

// GetProfitSummary returns the spot lead trader's profit-share summary
// plus per-currency rollups with a by-date breakdown. Takes no params.
func (s *SpotTraderClient) GetProfitSummary(ctx context.Context) (copytypes.SpotProfitSummary, error) {
	var out copytypes.SpotProfitSummary

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/spot-trader/profit-summarys",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env spotProfitSummaryEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("SpotTrader.GetProfitSummary", err)
	}

	if out.YesterdayProfit, err = bgcommon.ParseDecimalOrZero(env.ProfitSummarys.YesterdayProfit); err != nil {
		return out, errParse("SpotTrader.GetProfitSummary", err)
	}
	if out.SumProfit, err = bgcommon.ParseDecimalOrZero(env.ProfitSummarys.SumProfit); err != nil {
		return out, errParse("SpotTrader.GetProfitSummary", err)
	}
	if out.WaitProfit, err = bgcommon.ParseDecimalOrZero(env.ProfitSummarys.WaitProfit); err != nil {
		return out, errParse("SpotTrader.GetProfitSummary", err)
	}
	out.YesterdayTimeMs, _ = bgcommon.ParseInt64OrZero(env.ProfitSummarys.YesterdayTime)

	out.History = make([]copytypes.SpotProfitHistoryCoin, 0, len(env.ProfitHistoryList))
	var i int
	for i = 0; i < len(env.ProfitHistoryList); i++ {
		var h copytypes.SpotProfitHistoryCoin = copytypes.SpotProfitHistoryCoin{Coin: env.ProfitHistoryList[i].Coin}
		if h.ProfitCount, err = bgcommon.ParseDecimalOrZero(env.ProfitHistoryList[i].ProfitCount); err != nil {
			return out, errParse("SpotTrader.GetProfitSummary", err)
		}
		h.LastProfitTimeMs, _ = bgcommon.ParseInt64OrZero(env.ProfitHistoryList[i].LastProfitTime)
		var j int
		h.ByDate = make([]copytypes.ProfitPoint, 0, len(env.ProfitHistoryList[i].HistorysByDateList))
		for j = 0; j < len(env.ProfitHistoryList[i].HistorysByDateList); j++ {
			var p copytypes.ProfitPoint
			if p.Amount, err = bgcommon.ParseDecimalOrZero(env.ProfitHistoryList[i].HistorysByDateList[j].Profit); err != nil {
				return out, errParse("SpotTrader.GetProfitSummary", err)
			}
			p.TimeMs, _ = bgcommon.ParseInt64OrZero(env.ProfitHistoryList[i].HistorysByDateList[j].ProfitTime)
			h.ByDate = append(h.ByDate, p)
		}
		out.History = append(out.History, h)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetProfitShareHistory — profit-history-details.
// ---------------------------------------------------------------------

type spotProfitShareRow struct {
	ProfitID        string `json:"profitId"`
	Coin            string `json:"coin"`
	DistributeRatio string `json:"distributeRatio"`
	Profit          string `json:"profit"`
	FollowerName    string `json:"followerName"`
	ProfitTime      string `json:"profitTime"`
}

type spotProfitShareEnvelope struct {
	ProfitList []spotProfitShareRow `json:"profitList"`
	EndID      string               `json:"endId"`
}

// GetProfitShareHistory returns distributed profit-share events,
// optionally filtered by settlement `coin` and the [startTimeMs,
// endTimeMs] window. Walks the idLessThan / endId cursor.
func (s *SpotTraderClient) GetProfitShareHistory(ctx context.Context, coin string, startTimeMs, endTimeMs int64) ([]copytypes.SpotProfitShareRecord, error) {
	var rows []spotProfitShareRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.SpotTrader.GetProfitShareHistory",
		func(idLessThan string, limit int) ([]spotProfitShareRow, string, error) {
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
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/spot-trader/profit-history-details",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env spotProfitShareEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SpotTrader.GetProfitShareHistory", ferr)
			}
			return env.ProfitList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.SpotProfitShareRecord = make([]copytypes.SpotProfitShareRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec copytypes.SpotProfitShareRecord = copytypes.SpotProfitShareRecord{
			ProfitID:     rows[i].ProfitID,
			Coin:         rows[i].Coin,
			FollowerName: rows[i].FollowerName,
		}
		if rec.DistributeRatio, err = bgcommon.ParseDecimalOrZero(rows[i].DistributeRatio); err != nil {
			return nil, errParse("SpotTrader.GetProfitShareHistory", err)
		}
		if rec.Profit, err = bgcommon.ParseDecimalOrZero(rows[i].Profit); err != nil {
			return nil, errParse("SpotTrader.GetProfitShareHistory", err)
		}
		rec.ProfitTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].ProfitTime)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetPendingProfitShare — profit-details.
// ---------------------------------------------------------------------

type spotPendingProfitRow struct {
	DistributeRatio string `json:"distributeRatio"`
	Coin            string `json:"coin"`
	Profit          string `json:"profit"`
	FollowerName    string `json:"followerName"`
}

// GetPendingProfitShare returns the unrealized / to-be-distributed
// profit shares, optionally filtered by settlement `coin`. Uses
// page-number pagination (pageSize capped at 50 per the venue).
func (s *SpotTraderClient) GetPendingProfitShare(ctx context.Context, coin string) ([]copytypes.SpotPendingProfitShare, error) {
	return paginateByPageNo(ctx, 50, func(pageNo, pageSize int) ([]copytypes.SpotPendingProfitShare, error) {
		var query url.Values = url.Values{}
		query.Set("pageNo", strconv.Itoa(pageNo))
		query.Set("pageSize", strconv.Itoa(pageSize))
		if coin != "" {
			query.Set("coin", coin)
		}

		var resp rest.Response
		var err error
		resp, _, err = s.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/copy/spot-trader/profit-details",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}

		var rows []spotPendingProfitRow
		if err = resp.UnmarshalData(&rows); err != nil {
			return nil, errParse("SpotTrader.GetPendingProfitShare", err)
		}
		var out []copytypes.SpotPendingProfitShare = make([]copytypes.SpotPendingProfitShare, 0, len(rows))
		var i int
		for i = 0; i < len(rows); i++ {
			var p copytypes.SpotPendingProfitShare = copytypes.SpotPendingProfitShare{
				Coin:         rows[i].Coin,
				FollowerName: rows[i].FollowerName,
			}
			if p.DistributeRatio, err = bgcommon.ParseDecimalOrZero(rows[i].DistributeRatio); err != nil {
				return nil, errParse("SpotTrader.GetPendingProfitShare", err)
			}
			if p.Profit, err = bgcommon.ParseDecimalOrZero(rows[i].Profit); err != nil {
				return nil, errParse("SpotTrader.GetPendingProfitShare", err)
			}
			out = append(out, p)
		}
		return out, nil
	})
}
