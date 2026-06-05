/*
FILE: uta/public.go

DESCRIPTION:
Public sub-client — UNSIGNED V3 market data (core slice): server time,
instruments, tickers, orderbook, candles / history-candles and public
fills. None of these send an ACCESS-SIGN header; they work without
credentials.

Every market endpoint takes the unified `category` parameter. Request
params verified against the Bitget V3 docs and the tiagosiebler reference
client.
*/

package uta

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// PublicClient — unsigned V3 market-data sub-client.
type PublicClient struct {
	c *Client
}

func newPublicClient(c *Client) *PublicClient {
	return &PublicClient{c: c}
}

func i32(s string) int32 {
	var v int64
	v, _ = bgcommon.ParseInt64OrZero(s)
	return int32(v)
}

func i64(s string) int64 {
	var v int64
	v, _ = bgcommon.ParseInt64OrZero(s)
	return v
}

// ---------------------------------------------------------------------
// GetServerTime — public/time (unsigned).
// ---------------------------------------------------------------------

type serverTimeRow struct {
	ServerTime string `json:"serverTime"`
}

// GetServerTime returns the Bitget V3 server time in epoch milliseconds.
func (p *PublicClient) GetServerTime(ctx context.Context) (int64, error) {
	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v3/public/time",
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return 0, err
	}
	var row serverTimeRow
	if err = resp.UnmarshalData(&row); err != nil {
		return 0, errParse("Public.GetServerTime", err)
	}
	return i64(row.ServerTime), nil
}

// ---------------------------------------------------------------------
// GetInstruments — market/instruments (unsigned).
// ---------------------------------------------------------------------

type instrumentRow struct {
	Symbol              string `json:"symbol"`
	Category            string `json:"category"`
	BaseCoin            string `json:"baseCoin"`
	QuoteCoin           string `json:"quoteCoin"`
	Status              string `json:"status"`
	PricePrecision      string `json:"pricePrecision"`
	QuantityPrecision   string `json:"quantityPrecision"`
	QuotePrecision      string `json:"quotePrecision"`
	MinOrderQty         string `json:"minOrderQty"`
	MaxOrderQty         string `json:"maxOrderQty"`
	MaxMarketOrderQty   string `json:"maxMarketOrderQty"`
	MinOrderAmount      string `json:"minOrderAmount"`
	BuyLimitPriceRatio  string `json:"buyLimitPriceRatio"`
	SellLimitPriceRatio string `json:"sellLimitPriceRatio"`
	MakerFeeRate        string `json:"makerFeeRate"`
	TakerFeeRate        string `json:"takerFeeRate"`
	SymbolType          string `json:"symbolType"`
	MinLeverage         string `json:"minLeverage"`
	MaxLeverage         string `json:"maxLeverage"`
	FundInterval        string `json:"fundInterval"`
	LaunchTime          string `json:"launchTime"`
	DeliveryTime        string `json:"deliveryTime"`
}

// GetInstruments lists tradable instruments for a category. symbol is
// optional (filters to one). category is required.
func (p *PublicClient) GetInstruments(ctx context.Context, category utatypes.Category, symbol string) ([]utatypes.Instrument, error) {
	if category == "" {
		return nil, errInvalid("Public.GetInstruments", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v3/market/instruments",
		Query:  query,
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []instrumentRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Public.GetInstruments", err)
	}
	var out []utatypes.Instrument = make([]utatypes.Instrument, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var inst utatypes.Instrument = utatypes.Instrument{
			Symbol:            r.Symbol,
			Category:          utatypes.Category(r.Category),
			BaseCoin:          r.BaseCoin,
			QuoteCoin:         r.QuoteCoin,
			Status:            r.Status,
			PricePrecision:    i32(r.PricePrecision),
			QuantityPrecision: i32(r.QuantityPrecision),
			QuotePrecision:    i32(r.QuotePrecision),
			SymbolType:        r.SymbolType,
			FundInterval:      r.FundInterval,
			LaunchTimeMs:      i64(r.LaunchTime),
			DeliveryTime:      i64(r.DeliveryTime),
		}
		var scope = "Public.GetInstruments"
		var pairs = []struct {
			dst *decimal.Decimal
			raw string
		}{
			{&inst.MinOrderQty, r.MinOrderQty},
			{&inst.MaxOrderQty, r.MaxOrderQty},
			{&inst.MaxMarketOrderQty, r.MaxMarketOrderQty},
			{&inst.MinOrderAmount, r.MinOrderAmount},
			{&inst.BuyLimitPriceRatio, r.BuyLimitPriceRatio},
			{&inst.SellLimitPriceRatio, r.SellLimitPriceRatio},
			{&inst.MakerFeeRate, r.MakerFeeRate},
			{&inst.TakerFeeRate, r.TakerFeeRate},
			{&inst.MinLeverage, r.MinLeverage},
			{&inst.MaxLeverage, r.MaxLeverage},
		}
		var j int
		for j = 0; j < len(pairs); j++ {
			if err = dec(scope, pairs[j].dst, pairs[j].raw); err != nil {
				return nil, err
			}
		}
		out = append(out, inst)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetTickers — market/tickers (unsigned).
// ---------------------------------------------------------------------

type tickerRow struct {
	Category     string `json:"category"`
	Symbol       string `json:"symbol"`
	LastPrice    string `json:"lastPrice"`
	OpenPrice24h string `json:"openPrice24h"`
	HighPrice24h string `json:"highPrice24h"`
	LowPrice24h  string `json:"lowPrice24h"`
	Ask1Price    string `json:"ask1Price"`
	Bid1Price    string `json:"bid1Price"`
	Bid1Size     string `json:"bid1Size"`
	Ask1Size     string `json:"ask1Size"`
	Price24hPcnt string `json:"price24hPcnt"`
	Volume24h    string `json:"volume24h"`
	Turnover24h  string `json:"turnover24h"`
	IndexPrice   string `json:"indexPrice"`
	MarkPrice    string `json:"markPrice"`
	FundingRate  string `json:"fundingRate"`
	OpenInterest string `json:"openInterest"`
}

// GetTickers returns the 24h ticker snapshot for a category. symbol is
// optional (filters to one). category is required.
func (p *PublicClient) GetTickers(ctx context.Context, category utatypes.Category, symbol string) ([]utatypes.Ticker, error) {
	if category == "" {
		return nil, errInvalid("Public.GetTickers", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v3/market/tickers",
		Query:  query,
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []tickerRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Public.GetTickers", err)
	}
	var out []utatypes.Ticker = make([]utatypes.Ticker, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var tk utatypes.Ticker = utatypes.Ticker{Category: utatypes.Category(r.Category), Symbol: r.Symbol}
		var scope = "Public.GetTickers"
		var pairs = []struct {
			dst *decimal.Decimal
			raw string
		}{
			{&tk.LastPrice, r.LastPrice}, {&tk.OpenPrice24h, r.OpenPrice24h},
			{&tk.HighPrice24h, r.HighPrice24h}, {&tk.LowPrice24h, r.LowPrice24h},
			{&tk.Ask1Price, r.Ask1Price}, {&tk.Ask1Size, r.Ask1Size},
			{&tk.Bid1Price, r.Bid1Price}, {&tk.Bid1Size, r.Bid1Size},
			{&tk.Price24hPcnt, r.Price24hPcnt}, {&tk.Volume24h, r.Volume24h},
			{&tk.Turnover24h, r.Turnover24h}, {&tk.IndexPrice, r.IndexPrice},
			{&tk.MarkPrice, r.MarkPrice}, {&tk.FundingRate, r.FundingRate},
			{&tk.OpenInterest, r.OpenInterest},
		}
		var j int
		for j = 0; j < len(pairs); j++ {
			if err = dec(scope, pairs[j].dst, pairs[j].raw); err != nil {
				return nil, err
			}
		}
		out = append(out, tk)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetOrderBook — market/orderbook (unsigned).
// ---------------------------------------------------------------------

type orderBookRow struct {
	Asks [][]string `json:"a"`
	Bids [][]string `json:"b"`
	TS   string     `json:"ts"`
}

// GetOrderBook returns a depth snapshot. category and symbol are required;
// limit is optional (venue-defined depth cap).
func (p *PublicClient) GetOrderBook(ctx context.Context, category utatypes.Category, symbol string, limit int) (utatypes.OrderBook, error) {
	var out utatypes.OrderBook
	switch {
	case category == "":
		return out, errInvalid("Public.GetOrderBook", "category is required")
	case symbol == "":
		return out, errInvalid("Public.GetOrderBook", "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	query.Set("symbol", symbol)
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v3/market/orderbook",
		Query:  query,
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return out, err
	}
	var row orderBookRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Public.GetOrderBook", err)
	}
	if out.Asks, err = toLevels("Public.GetOrderBook", row.Asks); err != nil {
		return out, err
	}
	if out.Bids, err = toLevels("Public.GetOrderBook", row.Bids); err != nil {
		return out, err
	}
	out.TimeMs = i64(row.TS)
	return out, nil
}

func toLevels(scope string, raw [][]string) ([]utatypes.PriceLevel, error) {
	var out []utatypes.PriceLevel = make([]utatypes.PriceLevel, 0, len(raw))
	var i int
	for i = 0; i < len(raw); i++ {
		if len(raw[i]) < 2 {
			continue
		}
		var lvl utatypes.PriceLevel
		var err error
		if err = dec(scope, &lvl.Price, raw[i][0]); err != nil {
			return nil, err
		}
		if err = dec(scope, &lvl.Size, raw[i][1]); err != nil {
			return nil, err
		}
		out = append(out, lvl)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCandles / GetHistoryCandles — market/candles | history-candles.
// ---------------------------------------------------------------------

// CandlesQuery — parameters for GetCandles / GetHistoryCandles. Category,
// Symbol and Interval are required.
type CandlesQuery struct {
	Category    utatypes.Category
	Symbol      string
	Interval    utatypes.Interval
	Type        utatypes.CandleType
	StartTimeMs int64
	EndTimeMs   int64
	Limit       int
}

func (p *PublicClient) getCandles(ctx context.Context, path, scope string, q CandlesQuery) ([]utatypes.Candle, error) {
	switch {
	case q.Category == "":
		return nil, errInvalid(scope, "category is required")
	case q.Symbol == "":
		return nil, errInvalid(scope, "symbol is required")
	case q.Interval == "":
		return nil, errInvalid(scope, "interval is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(q.Category))
	query.Set("symbol", q.Symbol)
	query.Set("interval", string(q.Interval))
	if q.Type != "" {
		query.Set("type", string(q.Type))
	}
	if q.StartTimeMs > 0 {
		query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	}
	if q.EndTimeMs > 0 {
		query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	}
	if q.Limit > 0 {
		query.Set("limit", strconv.Itoa(q.Limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows [][]string
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse(scope, err)
	}
	var out []utatypes.Candle = make([]utatypes.Candle, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		if len(r) < 6 {
			continue
		}
		var k utatypes.Candle
		k.TimeMs = i64(r[0])
		var fields = []struct {
			dst *decimal.Decimal
			idx int
		}{
			{&k.Open, 1}, {&k.High, 2}, {&k.Low, 3}, {&k.Close, 4}, {&k.Volume, 5},
		}
		var j int
		for j = 0; j < len(fields); j++ {
			if err = dec(scope, fields[j].dst, r[fields[j].idx]); err != nil {
				return nil, err
			}
		}
		if len(r) >= 7 {
			if err = dec(scope, &k.Turnover, r[6]); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	return out, nil
}

// GetCandles returns recent candlesticks. Category, Symbol and Interval
// are required.
func (p *PublicClient) GetCandles(ctx context.Context, q CandlesQuery) ([]utatypes.Candle, error) {
	return p.getCandles(ctx, "/api/v3/market/candles", "Public.GetCandles", q)
}

// GetHistoryCandles returns older candlesticks (paged by time window).
// Category, Symbol and Interval are required.
func (p *PublicClient) GetHistoryCandles(ctx context.Context, q CandlesQuery) ([]utatypes.Candle, error) {
	return p.getCandles(ctx, "/api/v3/market/history-candles", "Public.GetHistoryCandles", q)
}

// ---------------------------------------------------------------------
// GetPublicFills — market/fills (unsigned).
// ---------------------------------------------------------------------

type publicFillRow struct {
	ExecID string `json:"execId"`
	Price  string `json:"price"`
	Size   string `json:"size"`
	Side   string `json:"side"`
	TS     string `json:"ts"`
}

// GetPublicFills returns recent public trades for a category. symbol is
// optional; limit is optional. category is required.
func (p *PublicClient) GetPublicFills(ctx context.Context, category utatypes.Category, symbol string, limit int) ([]utatypes.PublicFill, error) {
	if category == "" {
		return nil, errInvalid("Public.GetPublicFills", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v3/market/fills",
		Query:  query,
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []publicFillRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Public.GetPublicFills", err)
	}
	var out []utatypes.PublicFill = make([]utatypes.PublicFill, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var f utatypes.PublicFill = utatypes.PublicFill{ExecID: rows[i].ExecID, Side: rows[i].Side, TimeMs: i64(rows[i].TS)}
		if err = dec("Public.GetPublicFills", &f.Price, rows[i].Price); err != nil {
			return nil, err
		}
		if err = dec("Public.GetPublicFills", &f.Size, rows[i].Size); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}
