/*
FILE: copytrading/doc.go

DESCRIPTION:
Package copytrading implements the Bitget V2 COPY-TRADING profile —
the /api/v2/copy/* domain that links lead TRADERS with their FOLLOWERS
on both the futures (mix) and spot venues.

PROFILE SHAPE — product × role:

Bitget splits copy trading along two axes, mirrored 1:1 by the SDK's
sub-clients:

	            TRADER (lead)            FOLLOWER (copier)
	FUTURES     FuturesTrader()          FuturesFollower()
	SPOT        SpotTrader()             SpotFollower()

	(/api/v2/copy/mix-trader/...)  (/api/v2/copy/mix-follower/...)
	(/api/v2/copy/spot-trader/...) (/api/v2/copy/spot-follower/...)

REST-ONLY:

Copy trading has NO dedicated WebSocket. A lead trader's own fills and
a follower's copied orders flow through the ordinary mix / spot PRIVATE
WS channels (orders / fill / positions). This profile therefore ships
no Stream sub-client — use mix.Stream() / spot.Stream() for live order
state.

PRODUCT TYPE (futures only):

Every futures copy-trading endpoint carries productType
(USDT-FUTURES / COIN-FUTURES / USDC-FUTURES). It is pinned at
construction via ClientSettings{ProductType} (default USDT-FUTURES),
matching how the mix profile pins its product type — callers do not
repeat it on every call. The spot sub-clients ignore product type
(spot copy trading is single-venue).

ROLE ELIGIBILITY:

The TRADER endpoints require the account to be an approved Bitget lead
trader (application + KYC on the platform). The SDK validates request
shape and parses responses; whether the account is actually allowed to
act as a trader is a venue-side gate surfaced as a typed exchange error
on the live call.

BROKER:

The /api/v2/copy/mix-broker/* read-only queries are intentionally NOT
in this profile — they belong to the broker (Agent) domain shipped in
a later phase.
*/
package copytrading
