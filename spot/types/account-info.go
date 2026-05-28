/*
FILE: spot/types/account-info.go

DESCRIPTION:
AccountInfo — meta-information about the API key's owning account,
returned by GET /api/v2/spot/account/info.

The endpoint does NOT expose balances — those live under
/account/assets and map to roottypes.Balance. AccountInfo carries
identity / configuration data the desk uses for boot-time health
checks and operator display:

  - UserID            — the Bitget user-ID owning this API key.
  - InviterID         — referrer chain (informational; empty for
                        non-referred accounts).
  - IPs               — comma-delimited list of whitelisted source
                        IPs for this API key. The desk asserts the
                        running host is in this list at start-up.
  - Authorities       — granted scopes (e.g. ["spot","contract"]).
                        The desk refuses to start the spot connector
                        if "spot" is missing.
  - ParentID          — the master-account user-ID for sub-account
                        keys; equal to UserID for top-level accounts.
  - TraderType        — "general_trader" / "trader_pro" / "vip" /
                        "market_maker" — controls fee tiers. Stored
                        as a raw string because Bitget extends the
                        set without bumping the API version.
  - ChannelCode       — affiliate channel (operator display only).
  - RegisTimeMs       — account registration timestamp (ms since
                        epoch). Useful when the operator needs to
                        confirm the API key belongs to the expected
                        account on a multi-key host.
*/

package types

// AccountInfo — meta about the API key's owning account.
type AccountInfo struct {
	UserID       string
	InviterID    string
	IPs          string
	Authorities  []string
	ParentID     string
	TraderType   string
	ChannelCode  string
	RegisTimeMs  int64
}
