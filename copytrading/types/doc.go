/*
FILE: copytrading/types/doc.go

DESCRIPTION:
Domain types for the Bitget V2 COPY-TRADING profile — trader / follower
settings, copied-order and track-order views, profit / profit-share
records, and the trader-directory rows. Fields map straight from the
V2 wire (verified against the live docs and the cross-checked
third-party type definitions).

Concrete types are introduced per milestone alongside the sub-client
that returns them (M2 follower, M3 trader, M4 spot).
*/
package types
