/*
FILE: spot/market.go

DESCRIPTION:
Public market-data sub-client for the Bitget V2 SPOT profile. M1 ships
the struct + constructor; M2 wires the REST endpoints.

ENDPOINTS WIRED IN M2:

  - GET /api/v2/spot/public/symbols          (instrument metadata)
  - GET /api/v2/spot/public/coins            (deposit/withdraw flags)
  - GET /api/v2/spot/market/tickers          (per-symbol or all)
  - GET /api/v2/spot/market/orderbook        (depth snapshot)
  - GET /api/v2/spot/market/candles          (kline)
  - GET /api/v2/spot/market/history-candles  (paginated kline)
  - GET /api/v2/spot/market/fills            (recent trades)
  - GET /api/v2/spot/market/fills-history    (paginated trades)

DIFFERENCES FROM mix.MarketDataClient:

  - SymbolInfo schema is different — spot exposes precision /
    minTradeAmount / minTradeUSDT / status, but no leverage / margin
    coin / contract size. The shared internal/bgcommon parsers cover
    everything numeric; the SymbolInfo struct itself lives in
    spot/types/ (M2 introduces it).
  - No /api/v2/spot/market/contracts equivalent — spot uses /symbols.

All decoding goes through bgcommon helpers (ParseDecimalOrZero /
ParseInt*OrZero / orderbook.ParseLevels for snapshots returned by
/orderbook). Symbol normalisation stays the responsibility of the
caller — the SDK accepts any case Bitget would echo back.
*/

package spot

// MarketDataClient — public market-data sub-client. Built once per
// spot.Client (see client.go) and safe for concurrent use. All methods
// are read-only and require no signer; they go through the shared
// REST transport for connection pooling.
type MarketDataClient struct {
	c *Client
}

func newMarketDataClient(c *Client) *MarketDataClient {
	return &MarketDataClient{c: c}
}
