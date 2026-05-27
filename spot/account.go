/*
FILE: spot/account.go

DESCRIPTION:
Account sub-client for the Bitget V2 SPOT profile. M1 ships the
struct + constructor; M3 wires the REST endpoints. Spot has no
positions / leverage / position-mode — the account surface is
exclusively about coin balances, account-level metadata, and
post-trade history.

ENDPOINTS WIRED IN M3:

  - GET  /api/v2/spot/account/info
  - GET  /api/v2/spot/account/assets        (per-coin balance snapshot)
  - GET  /api/v2/spot/trade/unfilled-orders (open orders)
  - GET  /api/v2/spot/trade/history-orders  (filled / cancelled history)
  - GET  /api/v2/spot/trade/fills           (own fills history)

DIFFERENCES FROM mix.AccountClient:

  - No GetPosition / SetLeverage / SetPositionMode / ClosePosition.
    Spot has no positions; balances are returned per coin via
    GetBalance / GetCoinBalance.
  - SyncOrderMappings (the desk-side helper that pairs clientOid ↔
    orderId on every account-side response) keeps the same shape on
    spot — Bitget echoes both fields uniformly across V2 profiles.

Helpers used (already in internal/bgcommon, no per-profile copy):

  - bgcommon.ParseDecimalOrZero — coin balances ship as strings with
                                 empty-as-zero semantics.
  - bgcommon.ParseInt64OrZero   — timestamps (cTime / uTime).
*/

package spot

// AccountClient — account / balance / order-history sub-client. Built
// once per spot.Client (see client.go) and safe for concurrent use.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}
