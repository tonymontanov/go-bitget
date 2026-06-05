/*
FILE: copytrading/futures_follower.go

DESCRIPTION:
Futures FOLLOWER sub-client — the copier side of futures copy trading
(/api/v2/copy/mix-follower/...). A follower mirrors one or more lead
traders' futures positions.

WIRED IN M2:

	GET  mix-follower/query-traders         — GetMyTraders
	GET  mix-follower/query-current-orders  — GetCurrentOrders
	GET  mix-follower/query-history-orders  — GetHistoryOrders (paged)
	POST mix-follower/close-positions       — ClosePositions
	POST mix-follower/cancel-trader         — Unfollow

The configuration surface (settings / query-settings / setting-tpsl /
query-quantity-limit) lives in futures_follower_config.go (M2b).

PRODUCT TYPE:

Every futures follower call carries the pinned productType (USDT /
COIN / USDC-FUTURES). Callers never spell it.
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

// FuturesFollowerClient — futures follower sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type FuturesFollowerClient struct {
	c *Client
}

func newFuturesFollowerClient(c *Client) *FuturesFollowerClient {
	return &FuturesFollowerClient{c: c}
}

// productType returns the wire string for the pinned futures product
// type.
func (f *FuturesFollowerClient) productType() string {
	return string(f.c.productType)
}

// ---------------------------------------------------------------------
// GetMyTraders — query-traders.
// ---------------------------------------------------------------------

type traderRow struct {
	CertificationType      string   `json:"certificationType"`
	TraderID               string   `json:"traderId"`
	TraderName             string   `json:"traderName"`
	MaxFollowLimit         string   `json:"maxFollowLimit"`
	FollowCount            string   `json:"followCount"`
	BGBMaxFollowLimit      string   `json:"bgbMaxFollowLimit"`
	BGBFollowCount         string   `json:"bgbFollowCount"`
	TraceTotalMarginAmount string   `json:"traceTotalMarginAmount"`
	TraceTotalNetProfit    string   `json:"traceTotalNetProfit"`
	TraceTotalProfit       string   `json:"traceTotalProfit"`
	CurrentTradingPairs    []string `json:"currentTradingPairs"`
	FollowerTime           string   `json:"followerTime"`
}

// GetMyTraders returns the lead traders this account currently follows
// on the pinned futures product type.
func (f *FuturesFollowerClient) GetMyTraders(ctx context.Context) ([]copytypes.Trader, error) {
	var query url.Values = url.Values{}
	query.Set("productType", f.productType())
	query.Set("limit", "100")

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-follower/query-traders",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []traderRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("FuturesFollower.GetMyTraders", err)
	}

	var out []copytypes.Trader = make([]copytypes.Trader, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var tr copytypes.Trader
		tr, err = convertTraderRow(rows[i])
		if err != nil {
			return nil, errParse("FuturesFollower.GetMyTraders", err)
		}
		out = append(out, tr)
	}
	return out, nil
}

func convertTraderRow(row traderRow) (copytypes.Trader, error) {
	var out copytypes.Trader = copytypes.Trader{
		TraderID:            row.TraderID,
		TraderName:          row.TraderName,
		CertificationType:   row.CertificationType,
		CurrentTradingPairs: row.CurrentTradingPairs,
	}
	var err error
	if out.MaxFollowLimit, err = bgcommon.ParseInt64OrZero(row.MaxFollowLimit); err != nil {
		return copytypes.Trader{}, err
	}
	if out.FollowCount, err = bgcommon.ParseInt64OrZero(row.FollowCount); err != nil {
		return copytypes.Trader{}, err
	}
	if out.BGBMaxFollowLimit, err = bgcommon.ParseInt64OrZero(row.BGBMaxFollowLimit); err != nil {
		return copytypes.Trader{}, err
	}
	if out.BGBFollowCount, err = bgcommon.ParseInt64OrZero(row.BGBFollowCount); err != nil {
		return copytypes.Trader{}, err
	}
	if out.TraceTotalMarginAmount, err = bgcommon.ParseDecimalOrZero(row.TraceTotalMarginAmount); err != nil {
		return copytypes.Trader{}, err
	}
	if out.TraceTotalNetProfit, err = bgcommon.ParseDecimalOrZero(row.TraceTotalNetProfit); err != nil {
		return copytypes.Trader{}, err
	}
	if out.TraceTotalProfit, err = bgcommon.ParseDecimalOrZero(row.TraceTotalProfit); err != nil {
		return copytypes.Trader{}, err
	}
	out.FollowerTimeMs, _ = bgcommon.ParseInt64OrZero(row.FollowerTime)
	return out, nil
}

// ---------------------------------------------------------------------
// GetCurrentOrders — query-current-orders.
// ---------------------------------------------------------------------

type followerCurrentRow struct {
	TrackingNo   string `json:"trackingNo"`
	TraderID     string `json:"traderId"`
	TraderName   string `json:"traderName"`
	OpenOrderID  string `json:"openOrderId"`
	CloseOrderID string `json:"closeOrderId"`
	Symbol       string `json:"symbol"`
	PosSide      string `json:"posSide"`
	OpenLeverage string `json:"openLeverage"`
	// Bitget has shipped both spellings on this row across versions;
	// prefer openPriceAvg, fall back to openAvgPrice.
	OpenPriceAvg   string `json:"openPriceAvg"`
	OpenAvgPrice   string `json:"openAvgPrice"`
	OpenSize       string `json:"openSize"`
	OpenMarginSize string `json:"openMarginSz"`
	OpenFee        string `json:"openFee"`
	OpenTime       string `json:"openTime"`
	CloseAvgPrice  string `json:"closeAvgPrice"`
	CloseSize      string `json:"closeSize"`
	CloseTime      string `json:"closeTime"`
}

// GetCurrentOrders returns the live copied positions for the pinned
// futures product type. `symbol` and `traderID` are optional filters
// (pass "" to omit). Current copied positions are bounded, so this is a
// single page (up to 100 rows).
func (f *FuturesFollowerClient) GetCurrentOrders(ctx context.Context, symbol, traderID string) ([]copytypes.FollowerCurrentOrder, error) {
	var query url.Values = url.Values{}
	query.Set("productType", f.productType())
	query.Set("limit", "100")
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	if traderID != "" {
		query.Set("traderId", traderID)
	}

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-follower/query-current-orders",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []followerCurrentRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("FuturesFollower.GetCurrentOrders", err)
	}

	var out []copytypes.FollowerCurrentOrder = make([]copytypes.FollowerCurrentOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.FollowerCurrentOrder
		o, err = convertFollowerCurrentRow(rows[i])
		if err != nil {
			return nil, errParse("FuturesFollower.GetCurrentOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertFollowerCurrentRow(row followerCurrentRow) (copytypes.FollowerCurrentOrder, error) {
	var out copytypes.FollowerCurrentOrder = copytypes.FollowerCurrentOrder{
		TrackingNo:   row.TrackingNo,
		TraderID:     row.TraderID,
		TraderName:   row.TraderName,
		OpenOrderID:  row.OpenOrderID,
		CloseOrderID: row.CloseOrderID,
		Symbol:       row.Symbol,
		PosSide:      row.PosSide,
	}
	var openPrice string = row.OpenPriceAvg
	if openPrice == "" {
		openPrice = row.OpenAvgPrice
	}
	var err error
	if out.OpenLeverage, err = bgcommon.ParseDecimalOrZero(row.OpenLeverage); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	if out.OpenPriceAvg, err = bgcommon.ParseDecimalOrZero(openPrice); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	if out.OpenSize, err = bgcommon.ParseDecimalOrZero(row.OpenSize); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	if out.OpenMarginSize, err = bgcommon.ParseDecimalOrZero(row.OpenMarginSize); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	if out.OpenFee, err = bgcommon.ParseDecimalOrZero(row.OpenFee); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	out.OpenTimeMs, _ = bgcommon.ParseInt64OrZero(row.OpenTime)
	if out.CloseAvgPrice, err = bgcommon.ParseDecimalOrZero(row.CloseAvgPrice); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	if out.CloseSize, err = bgcommon.ParseDecimalOrZero(row.CloseSize); err != nil {
		return copytypes.FollowerCurrentOrder{}, err
	}
	out.CloseTimeMs, _ = bgcommon.ParseInt64OrZero(row.CloseTime)
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistoryOrders — query-history-orders.
// ---------------------------------------------------------------------

type followerHistoryRow struct {
	TrackingNo    string `json:"trackingNo"`
	TraderID      string `json:"traderId"`
	OpenOrderID   string `json:"openOrderId"`
	CloseOrderID  string `json:"closeOrderId"`
	ProductType   string `json:"productType"`
	Symbol        string `json:"symbol"`
	PosSide       string `json:"posSide"`
	OpenLeverage  string `json:"openLeverage"`
	OpenPriceAvg  string `json:"openPriceAvg"`
	OpenSize      string `json:"openSize"`
	OpenFee       string `json:"openFee"`
	OpenTime      string `json:"openTime"`
	ClosePriceAvg string `json:"closePriceAvg"`
	CloseSize     string `json:"closeSize"`
	CloseFee      string `json:"closeFee"`
	CloseTime     string `json:"closeTime"`
	ProfitRate    string `json:"profitRate"`
	NetProfit     string `json:"netProfit"`
	AchievedPL    string `json:"achievedPL"`
}

type followerHistoryEnvelope struct {
	TrackingList []followerHistoryRow `json:"trackingList"`
	EndID        string               `json:"endId"`
}

// GetHistoryOrders returns the closed copied positions for the pinned
// futures product type in the optional [startTimeMs, endTimeMs] window.
// Walks the standard idLessThan / endId cursor.
func (f *FuturesFollowerClient) GetHistoryOrders(ctx context.Context, symbol string, startTimeMs, endTimeMs int64) ([]copytypes.FollowerHistoryOrder, error) {
	var rows []followerHistoryRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "copytrading.FuturesFollower.GetHistoryOrders",
		func(idLessThan string, limit int) ([]followerHistoryRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("productType", f.productType())
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
			resp, _, ferr = f.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/copy/mix-follower/query-history-orders",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env followerHistoryEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("FuturesFollower.GetHistoryOrders", ferr)
			}
			return env.TrackingList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []copytypes.FollowerHistoryOrder = make([]copytypes.FollowerHistoryOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var o copytypes.FollowerHistoryOrder
		o, err = convertFollowerHistoryRow(rows[i])
		if err != nil {
			return nil, errParse("FuturesFollower.GetHistoryOrders", err)
		}
		out = append(out, o)
	}
	return out, nil
}

func convertFollowerHistoryRow(row followerHistoryRow) (copytypes.FollowerHistoryOrder, error) {
	var out copytypes.FollowerHistoryOrder = copytypes.FollowerHistoryOrder{
		TrackingNo:   row.TrackingNo,
		TraderID:     row.TraderID,
		OpenOrderID:  row.OpenOrderID,
		CloseOrderID: row.CloseOrderID,
		ProductType:  row.ProductType,
		Symbol:       row.Symbol,
		PosSide:      row.PosSide,
	}
	var err error
	if out.OpenLeverage, err = bgcommon.ParseDecimalOrZero(row.OpenLeverage); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.OpenPriceAvg, err = bgcommon.ParseDecimalOrZero(row.OpenPriceAvg); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.OpenSize, err = bgcommon.ParseDecimalOrZero(row.OpenSize); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.OpenFee, err = bgcommon.ParseDecimalOrZero(row.OpenFee); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	out.OpenTimeMs, _ = bgcommon.ParseInt64OrZero(row.OpenTime)
	if out.ClosePriceAvg, err = bgcommon.ParseDecimalOrZero(row.ClosePriceAvg); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.CloseSize, err = bgcommon.ParseDecimalOrZero(row.CloseSize); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.CloseFee, err = bgcommon.ParseDecimalOrZero(row.CloseFee); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	out.CloseTimeMs, _ = bgcommon.ParseInt64OrZero(row.CloseTime)
	if out.ProfitRate, err = bgcommon.ParseDecimalOrZero(row.ProfitRate); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.NetProfit, err = bgcommon.ParseDecimalOrZero(row.NetProfit); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	if out.AchievedPL, err = bgcommon.ParseDecimalOrZero(row.AchievedPL); err != nil {
		return copytypes.FollowerHistoryOrder{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// ClosePositions — close-positions.
// ---------------------------------------------------------------------

type closePositionsBody struct {
	ProductType string `json:"productType"`
	TrackingNo  string `json:"trackingNo,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	MarginCoin  string `json:"marginCoin,omitempty"`
	MarginMode  string `json:"marginMode,omitempty"`
	HoldSide    string `json:"holdSide,omitempty"`
}

type closeResultResp struct {
	OrderIDList []string `json:"orderIdList"`
}

// ClosePositions closes one, some, or all copied positions for the
// pinned futures product type. An empty request (no TrackingNo / Symbol)
// closes every copied position. Returns the venue order IDs the close
// generated.
func (f *FuturesFollowerClient) ClosePositions(ctx context.Context, req copytypes.CloseFollowerPositionsRequest) (copytypes.CloseResult, error) {
	var out copytypes.CloseResult
	var body closePositionsBody = closePositionsBody{
		ProductType: f.productType(),
		TrackingNo:  req.TrackingNo,
		Symbol:      req.Symbol,
		MarginCoin:  req.MarginCoin,
		MarginMode:  req.MarginMode,
		HoldSide:    req.HoldSide,
	}

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-follower/close-positions",
		Body:   body,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var data closeResultResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, errParse("FuturesFollower.ClosePositions", err)
	}
	out.OrderIDList = data.OrderIDList
	return out, nil
}

// ---------------------------------------------------------------------
// Unfollow — cancel-trader.
// ---------------------------------------------------------------------

type cancelTraderBody struct {
	TraderID string `json:"traderId"`
}

// Unfollow stops copying the given lead trader. traderID is required.
func (f *FuturesFollowerClient) Unfollow(ctx context.Context, traderID string) error {
	if traderID == "" {
		return errInvalid("FuturesFollower.Unfollow", "traderID is empty")
	}
	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-follower/cancel-trader",
		Body:   cancelTraderBody{TraderID: traderID},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}
