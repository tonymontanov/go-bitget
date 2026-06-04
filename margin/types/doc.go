/*
FILE: margin/types/doc.go

DESCRIPTION:
Public request / response types for the Bitget V2 MARGIN profile.

Margin trades SPOT instruments with borrowed funds, so these types are
close cousins of the spot/types equivalents — but margin adds a
loanType (auto-borrow / auto-repay control) on every order and splits
the order size into BaseSize / QuoteSize on the wire (Bitget uses
distinct fields here rather than the single side-dependent `size` that
spot accepts). They are kept profile-local (not aliased to spot) so the
margin-only fields have a natural home and the two profiles can evolve
independently.

These types intentionally avoid mix's position concepts (HoldSide /
TradeSide / leverage on the order) — margin has no positions, only a
borrow balance tracked at the account level.
*/
package types
