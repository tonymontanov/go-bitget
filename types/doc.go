/*
Package types defines protocol-common data types reused across every
Bitget profile (mix, spot, uta).

It contains the wire-shaped enums (Side / OrderType / TimeInForce /
OrderStatus / ProductType), the order-book primitives, the candle /
timeframe representation and the trade-update / kline-update structs.

Profile packages re-export the set they need via type aliases — no
behaviour duplication. See mix/types/ for the v1.0 (USDT-margined
perpetuals) re-exports.
*/
package types
