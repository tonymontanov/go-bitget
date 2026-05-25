/*
FILE: mix/market.go

DESCRIPTION:
Public market-data sub-client for Bitget MIX (legacy V2). None of these
endpoints require authentication; the SDK still funnels them through
the same restDoer for unified rate-limit accounting.

Implements:

	GET /api/v2/mix/market/contracts        — GetSymbolInfo
	GET /api/v2/mix/market/merge-depth      — GetOrderBook
	GET /api/v2/mix/market/ticker           — GetMarketTicker
	GET /api/v2/mix/market/candles          — GetHistoricalCandles
	                                          GetHistoricalCandles1m

BITGET MIX SPECIFICS:

  - /merge-depth uses NAMED depth limits ("max15", "max50", "max100",
    "max200") instead of integers. The SDK clamps the caller's
    requested depth to the closest preset; depth ≤ 0 resolves to
    "max50" (the SDK default). The "precision" parameter is pinned
    to "scale0" (smallest tick) — that's what every market-making
    workflow needs; configurable presets can be exposed later.

  - /candles returns klines ASCENDING by openTime (oldest first),
    confirmed against the V2 docs. The SDK preserves this order so
    callers feeding klines into a backfill pipeline can simply
    append.

  - /candles caps at 200 rows per call when neither startTime nor
    endTime is provided. With startTime/endTime the cap is 1000. The
    SDK does NOT page automatically — it caps the requested length
    at 200 and returns whatever Bitget gave us. A historical-window
    helper that pages will land alongside backfill work in a later
    milestone if the desk needs it.

  - All response numbers are JSON STRINGS. The SDK decodes them via
    decimal.NewFromString and exposes shopspring/decimal to callers.

  - /contracts returns ALL contracts under the configured product type
    when called without a `symbol` parameter. GetSymbolInfo always
    sends `symbol`; an empty result means the symbol does not exist
    in the configured productType, which the SDK reports as
    ErrorKindInvalidRequest.
*/

package mix

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// MarketDataClient — public market-data sub-client.
type MarketDataClient struct {
	c *Client
}

func newMarketDataClient(c *Client) *MarketDataClient {
	return &MarketDataClient{c: c}
}

// ---------------------------------------------------------------------
// Symbol info.
// ---------------------------------------------------------------------

// contractRow mirrors one row of GET /api/v2/mix/market/contracts. Field
// order follows the live Bitget V2 response; absent fields default to
// "" and the SDK normalises them to zero via parseDecimalOrZero.
type contractRow struct {
	Symbol         string `json:"symbol"`
	BaseCoin       string `json:"baseCoin"`
	QuoteCoin      string `json:"quoteCoin"`
	BuyLimitPrice  string `json:"buyLimitPriceRatio"`
	SellLimitPrice string `json:"sellLimitPriceRatio"`
	FeeRateUpRatio string `json:"feeRateUpRatio"`
	MakerFeeRate   string `json:"makerFeeRate"`
	TakerFeeRate   string `json:"takerFeeRate"`
	OpenCostUpRate string `json:"openCostUpRatio"`
	SupportMargin  any    `json:"supportMarginCoins"`
	MinTradeNum    string `json:"minTradeNum"`
	PriceEndStep   string `json:"priceEndStep"`
	VolumePlace    string `json:"volumePlace"`
	PricePlace     string `json:"pricePlace"`
	SizeMultiplier string `json:"sizeMultiplier"`
	SymbolType     string `json:"symbolType"`
	MinTradeUSDT   string `json:"minTradeUSDT"`
	MaxSymbolOrder string `json:"maxSymbolOrderNum"`
	MaxProductOrd  string `json:"maxProductOrderNum"`
	MaxPositionNum string `json:"maxPositionNum"`
	SymbolStatus   string `json:"symbolStatus"`
	OffTime        string `json:"offTime"`
	LimitOpenTime  string `json:"limitOpenTime"`
	DeliveryTime   string `json:"deliveryTime"`
	DeliveryStart  string `json:"deliveryStartTime"`
	LaunchTime     string `json:"launchTime"`
	FundInterval   string `json:"fundInterval"`
	MinLever       string `json:"minLever"`
	MaxLever       string `json:"maxLever"`
	PosLimit       string `json:"posLimit"`
	MaintainTime   string `json:"maintainTime"`
}

// GetSymbolInfo returns the instrument specification for `symbol` under
// the client's configured product type. Returns ErrorKindInvalidRequest
// if the symbol is empty or absent on the venue.
func (m *MarketDataClient) GetSymbolInfo(ctx context.Context, symbol string) (mixtypes.SymbolInfo, error) {
	var out mixtypes.SymbolInfo
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetSymbolInfo: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("productType", string(m.c.productType))
	query.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/market/contracts",
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

	var rows []contractRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse", err)
	}
	if len(rows) == 0 {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetSymbolInfo: symbol not found: "+symbol, nil)
	}
	return convertContractRow(rows[0], m.c.productType)
}

// convertContractRow normalises one /contracts row into a SymbolInfo.
// Errors short-circuit on the first malformed numeric field; a single
// bad row should not silently corrupt downstream consumers.
func convertContractRow(row contractRow, productType roottypes.ProductType) (mixtypes.SymbolInfo, error) {
	var out mixtypes.SymbolInfo = mixtypes.SymbolInfo{
		Symbol:       row.Symbol,
		BaseCoin:     row.BaseCoin,
		QuoteCoin:    row.QuoteCoin,
		ProductType:  productType,
		SymbolStatus: row.SymbolStatus,
	}

	var err error
	out.MinTradeNum, err = parseDecimalOrZero(row.MinTradeNum)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse minTradeNum", err)
	}
	out.MinTradeUSDT, err = parseDecimalOrZero(row.MinTradeUSDT)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse minTradeUSDT", err)
	}

	// Bitget reports precision (price / volume) as integer counts of
	// decimal digits. tickSize / stepSize are derived from precision via
	// 10^(-precision).
	out.PricePrecision, err = parseIntOrZero(row.PricePlace)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse pricePlace", err)
	}
	out.SizePrecision, err = parseIntOrZero(row.VolumePlace)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse volumePlace", err)
	}
	// Bitget exposes priceEndStep as an integer multiplier of 10^(-pricePlace).
	// The effective price tick equals priceEndStep * 10^(-pricePlace).
	// Example: pricePlace=1, priceEndStep=5 → tick = 5 * 10^-1 = 0.5.
	var priceEndStep int
	priceEndStep, err = parseIntOrZero(row.PriceEndStep)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse priceEndStep", err)
	}
	if priceEndStep <= 0 {
		// Some symbols report priceEndStep="" — fall back to a unit step
		// at the configured precision (1 * 10^-pricePlace).
		priceEndStep = 1
	}
	out.PriceTick = decimalScale(int64(priceEndStep), -out.PricePrecision)
	out.SizeStep = decimalScale(1, -out.SizePrecision)

	out.MinLever, err = parseIntOrZero(row.MinLever)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse minLever", err)
	}
	out.MaxLever, err = parseIntOrZero(row.MaxLever)
	if err != nil {
		return mixtypes.SymbolInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetSymbolInfo: parse maxLever", err)
	}
	return out, nil
}

// decimalScale returns base * 10^exp as decimal.Decimal without going
// through float64. Used to derive price / size step from Bitget's
// integer-multiplier + precision pair (e.g. priceEndStep=5 with
// pricePlace=1 → tick = 5e-1 = 0.5).
func decimalScale(base int64, exp int) decimal.Decimal {
	return decimal.New(base, int32(exp))
}

// ---------------------------------------------------------------------
// Order book snapshot.
// ---------------------------------------------------------------------

// orderbookDepthPresets maps an upper-bound depth (>=15..200) to the
// Bitget /merge-depth `limit` keyword. Sorted ascending by depth so a
// linear scan picks the closest preset.
var orderbookDepthPresets = []struct {
	maxRows int
	keyword string
}{
	{15, "max15"},
	{50, "max50"},
	{100, "max100"},
	{200, "max200"},
}

// resolveDepth picks the closest Bitget depth keyword for the requested
// row count. depth ≤ 0 → "max50".
func resolveDepth(depth int) string {
	if depth <= 0 {
		return "max50"
	}
	var i int
	for i = 0; i < len(orderbookDepthPresets); i++ {
		if depth <= orderbookDepthPresets[i].maxRows {
			return orderbookDepthPresets[i].keyword
		}
	}
	return orderbookDepthPresets[len(orderbookDepthPresets)-1].keyword
}

// orderbookPayload mirrors the data field of /merge-depth.
type orderbookPayload struct {
	Asks      [][]string `json:"asks"`
	Bids      [][]string `json:"bids"`
	Ts        string     `json:"ts"`
	Precision string     `json:"precision"`
	Scale     string     `json:"scale"`
}

// GetOrderBook returns a depth snapshot for `symbol`. depth is clamped
// to Bitget's named presets (max15 / max50 / max100 / max200); depth ≤
// 0 resolves to max50, the SDK default.
func (m *MarketDataClient) GetOrderBook(ctx context.Context, symbol string, depth int) (roottypes.OrderBookSnapshot, error) {
	var out roottypes.OrderBookSnapshot
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetOrderBook: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("productType", string(m.c.productType))
	query.Set("symbol", symbol)
	query.Set("limit", resolveDepth(depth))
	// "scale0" = native tick precision. Other presets aggregate levels;
	// market-making consumers always need the smallest tick.
	query.Set("precision", "scale0")

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/market/merge-depth",
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
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetOrderBook: parse", err)
	}

	out.Symbol = symbol
	out.Asks, err = bgcommon.ParseLevels(payload.Asks)
	if err != nil {
		return roottypes.OrderBookSnapshot{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetOrderBook: parse asks", err)
	}
	out.Bids, err = bgcommon.ParseLevels(payload.Bids)
	if err != nil {
		return roottypes.OrderBookSnapshot{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetOrderBook: parse bids", err)
	}
	out.TsMs, err = parseInt64OrZero(payload.Ts)
	if err != nil {
		return roottypes.OrderBookSnapshot{}, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetOrderBook: parse ts", err)
	}
	// /merge-depth never exposes a checksum — the WS "books" channel
	// does, and the M4 engine validates it there.
	out.Checksum = 0
	return out, nil
}

// ---------------------------------------------------------------------
// Historical candles.
// ---------------------------------------------------------------------

// candlesMaxLength is the Bitget V2 cap on /candles when the caller
// provides neither startTime nor endTime. With a window the cap is
// 1000; M1 only exposes the simpler "most recent N" form.
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
		return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetHistoricalCandles: symbol is empty", nil)
	}
	if timeframe == "" {
		return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetHistoricalCandles: timeframe is empty", nil)
	}
	if length <= 0 {
		length = 100
	}
	if length > candlesMaxLength {
		length = candlesMaxLength
	}

	var query url.Values = url.Values{}
	query.Set("productType", string(m.c.productType))
	query.Set("symbol", symbol)
	query.Set("granularity", timeframe.Wire())
	query.Set("limit", strconv.Itoa(length))

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/market/candles",
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
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetHistoricalCandles: parse", err)
	}
	var out roottypes.Candles
	out, err = bgcommon.ParseCandles(rows)
	if err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetHistoricalCandles: parse rows", err)
	}
	return out, nil
}

// GetHistoricalCandles1m is a thin shortcut around GetHistoricalCandles
// at the 1-minute timeframe. Mirrors the desk's ExchangeConnector
// helper of the same name.
func (m *MarketDataClient) GetHistoricalCandles1m(ctx context.Context, symbol string, length int) (roottypes.Candles, error) {
	return m.GetHistoricalCandles(ctx, symbol, roottypes.Timeframe1m, length)
}

// ---------------------------------------------------------------------
// Composite market ticker.
// ---------------------------------------------------------------------

// tickerRow mirrors one row of /api/v2/mix/market/ticker. Bitget
// returns a single-element list for a symbol query.
type tickerRow struct {
	Symbol             string `json:"symbol"`
	LastPrice          string `json:"lastPr"`
	MarkPrice          string `json:"markPrice"`
	IndexPrice         string `json:"indexPrice"`
	AskPrice           string `json:"askPr"`
	BidPrice           string `json:"bidPr"`
	FundingRate        string `json:"fundingRate"`
	NextFundingTime    string `json:"nextFundingTime"`
	Ts                 string `json:"ts"`
	High24h            string `json:"high24h"`
	Low24h             string `json:"low24h"`
	BaseVolume         string `json:"baseVolume"`
	QuoteVolume        string `json:"quoteVolume"`
	UsdtVolume         string `json:"usdtVolume"`
	OpenInterest       string `json:"holdingAmount"`
	Open24hPrice       string `json:"open24h"`
	OpenChangeUtc24h   string `json:"changeUtc24h"`
	OpenChangePercent  string `json:"change24h"`
	DeliveryStartTime  string `json:"deliveryStartTime"`
	DeliveryTime       string `json:"deliveryTime"`
	DeliveryStatus     string `json:"deliveryStatus"`
}

// GetMarketTicker returns the composite price snapshot for `symbol`.
// Bitget bundles last/mark/index/funding into one response; the SDK
// exposes them all so the caller doesn't have to issue three calls.
func (m *MarketDataClient) GetMarketTicker(ctx context.Context, symbol string) (mixtypes.MarketTicker, error) {
	var out mixtypes.MarketTicker
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetMarketTicker: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("productType", string(m.c.productType))
	query.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/market/ticker",
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
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetMarketTicker: parse", err)
	}
	if len(rows) == 0 {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.MarketData.GetMarketTicker: symbol not found: "+symbol, nil)
	}
	return convertTicker(rows[0])
}

// convertTicker normalises one /ticker row.
func convertTicker(row tickerRow) (mixtypes.MarketTicker, error) {
	var out mixtypes.MarketTicker
	out.Symbol = row.Symbol

	var err error
	out.LastPrice, err = parseDecimalOrZero(row.LastPrice)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("lastPr", err)
	}
	out.MarkPrice, err = parseDecimalOrZero(row.MarkPrice)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("markPrice", err)
	}
	out.IndexPrice, err = parseDecimalOrZero(row.IndexPrice)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("indexPrice", err)
	}
	out.AskPrice, err = parseDecimalOrZero(row.AskPrice)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("askPr", err)
	}
	out.BidPrice, err = parseDecimalOrZero(row.BidPrice)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("bidPr", err)
	}
	out.FundingRate, err = parseDecimalOrZero(row.FundingRate)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("fundingRate", err)
	}
	out.NextFundingTimeMs, err = parseInt64OrZero(row.NextFundingTime)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("nextFundingTime", err)
	}
	out.TsMs, err = parseInt64OrZero(row.Ts)
	if err != nil {
		return mixtypes.MarketTicker{}, wrapTickerParseErr("ts", err)
	}
	return out, nil
}

func wrapTickerParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "mix.MarketData.GetMarketTicker: parse "+field, err)
}
