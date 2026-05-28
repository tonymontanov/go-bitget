/*
FILE: spot/market.go

DESCRIPTION:
Public market-data sub-client for Bitget V2 SPOT. None of these
endpoints require authentication; the SDK still funnels them through
the same RestDoer for unified rate-limit accounting.

IMPLEMENTS:

	GET /api/v2/spot/public/symbols          — GetSymbolInfo
	GET /api/v2/spot/market/orderbook        — GetOrderBook
	GET /api/v2/spot/market/tickers          — GetMarketTicker
	GET /api/v2/spot/market/candles          — GetHistoricalCandles
	                                          GetHistoricalCandles1m

BITGET SPOT SPECIFICS:

  - /orderbook takes a numeric `limit` (1..150) — unlike mix
    /merge-depth which uses the named "max15"/"max50"/"max100"/
    "max200" presets. The SDK clamps the requested depth to [1, 150]
    with depth ≤ 0 → 50 (the venue default plus a sensible market-
    making default).

  - /candles ships an 8-element row [t,o,h,l,c,vBase,vQuote,vUsdt]
    on spot (mix has 7). bgcommon.ParseCandles ignores the optional
    8th column, so the same parser handles both profiles without
    branching.

  - All response numbers are JSON STRINGS. The SDK decodes them via
    bgcommon.ParseDecimalOrZero / ParseInt*OrZero — empty strings
    resolve to zero (Bitget occasionally emits "" for fields that
    don't apply to the symbol, e.g. fee-rate on pre-launch coins).

  - /symbols returns all online symbols when called without the
    `symbol` query parameter. GetSymbolInfo always sends `symbol`;
    an empty result is reported as ErrorKindInvalidRequest.
*/

package spot

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// MarketDataClient — public market-data sub-client for the spot
// profile. Constructed eagerly by the parent spot.Client; depends on
// the parent only for REST plumbing (rate-limit-aware Do, signing if
// the venue ever asks for it on a market endpoint, logger).
type MarketDataClient struct {
	c *Client
}

func newMarketDataClient(c *Client) *MarketDataClient {
	return &MarketDataClient{c: c}
}

// ---------------------------------------------------------------------
// Symbol info.
// ---------------------------------------------------------------------

// symbolRow mirrors one row of GET /api/v2/spot/public/symbols. Field
// order follows the live Bitget V2 response; absent fields default to
// "" and the SDK normalises them to zero via bgcommon.ParseDecimalOrZero.
type symbolRow struct {
	Symbol            string `json:"symbol"`
	BaseCoin          string `json:"baseCoin"`
	QuoteCoin         string `json:"quoteCoin"`
	MinTradeAmount    string `json:"minTradeAmount"`
	MaxTradeAmount    string `json:"maxTradeAmount"`
	TakerFeeRate      string `json:"takerFeeRate"`
	MakerFeeRate      string `json:"makerFeeRate"`
	PricePrecision    string `json:"pricePrecision"`
	QuantityPrecision string `json:"quantityPrecision"`
	QuotePrecision    string `json:"quotePrecision"`
	Status            string `json:"status"`
	MinTradeUSDT      string `json:"minTradeUSDT"`
}

// GetSymbolInfo returns the instrument specification for `symbol`.
// Returns ErrorKindInvalidRequest if the symbol is empty or absent
// on the venue.
func (m *MarketDataClient) GetSymbolInfo(ctx context.Context, symbol string) (spottypes.SymbolInfo, error) {
	var out spottypes.SymbolInfo
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetSymbolInfo: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/spot/public/symbols",
		Query:  query,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return out, err
	}

	var rows []symbolRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetSymbolInfo: parse", err)
	}
	if len(rows) == 0 {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetSymbolInfo: symbol not found: "+symbol, nil)
	}
	return convertSymbolRow(rows[0])
}

// convertSymbolRow normalises one /symbols row into a SymbolInfo.
// Errors short-circuit on the first malformed numeric field.
func convertSymbolRow(row symbolRow) (spottypes.SymbolInfo, error) {
	var out spottypes.SymbolInfo = spottypes.SymbolInfo{
		Symbol:    row.Symbol,
		BaseCoin:  row.BaseCoin,
		QuoteCoin: row.QuoteCoin,
		Status:    row.Status,
	}

	var err error
	out.MinTradeAmount, err = bgcommon.ParseDecimalOrZero(row.MinTradeAmount)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("minTradeAmount", err)
	}
	out.MaxTradeAmount, err = bgcommon.ParseDecimalOrZero(row.MaxTradeAmount)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("maxTradeAmount", err)
	}
	out.MinTradeUSDT, err = bgcommon.ParseDecimalOrZero(row.MinTradeUSDT)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("minTradeUSDT", err)
	}
	out.MakerFeeRate, err = bgcommon.ParseDecimalOrZero(row.MakerFeeRate)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("makerFeeRate", err)
	}
	out.TakerFeeRate, err = bgcommon.ParseDecimalOrZero(row.TakerFeeRate)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("takerFeeRate", err)
	}

	out.PricePrecision, err = bgcommon.ParseIntOrZero(row.PricePrecision)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("pricePrecision", err)
	}
	out.QuantityPrecision, err = bgcommon.ParseIntOrZero(row.QuantityPrecision)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("quantityPrecision", err)
	}
	out.QuotePrecision, err = bgcommon.ParseIntOrZero(row.QuotePrecision)
	if err != nil {
		return spottypes.SymbolInfo{}, wrapSymbolParseErr("quotePrecision", err)
	}

	// Spot price/size step is always 10^(-precision) — Bitget never
	// publishes a separate "endStep" multiplier on this profile (mix
	// does, hence the divergence in convertContractRow on that side).
	out.PriceTick = decimal.New(1, int32(-out.PricePrecision))
	out.SizeStep = decimal.New(1, int32(-out.QuantityPrecision))
	out.QuoteStep = decimal.New(1, int32(-out.QuotePrecision))
	return out, nil
}

func wrapSymbolParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetSymbolInfo: parse "+field, err)
}

// ---------------------------------------------------------------------
// Order book snapshot.
// ---------------------------------------------------------------------

// orderbookMaxDepth is the venue-side cap on /orderbook `limit`.
// Documented at 150 in the V2 spot reference. Requests above this
// floor are clamped client-side so we never round-trip just to be
// rejected.
const orderbookMaxDepth = 150

// resolveOrderbookDepth normalises the requested depth to [1, 150];
// depth ≤ 0 → 50 (a sensible market-making default).
func resolveOrderbookDepth(depth int) int {
	if depth <= 0 {
		return 50
	}
	if depth > orderbookMaxDepth {
		return orderbookMaxDepth
	}
	return depth
}

// orderbookPayload mirrors the data field of /orderbook.
type orderbookPayload struct {
	Asks [][]string `json:"asks"`
	Bids [][]string `json:"bids"`
	Ts   string     `json:"ts"`
}

// GetOrderBook returns a depth snapshot for `symbol`. depth is clamped
// to the venue cap of 150; depth ≤ 0 resolves to 50 (the SDK default).
func (m *MarketDataClient) GetOrderBook(ctx context.Context, symbol string, depth int) (roottypes.OrderBookSnapshot, error) {
	var out roottypes.OrderBookSnapshot
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetOrderBook: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("symbol", symbol)
	query.Set("type", "step0") // native tick precision; aggregation presets are not exposed in M2.
	query.Set("limit", strconv.Itoa(resolveOrderbookDepth(depth)))

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/spot/market/orderbook",
		Query:  query,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return out, err
	}

	var payload orderbookPayload
	if err = resp.UnmarshalData(&payload); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetOrderBook: parse", err)
	}

	out.Symbol = symbol
	out.Asks, err = bgcommon.ParseLevels(payload.Asks)
	if err != nil {
		return roottypes.OrderBookSnapshot{}, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetOrderBook: parse asks", err)
	}
	out.Bids, err = bgcommon.ParseLevels(payload.Bids)
	if err != nil {
		return roottypes.OrderBookSnapshot{}, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetOrderBook: parse bids", err)
	}
	out.TsMs, err = bgcommon.ParseInt64OrZero(payload.Ts)
	if err != nil {
		return roottypes.OrderBookSnapshot{}, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetOrderBook: parse ts", err)
	}
	// /orderbook never exposes a checksum — the WS "books" channel
	// does, and the M4 engine validates it there.
	out.Checksum = 0
	return out, nil
}

// ---------------------------------------------------------------------
// Historical candles.
// ---------------------------------------------------------------------

// candlesMaxLength is the Bitget V2 cap on /candles when the caller
// provides neither startTime nor endTime. With a window the cap is
// 1000; M2 only exposes the simpler "most recent N" form.
const candlesMaxLength = 200

// GetHistoricalCandles returns up to `length` recent candles for
// `symbol` at the given timeframe. Bitget caps a single call at 200
// rows in this mode (no startTime/endTime). length ≤ 0 → 100 (a
// sensible default that fits the desk's typical backfill).
func (m *MarketDataClient) GetHistoricalCandles(
	ctx context.Context,
	symbol string,
	timeframe roottypes.Timeframe,
	length int,
) (roottypes.Candles, error) {
	if symbol == "" {
		return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetHistoricalCandles: symbol is empty", nil)
	}
	if timeframe == "" {
		return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetHistoricalCandles: timeframe is empty", nil)
	}
	if length <= 0 {
		length = 100
	}
	if length > candlesMaxLength {
		length = candlesMaxLength
	}

	var query url.Values = url.Values{}
	query.Set("symbol", symbol)
	query.Set("granularity", timeframe.Wire())
	query.Set("limit", strconv.Itoa(length))

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/spot/market/candles",
		Query:  query,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return nil, err
	}

	var rows [][]string
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetHistoricalCandles: parse", err)
	}
	var out roottypes.Candles
	out, err = bgcommon.ParseCandles(rows)
	if err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetHistoricalCandles: parse rows", err)
	}
	return out, nil
}

// GetHistoricalCandles1m is a thin shortcut around GetHistoricalCandles
// at the 1-minute timeframe. Mirrors mix.MarketDataClient and the
// desk's ExchangeConnector helper of the same name.
func (m *MarketDataClient) GetHistoricalCandles1m(ctx context.Context, symbol string, length int) (roottypes.Candles, error) {
	return m.GetHistoricalCandles(ctx, symbol, roottypes.Timeframe1m, length)
}

// ---------------------------------------------------------------------
// Composite market ticker.
// ---------------------------------------------------------------------

// tickerRow mirrors one row of /api/v2/spot/market/tickers. Bitget
// returns a single-element list for a symbol query.
type tickerRow struct {
	Symbol       string `json:"symbol"`
	LastPrice    string `json:"lastPr"`
	AskPrice     string `json:"askPr"`
	AskSize      string `json:"askSz"`
	BidPrice     string `json:"bidPr"`
	BidSize      string `json:"bidSz"`
	High24h      string `json:"high24h"`
	Low24h       string `json:"low24h"`
	Open         string `json:"open"`
	OpenUtc      string `json:"openUtc"`
	BaseVolume   string `json:"baseVolume"`
	QuoteVolume  string `json:"quoteVolume"`
	UsdtVolume   string `json:"usdtVolume"`
	Change24h    string `json:"change24h"`
	ChangeUtc24h string `json:"changeUtc24h"`
	Ts           string `json:"ts"`
}

// GetMarketTicker returns the composite price snapshot for `symbol`.
func (m *MarketDataClient) GetMarketTicker(ctx context.Context, symbol string) (spottypes.MarketTicker, error) {
	var out spottypes.MarketTicker
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetMarketTicker: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/spot/market/tickers",
		Query:  query,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return out, err
	}

	var rows []tickerRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetMarketTicker: parse", err)
	}
	if len(rows) == 0 {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.MarketData.GetMarketTicker: symbol not found: "+symbol, nil)
	}
	return convertTicker(rows[0])
}

// convertTicker normalises one /tickers row.
func convertTicker(row tickerRow) (spottypes.MarketTicker, error) {
	var out spottypes.MarketTicker
	out.Symbol = row.Symbol

	var err error
	out.LastPrice, err = bgcommon.ParseDecimalOrZero(row.LastPrice)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("lastPr", err)
	}
	out.AskPrice, err = bgcommon.ParseDecimalOrZero(row.AskPrice)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("askPr", err)
	}
	out.AskSize, err = bgcommon.ParseDecimalOrZero(row.AskSize)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("askSz", err)
	}
	out.BidPrice, err = bgcommon.ParseDecimalOrZero(row.BidPrice)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("bidPr", err)
	}
	out.BidSize, err = bgcommon.ParseDecimalOrZero(row.BidSize)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("bidSz", err)
	}
	out.High24h, err = bgcommon.ParseDecimalOrZero(row.High24h)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("high24h", err)
	}
	out.Low24h, err = bgcommon.ParseDecimalOrZero(row.Low24h)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("low24h", err)
	}
	out.Open, err = bgcommon.ParseDecimalOrZero(row.Open)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("open", err)
	}
	out.OpenUtc, err = bgcommon.ParseDecimalOrZero(row.OpenUtc)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("openUtc", err)
	}
	out.BaseVolume, err = bgcommon.ParseDecimalOrZero(row.BaseVolume)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("baseVolume", err)
	}
	out.QuoteVolume, err = bgcommon.ParseDecimalOrZero(row.QuoteVolume)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("quoteVolume", err)
	}
	out.UsdtVolume, err = bgcommon.ParseDecimalOrZero(row.UsdtVolume)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("usdtVolume", err)
	}
	out.Change24h, err = bgcommon.ParseDecimalOrZero(row.Change24h)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("change24h", err)
	}
	out.ChangeUtc24h, err = bgcommon.ParseDecimalOrZero(row.ChangeUtc24h)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("changeUtc24h", err)
	}
	out.TsMs, err = bgcommon.ParseInt64OrZero(row.Ts)
	if err != nil {
		return spottypes.MarketTicker{}, wrapTickerParseErr("ts", err)
	}
	return out, nil
}

func wrapTickerParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "spot.MarketData.GetMarketTicker: parse "+field, err)
}
