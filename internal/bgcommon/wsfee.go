/*
FILE: internal/bgcommon/wsfee.go

DESCRIPTION:
Profile-agnostic wire shape and parser for the `feeDetail[]` array
that the Bitget V2 `fill` channel ships on both spot and mix. The
field-level wire contract is byte-identical across product types —
spot doesn't even have hedge-mode aliases, contract-side and spot-
side fees both look like:

	{
	    "deduction":         "no",
	    "totalDeductionFee": "0",
	    "totalFee":          "0.0153865",
	    "feeCoin":           "USDT"
	}

So we lift the row+parser into shared infrastructure rather than
copy-pasting it into spot/stream-private.go and mix/stream-private.go
once `WatchFills` lands on each profile in v2.0.0-m6.

WHAT IS NOT HERE:

  - The `Fill` row itself is INTENTIONALLY profile-specific. Spot
    ships `priceAvg`/`size`/`amount`; mix ships `price`/`baseVolume`/
    `quoteVolume`/`profit`/`tradeSide`/`posMode`/`clientOid` instead.
    A shared `Fill` shape would either bloat spot with always-zero
    derivatives fields or omit half of mix's data — same anti-pattern
    we avoided for the ticker channel in M4.

  - The `feeDetail` array is the single piece of the fill push that
    has identical wire across both profiles. THAT is what lives here.
*/

package bgcommon

import (
	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
)

// WSFeeDetailRow mirrors one element of the `feeDetail` JSON array
// on the Bitget V2 fill channel (spot + mix). Numeric fields use
// FlexString to absorb the "JSON number instead of quoted string"
// regression class we already shipped fixes for on positions
// (v1.2.1) — better to set the precedent here than wait for the
// next prod incident.
type WSFeeDetailRow struct {
	FeeCoin           string     `json:"feeCoin"`
	Deduction         string     `json:"deduction"`
	TotalDeductionFee FlexString `json:"totalDeductionFee"`
	TotalFee          FlexString `json:"totalFee"`
}

// WSFeeDetail is the SDK-side typed representation of one feeDetail
// row. Numeric fields are decimal.Decimal; the deduction flag stays
// as a string ("yes"/"no") because we'd rather not invent an enum
// for a two-value contract that never widens.
type WSFeeDetail struct {
	FeeCoin           string
	Deduction         string
	TotalDeductionFee decimal.Decimal
	TotalFee          decimal.Decimal
}

// ParseFeeDetail converts one wire row into the typed shape. Empty
// numeric strings parse as decimal.Zero (Bitget's standard "absent"
// sentinel). Non-empty malformed strings surface as a typed error
// so the caller can route it through the stream's errHandler — the
// row is rejected, not silently zeroed.
func ParseFeeDetail(row WSFeeDetailRow) (WSFeeDetail, error) {
	var out WSFeeDetail = WSFeeDetail{
		FeeCoin:   row.FeeCoin,
		Deduction: row.Deduction,
	}
	var err error
	out.TotalDeductionFee, err = ParseDecimalOrZero(string(row.TotalDeductionFee))
	if err != nil {
		return WSFeeDetail{}, bgerr.New(bgerr.ErrorKindUnknown, "",
			"bgcommon.ParseFeeDetail: parse totalDeductionFee", err)
	}
	out.TotalFee, err = ParseDecimalOrZero(string(row.TotalFee))
	if err != nil {
		return WSFeeDetail{}, bgerr.New(bgerr.ErrorKindUnknown, "",
			"bgcommon.ParseFeeDetail: parse totalFee", err)
	}
	return out, nil
}

// ParseFeeDetailList converts the entire wire array. On the first
// malformed row the function returns the underlying error (wrapped)
// and a nil slice — callers should treat this as "drop the row, do
// not aggregate partial data". Empty input returns (nil, nil) so a
// fill push without a feeDetail array decodes cleanly.
func ParseFeeDetailList(rows []WSFeeDetailRow) ([]WSFeeDetail, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	var out []WSFeeDetail = make([]WSFeeDetail, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var fd WSFeeDetail
		var err error
		fd, err = ParseFeeDetail(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, fd)
	}
	return out, nil
}
