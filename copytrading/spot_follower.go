/*
FILE: copytrading/spot_follower.go

DESCRIPTION:
Spot FOLLOWER sub-client — the copier side of spot copy trading
(/api/v2/copy/spot-follower/...). A spot follower mirrors a lead
trader's spot buy/sell tracking orders.

WIRED IN M4b:

	GET  query-traders           — GetMyTraders (page-no paged)
	GET  query-trader-symbols    — GetTraderSymbols
	GET  query-settings          — GetSettings
	POST settings                — UpdateSettings
	POST setting-tpsl            — SetTPSL
	GET  query-current-orders    — GetCurrentOrders (paged)
	GET  query-history-orders    — GetHistoryOrders (paged)
	POST order-close-tracking    — ClosePositions (sell)
	POST stop-order              — StopOrders
	POST cancel-trader           — Unfollow

NO PRODUCT TYPE: spot copy trading is spot; none of these calls send
productType (the pinned futures product type is ignored here).
*/

package copytrading

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	copytypes "github.com/tonymontanov/go-bitget/v2/copytrading/types"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// SpotFollowerClient — spot follower sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type SpotFollowerClient struct {
	c *Client
}

func newSpotFollowerClient(c *Client) *SpotFollowerClient {
	return &SpotFollowerClient{c: c}
}

// ---------------------------------------------------------------------
// GetMyTraders — query-traders.
// ---------------------------------------------------------------------

type spotMyTraderRow struct {
	CertificationType    string `json:"certificationType"`
	TraceTotalAmount     string `json:"traceTotalAmount"`
	TraceTotalNetProfit  string `json:"traceTotalNetProfit"`
	TraceTotalProfit     string `json:"traceTotalProfit"`
	TraderName           string `json:"traderName"`
	TraderID             string `json:"traderId"`
	MaxFollowLimit       string `json:"maxFollowLimit"`
	PlatskMaxFollowLimit string `json:"platskMaxFollowLimit"`
	FollowCount          string `json:"followCount"`
	PlatskFollowCount    string `json:"platskFollowCount"`
	FollowerTime         string `json:"followerTime"`
}

type spotMyTradersEnvelope struct {
	ResultList []spotMyTraderRow `json:"resultList"`
}

// GetMyTraders returns the spot follower's "my traders" directory.
// Walks page-number pagination (pageSize capped at 50 per the venue).
func (f *SpotFollowerClient) GetMyTraders(ctx context.Context) ([]copytypes.SpotMyTrader, error) {
	return paginateByPageNo(ctx, 50, func(pageNo, pageSize int) ([]copytypes.SpotMyTrader, error) {
		var query url.Values = url.Values{}
		query.Set("pageNo", strconv.Itoa(pageNo))
		query.Set("pageSize", strconv.Itoa(pageSize))

		var resp rest.Response
		var err error
		resp, _, err = f.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/copy/spot-follower/query-traders",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}

		var env spotMyTradersEnvelope
		if err = resp.UnmarshalData(&env); err != nil {
			return nil, errParse("SpotFollower.GetMyTraders", err)
		}
		var out []copytypes.SpotMyTrader = make([]copytypes.SpotMyTrader, 0, len(env.ResultList))
		var i int
		for i = 0; i < len(env.ResultList); i++ {
			var tr copytypes.SpotMyTrader
			tr, err = convertSpotMyTraderRow(env.ResultList[i])
			if err != nil {
				return nil, errParse("SpotFollower.GetMyTraders", err)
			}
			out = append(out, tr)
		}
		return out, nil
	})
}

func convertSpotMyTraderRow(row spotMyTraderRow) (copytypes.SpotMyTrader, error) {
	var out copytypes.SpotMyTrader = copytypes.SpotMyTrader{
		TraderID:          row.TraderID,
		TraderName:        row.TraderName,
		CertificationType: row.CertificationType,
	}
	var err error
	if out.TraceTotalAmount, err = bgcommon.ParseDecimalOrZero(row.TraceTotalAmount); err != nil {
		return copytypes.SpotMyTrader{}, err
	}
	if out.TraceTotalNetProfit, err = bgcommon.ParseDecimalOrZero(row.TraceTotalNetProfit); err != nil {
		return copytypes.SpotMyTrader{}, err
	}
	if out.TraceTotalProfit, err = bgcommon.ParseDecimalOrZero(row.TraceTotalProfit); err != nil {
		return copytypes.SpotMyTrader{}, err
	}
	out.MaxFollowLimit, _ = bgcommon.ParseInt64OrZero(row.MaxFollowLimit)
	out.FollowCount, _ = bgcommon.ParseInt64OrZero(row.FollowCount)
	out.PlatskMaxFollowLimit, _ = bgcommon.ParseInt64OrZero(row.PlatskMaxFollowLimit)
	out.PlatskFollowCount, _ = bgcommon.ParseInt64OrZero(row.PlatskFollowCount)
	out.FollowerTimeMs, _ = bgcommon.ParseInt64OrZero(row.FollowerTime)
	return out, nil
}

// ---------------------------------------------------------------------
// GetTraderSymbols — query-trader-symbols.
// ---------------------------------------------------------------------

type spotTraderSymbolsEnvelope struct {
	CurrentTradingList []string `json:"currentTradingList"`
}

// GetTraderSymbols returns the symbols the given trader is currently
// copy-trading. traderID is required.
func (f *SpotFollowerClient) GetTraderSymbols(ctx context.Context, traderID string) ([]string, error) {
	if traderID == "" {
		return nil, errInvalid("SpotFollower.GetTraderSymbols", "traderID is empty")
	}
	var query url.Values = url.Values{}
	query.Set("traderId", traderID)

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/spot-follower/query-trader-symbols",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var env spotTraderSymbolsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, errParse("SpotFollower.GetTraderSymbols", err)
	}
	return env.CurrentTradingList, nil
}

// ---------------------------------------------------------------------
// GetSettings — query-settings.
// ---------------------------------------------------------------------

type spotFollowSettingRow struct {
	MaxTraceAmount    string `json:"maxTraceAmount"`
	StopLossRation    string `json:"stopLossRation"`
	StopSurplusRation string `json:"stopSurplusRation"`
	Symbol            string `json:"symbol"`
	TraceType         string `json:"traceType"`
}

type spotFollowSymbolBoundsRow struct {
	MaxStopLossRation         string `json:"maxStopLossRation"`
	MaxStopSurplusRation      string `json:"maxStopSurplusRation"`
	MaxTraceAmount            string `json:"maxTraceAmount"`
	MaxTraceAmountSystem      string `json:"maxTraceAmountSystem"`
	MaxTraceSize              string `json:"maxTraceSize"`
	MaxTraceRation            string `json:"maxTraceRation"`
	MinStopLossRation         string `json:"minStopLossRation"`
	MinStopSurplusRation      string `json:"minStopSurplusRation"`
	MinTraceAmount            string `json:"minTraceAmount"`
	MinTraceSize              string `json:"minTraceSize"`
	MinTraceRation            string `json:"minTraceRation"`
	SliderMaxStopLossRatio    string `json:"sliderMaxStopLossRatio"`
	SliderMaxStopSurplusRatio string `json:"sliderMaxStopSurplusRatio"`
	Symbol                    string `json:"symbol"`
}

type spotFollowSettingsEnvelope struct {
	Enable                 string                      `json:"enable"`
	ProfitRate             string                      `json:"profitRate"`
	SettledInDays          string                      `json:"settledInDays"`
	TraderHeadPic          string                      `json:"traderHeadPic"`
	TraderName             string                      `json:"traderName"`
	TradeSettingList       []spotFollowSettingRow      `json:"tradeSettingList"`
	TradeSymbolSettingList []spotFollowSymbolBoundsRow `json:"tradeSymbolSettingList"`
}

// GetSettings returns the follower's active configuration for one lead
// trader, plus the venue min/max bounds per symbol. traderID is required.
func (f *SpotFollowerClient) GetSettings(ctx context.Context, traderID string) (copytypes.SpotFollowSettings, error) {
	var out copytypes.SpotFollowSettings
	if traderID == "" {
		return out, errInvalid("SpotFollower.GetSettings", "traderID is empty")
	}
	var query url.Values = url.Values{}
	query.Set("traderId", traderID)

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/spot-follower/query-settings",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env spotFollowSettingsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("SpotFollower.GetSettings", err)
	}

	out.Following = env.Enable == "YES"
	out.TraderName = env.TraderName
	out.TraderHeadPic = env.TraderHeadPic
	if out.ProfitRate, err = bgcommon.ParseDecimalOrZero(env.ProfitRate); err != nil {
		return out, errParse("SpotFollower.GetSettings", err)
	}
	out.SettledInDays, _ = bgcommon.ParseInt64OrZero(env.SettledInDays)

	out.TradeSettings = make([]copytypes.SpotFollowSettingDetail, 0, len(env.TradeSettingList))
	var i int
	for i = 0; i < len(env.TradeSettingList); i++ {
		var d copytypes.SpotFollowSettingDetail = copytypes.SpotFollowSettingDetail{
			Symbol:    env.TradeSettingList[i].Symbol,
			TraceType: env.TradeSettingList[i].TraceType,
		}
		if d.MaxTraceAmount, err = bgcommon.ParseDecimalOrZero(env.TradeSettingList[i].MaxTraceAmount); err != nil {
			return out, errParse("SpotFollower.GetSettings", err)
		}
		if d.StopLossRation, err = bgcommon.ParseDecimalOrZero(env.TradeSettingList[i].StopLossRation); err != nil {
			return out, errParse("SpotFollower.GetSettings", err)
		}
		if d.StopSurplusRation, err = bgcommon.ParseDecimalOrZero(env.TradeSettingList[i].StopSurplusRation); err != nil {
			return out, errParse("SpotFollower.GetSettings", err)
		}
		out.TradeSettings = append(out.TradeSettings, d)
	}

	out.TradeSymbolSettings = make([]copytypes.SpotFollowSymbolBounds, 0, len(env.TradeSymbolSettingList))
	for i = 0; i < len(env.TradeSymbolSettingList); i++ {
		var b copytypes.SpotFollowSymbolBounds
		b, err = convertSpotSymbolBoundsRow(env.TradeSymbolSettingList[i])
		if err != nil {
			return out, errParse("SpotFollower.GetSettings", err)
		}
		out.TradeSymbolSettings = append(out.TradeSymbolSettings, b)
	}
	return out, nil
}

func convertSpotSymbolBoundsRow(row spotFollowSymbolBoundsRow) (copytypes.SpotFollowSymbolBounds, error) {
	var out copytypes.SpotFollowSymbolBounds = copytypes.SpotFollowSymbolBounds{Symbol: row.Symbol}
	var err error
	var fields = []struct {
		dst *decimal.Decimal
		src string
	}{
		{&out.MaxTraceAmount, row.MaxTraceAmount},
		{&out.MaxTraceAmountSystem, row.MaxTraceAmountSystem},
		{&out.MaxTraceSize, row.MaxTraceSize},
		{&out.MaxTraceRation, row.MaxTraceRation},
		{&out.MinTraceAmount, row.MinTraceAmount},
		{&out.MinTraceSize, row.MinTraceSize},
		{&out.MinTraceRation, row.MinTraceRation},
		{&out.MaxStopLossRation, row.MaxStopLossRation},
		{&out.MaxStopSurplusRation, row.MaxStopSurplusRation},
		{&out.MinStopLossRation, row.MinStopLossRation},
		{&out.MinStopSurplusRation, row.MinStopSurplusRation},
		{&out.SliderMaxStopLossRatio, row.SliderMaxStopLossRatio},
		{&out.SliderMaxStopSurplusRatio, row.SliderMaxStopSurplusRatio},
	}
	var i int
	for i = 0; i < len(fields); i++ {
		if *fields[i].dst, err = bgcommon.ParseDecimalOrZero(fields[i].src); err != nil {
			return copytypes.SpotFollowSymbolBounds{}, err
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------
// UpdateSettings — settings.
// ---------------------------------------------------------------------

type spotFollowSettingBody struct {
	Symbol           string `json:"symbol"`
	TraceType        string `json:"traceType"`
	MaxHoldSize      string `json:"maxHoldSize"`
	TraceValue       string `json:"traceValue"`
	StopSurplusRatio string `json:"stopSurplusRatio,omitempty"`
	StopLossRatio    string `json:"stopLossRatio,omitempty"`
}

type spotFollowSettingsBody struct {
	TraderID string                  `json:"traderId"`
	AutoCopy string                  `json:"autoCopy,omitempty"`
	Mode     string                  `json:"mode,omitempty"`
	Settings []spotFollowSettingBody `json:"settings"`
}

// UpdateSettings sets / modifies the follower's copy-trade configuration
// for one lead trader. traderID and a non-empty settings list are
// required; each row must carry symbol / traceType / maxHoldSize /
// traceValue.
func (f *SpotFollowerClient) UpdateSettings(ctx context.Context, req copytypes.SpotFollowSettingsRequest) error {
	if req.TraderID == "" {
		return errInvalid("SpotFollower.UpdateSettings", "traderID is empty")
	}
	if len(req.Settings) == 0 {
		return errInvalid("SpotFollower.UpdateSettings", "settings is empty")
	}

	var rows []spotFollowSettingBody = make([]spotFollowSettingBody, 0, len(req.Settings))
	var i int
	for i = 0; i < len(req.Settings); i++ {
		var s copytypes.SpotFollowSetting = req.Settings[i]
		if s.Symbol == "" {
			return errInvalid("SpotFollower.UpdateSettings", "settings: symbol is empty")
		}
		if s.TraceType == "" {
			return errInvalid("SpotFollower.UpdateSettings", "settings: traceType is empty")
		}
		if s.MaxHoldSize == "" {
			return errInvalid("SpotFollower.UpdateSettings", "settings: maxHoldSize is empty")
		}
		if s.TraceValue == "" {
			return errInvalid("SpotFollower.UpdateSettings", "settings: traceValue is empty")
		}
		rows = append(rows, spotFollowSettingBody{
			Symbol:           s.Symbol,
			TraceType:        s.TraceType,
			MaxHoldSize:      s.MaxHoldSize,
			TraceValue:       s.TraceValue,
			StopSurplusRatio: s.StopSurplusRatio,
			StopLossRatio:    s.StopLossRatio,
		})
	}

	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-follower/settings",
		Body: spotFollowSettingsBody{
			TraderID: req.TraderID,
			AutoCopy: req.AutoCopy,
			Mode:     req.Mode,
			Settings: rows,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// SetTPSL — setting-tpsl.
// ---------------------------------------------------------------------

type spotFollowTPSLBody struct {
	TrackingNo       string `json:"trackingNo"`
	StopSurplusPrice string `json:"stopSurplusPrice,omitempty"`
	StopLossPrice    string `json:"stopLossPrice,omitempty"`
}

// SetTPSL sets / updates / cancels the TP/SL on a copied spot order.
// trackingNo is required and at least one price must be set. Price
// semantics: empty = leave unchanged, "0" = cancel, > 0 = set.
func (f *SpotFollowerClient) SetTPSL(ctx context.Context, req copytypes.SpotFollowTPSLRequest) error {
	if req.TrackingNo == "" {
		return errInvalid("SpotFollower.SetTPSL", "trackingNo is empty")
	}
	if req.StopSurplusPrice == "" && req.StopLossPrice == "" {
		return errInvalid("SpotFollower.SetTPSL", "one of stopSurplusPrice / stopLossPrice is required")
	}
	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-follower/setting-tpsl",
		Body: spotFollowTPSLBody{
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
// GetCurrentOrders — query-current-orders.
// ---------------------------------------------------------------------

type spotFollowerCurrentRow struct {
	TrackingNo       string `json:"trackingNo"`
	TraderID         string `json:"traderId"`
	BuyFillSize      string `json:"buyFillSize"`
	BuyDelegateSize  string `json:"buyDelegateSize"`
	BuyPrice         string `json:"buyPrice"`
	UnrealizedPL     string `json:"unrealizedPL"`
	BuyTime          string `json:"buyTime"`
	BuyFee           string `json:"buyFee"`
	UnrealizedPLR    string `json:"unrealizedPLR"`
	Symbol           string `json:"symbol"`
	StopSurplusPrice string `json:"stopSurplusPrice"`
	StopLossPrice    string `json:"stopLossPrice"`
}

type spotFollowerCurrentEnvelope struct {
	TrackingList []spotFollowerCurrentRow `json:"trackingList"`
	EndID        string                   `json:"endId"`
}

// GetCurrentOrders returns the follower's live copied spot orders.
// `symbol` and `traderID` are optional filters. Walks the
// idLessThan / endId cursor.
func (f *SpotFollowerClient) GetCurrentOrders(ctx context.Context, symbol, traderID string) ([]copytypes.SpotFollowerCurrentOrder, error) {
	var rows []spotFollowerCurrentRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.SpotFollower.GetCurrentOrders",
		func(idLessThan string, limit int) ([]spotFollowerCurrentRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if symbol != "" {
				query.Set("symbol", symbol)
			}
			if traderID != "" {
				query.Set("traderId", traderID)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = f.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/spot-follower/query-current-orders",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env spotFollowerCurrentEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SpotFollower.GetCurrentOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.SpotFollowerCurrentOrder = make([]copytypes.SpotFollowerCurrentOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.SpotFollowerCurrentOrder
		o, err = convertSpotFollowerCurrentRow(rows[i])
		if err != nil {
			return nil, errParse("SpotFollower.GetCurrentOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertSpotFollowerCurrentRow(row spotFollowerCurrentRow) (copytypes.SpotFollowerCurrentOrder, error) {
	var out copytypes.SpotFollowerCurrentOrder = copytypes.SpotFollowerCurrentOrder{
		TrackingNo: row.TrackingNo,
		TraderID:   row.TraderID,
		Symbol:     row.Symbol,
	}
	var err error
	if out.BuyFillSize, err = bgcommon.ParseDecimalOrZero(row.BuyFillSize); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	if out.BuyDelegateSize, err = bgcommon.ParseDecimalOrZero(row.BuyDelegateSize); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	if out.BuyPrice, err = bgcommon.ParseDecimalOrZero(row.BuyPrice); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	if out.BuyFee, err = bgcommon.ParseDecimalOrZero(row.BuyFee); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	out.BuyTimeMs, _ = bgcommon.ParseInt64OrZero(row.BuyTime)
	if out.UnrealizedPL, err = bgcommon.ParseDecimalOrZero(row.UnrealizedPL); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	if out.UnrealizedPLR, err = bgcommon.ParseDecimalOrZero(row.UnrealizedPLR); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	if out.StopSurplusPrice, err = bgcommon.ParseDecimalOrZero(row.StopSurplusPrice); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	if out.StopLossPrice, err = bgcommon.ParseDecimalOrZero(row.StopLossPrice); err != nil {
		return copytypes.SpotFollowerCurrentOrder{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistoryOrders — query-history-orders.
// ---------------------------------------------------------------------

type spotFollowerHistoryRow struct {
	TrackingNo  string `json:"trackingNo"`
	TraderID    string `json:"traderId"`
	FillSize    string `json:"fillSize"`
	BuyPrice    string `json:"buyPrice"`
	SellPrice   string `json:"sellPrice"`
	BuyFee      string `json:"buyFee"`
	SellFee     string `json:"sellFee"`
	AchievedPL  string `json:"achievedPL"`
	AchievedPLR string `json:"achievedPLR"`
	Symbol      string `json:"symbol"`
	BuyTime     string `json:"buyTime"`
	SellTime    string `json:"sellTime"`
}

type spotFollowerHistoryEnvelope struct {
	TrackingList []spotFollowerHistoryRow `json:"trackingList"`
	EndID        string                   `json:"endId"`
}

// GetHistoryOrders returns the follower's closed copied spot orders in
// the optional [startTimeMs, endTimeMs] window. `symbol` and `traderID`
// are optional filters. Walks the idLessThan / endId cursor.
func (f *SpotFollowerClient) GetHistoryOrders(ctx context.Context, symbol, traderID string, startTimeMs, endTimeMs int64) ([]copytypes.SpotFollowerHistoryOrder, error) {
	var rows []spotFollowerHistoryRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.SpotFollower.GetHistoryOrders",
		func(idLessThan string, limit int) ([]spotFollowerHistoryRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if symbol != "" {
				query.Set("symbol", symbol)
			}
			if traderID != "" {
				query.Set("traderId", traderID)
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
			resp, _, ferr = f.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/spot-follower/query-history-orders",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env spotFollowerHistoryEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SpotFollower.GetHistoryOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.SpotFollowerHistoryOrder = make([]copytypes.SpotFollowerHistoryOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.SpotFollowerHistoryOrder
		o, err = convertSpotFollowerHistoryRow(rows[i])
		if err != nil {
			return nil, errParse("SpotFollower.GetHistoryOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertSpotFollowerHistoryRow(row spotFollowerHistoryRow) (copytypes.SpotFollowerHistoryOrder, error) {
	var out copytypes.SpotFollowerHistoryOrder = copytypes.SpotFollowerHistoryOrder{
		TrackingNo: row.TrackingNo,
		TraderID:   row.TraderID,
		Symbol:     row.Symbol,
	}
	var err error
	if out.FillSize, err = bgcommon.ParseDecimalOrZero(row.FillSize); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	if out.BuyPrice, err = bgcommon.ParseDecimalOrZero(row.BuyPrice); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	if out.SellPrice, err = bgcommon.ParseDecimalOrZero(row.SellPrice); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	if out.BuyFee, err = bgcommon.ParseDecimalOrZero(row.BuyFee); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	if out.SellFee, err = bgcommon.ParseDecimalOrZero(row.SellFee); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	out.BuyTimeMs, _ = bgcommon.ParseInt64OrZero(row.BuyTime)
	out.SellTimeMs, _ = bgcommon.ParseInt64OrZero(row.SellTime)
	if out.AchievedPL, err = bgcommon.ParseDecimalOrZero(row.AchievedPL); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	if out.AchievedPLR, err = bgcommon.ParseDecimalOrZero(row.AchievedPLR); err != nil {
		return copytypes.SpotFollowerHistoryOrder{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// ClosePositions — order-close-tracking (sell).
// ---------------------------------------------------------------------

// ClosePositions sells (closes) the given copied spot orders. Both
// symbol and a non-empty trackingNos list are required; all trackingNos
// must belong to `symbol`. Max 50 per call (all-or-nothing).
func (f *SpotFollowerClient) ClosePositions(ctx context.Context, symbol string, trackingNos []string) error {
	if symbol == "" {
		return errInvalid("SpotFollower.ClosePositions", "symbol is empty")
	}
	if len(trackingNos) == 0 {
		return errInvalid("SpotFollower.ClosePositions", "trackingNos is empty")
	}
	if len(trackingNos) > 50 {
		return errInvalid("SpotFollower.ClosePositions", "trackingNos exceeds 50")
	}
	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-follower/order-close-tracking",
		Body:   spotCloseTrackingBody{Symbol: symbol, TrackingNoList: trackingNos},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// StopOrders — stop-order.
// ---------------------------------------------------------------------

type spotStopOrderBody struct {
	TrackingNoList []string `json:"trackingNoList"`
}

// StopOrders stops the given copied orders (atomic; all-or-nothing, max
// 50). Unlike ClosePositions it carries no symbol.
func (f *SpotFollowerClient) StopOrders(ctx context.Context, trackingNos []string) error {
	if len(trackingNos) == 0 {
		return errInvalid("SpotFollower.StopOrders", "trackingNos is empty")
	}
	if len(trackingNos) > 50 {
		return errInvalid("SpotFollower.StopOrders", "trackingNos exceeds 50")
	}
	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-follower/stop-order",
		Body:   spotStopOrderBody{TrackingNoList: trackingNos},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// Unfollow — cancel-trader.
// ---------------------------------------------------------------------

type spotCancelTraderBody struct {
	TraderID string `json:"traderId"`
}

// Unfollow stops following the given lead trader. traderID is required.
func (f *SpotFollowerClient) Unfollow(ctx context.Context, traderID string) error {
	if traderID == "" {
		return errInvalid("SpotFollower.Unfollow", "traderID is empty")
	}
	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/spot-follower/cancel-trader",
		Body:   spotCancelTraderBody{TraderID: traderID},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}
