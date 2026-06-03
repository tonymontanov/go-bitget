/*
FILE: internal/bgcommon/wsfee_test.go

DESCRIPTION:
Unit tests for the shared WSFeeDetail wire shape and parser. Pins
empty-as-zero semantics, JSON-number-vs-string acceptance (the
PARTIUSDT regression class from v1.2.1, applied pre-emptively here
before fills land), and the empty-input contract.
*/

package bgcommon

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/codec"
)

func TestParseFeeDetail_HappyPath(t *testing.T) {
	t.Parallel()
	var row WSFeeDetailRow = WSFeeDetailRow{
		FeeCoin:           "USDT",
		Deduction:         "no",
		TotalDeductionFee: "0",
		TotalFee:          "0.0153865",
	}
	var fd WSFeeDetail
	var err error
	fd, err = ParseFeeDetail(row)
	if err != nil {
		t.Fatalf("ParseFeeDetail: %v", err)
	}
	if fd.FeeCoin != "USDT" {
		t.Fatalf("feeCoin: %q", fd.FeeCoin)
	}
	if fd.Deduction != "no" {
		t.Fatalf("deduction: %q", fd.Deduction)
	}
	if !fd.TotalDeductionFee.Equal(decimal.Zero) {
		t.Fatalf("totalDeductionFee: %s", fd.TotalDeductionFee)
	}
	if !fd.TotalFee.Equal(decimal.RequireFromString("0.0153865")) {
		t.Fatalf("totalFee: %s", fd.TotalFee)
	}
}

// TestParseFeeDetail_AcceptsNumericFields locks down the same
// flexString regression we shipped fixes for on positions
// (v1.2.1, PARTIUSDT prod incident): every numeric field MAY arrive
// as a JSON number instead of the documented quoted string. Pre-
// empting the same issue on fills before it ever fires.
func TestParseFeeDetail_AcceptsNumericFields(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{
		"feeCoin": "USDT",
		"deduction": "no",
		"totalDeductionFee": 0,
		"totalFee": 0.0153865
	}`)
	var row WSFeeDetailRow
	if err := codec.Unmarshal(raw, &row); err != nil {
		t.Fatalf("unmarshal: %v (numeric-field regression)", err)
	}
	var fd WSFeeDetail
	var err error
	fd, err = ParseFeeDetail(row)
	if err != nil {
		t.Fatalf("ParseFeeDetail: %v", err)
	}
	if !fd.TotalFee.Equal(decimal.RequireFromString("0.0153865")) {
		t.Fatalf("totalFee from JSON-number: %s", fd.TotalFee)
	}
}

func TestParseFeeDetailList_Empty(t *testing.T) {
	t.Parallel()
	var out []WSFeeDetail
	var err error
	out, err = ParseFeeDetailList(nil)
	if err != nil {
		t.Fatalf("nil rows: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil slice, got %v", out)
	}
	out, err = ParseFeeDetailList([]WSFeeDetailRow{})
	if err != nil {
		t.Fatalf("empty rows: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil slice, got %v", out)
	}
}

func TestParseFeeDetailList_PropagatesError(t *testing.T) {
	t.Parallel()
	var rows = []WSFeeDetailRow{
		{FeeCoin: "USDT", Deduction: "no", TotalFee: "0.01"},
		{FeeCoin: "USDT", Deduction: "no", TotalFee: "not-a-number"},
	}
	var _, err = ParseFeeDetailList(rows)
	if err == nil {
		t.Fatal("expected error on malformed row, got nil")
	}
}
