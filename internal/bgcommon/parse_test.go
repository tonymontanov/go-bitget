/*
FILE: internal/bgcommon/parse_test.go

DESCRIPTION:
Unit tests for the shared parse helpers. Catches regressions in the most
critical hot-path conversions (level / candle arrays).
*/

package bgcommon

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseLevel(t *testing.T) {
	var lvl, err = ParseLevel([]string{"100.5", "0.123"})
	if err != nil {
		t.Fatal(err)
	}
	if !lvl.Price.Equal(decimal.RequireFromString("100.5")) {
		t.Fatalf("price = %s", lvl.Price)
	}
	if !lvl.Size.Equal(decimal.RequireFromString("0.123")) {
		t.Fatalf("size = %s", lvl.Size)
	}
}

func TestParseLevelMalformed(t *testing.T) {
	var _, err = ParseLevel([]string{"100"})
	if err == nil {
		t.Fatal("expected error on 1-element input")
	}
	_, err = ParseLevel([]string{"100", "abc"})
	if err == nil {
		t.Fatal("expected error on non-numeric size")
	}
}

func TestParseLevels(t *testing.T) {
	var lvls, err = ParseLevels([][]string{{"100", "1"}, {"99", "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lvls) != 2 {
		t.Fatalf("len = %d", len(lvls))
	}
}

func TestParseCandle(t *testing.T) {
	var cdl, err = ParseCandle([]string{"1700000000000", "100", "110", "90", "105", "1234.5", "129500"})
	if err != nil {
		t.Fatal(err)
	}
	if cdl.OpenTimeMs != 1700000000000 {
		t.Fatalf("ts = %d", cdl.OpenTimeMs)
	}
	if !cdl.Close.Equal(decimal.RequireFromString("105")) {
		t.Fatalf("close = %s", cdl.Close)
	}
}

func TestParseCandleShape(t *testing.T) {
	var _, err = ParseCandle([]string{"1700000000000", "100"})
	if err != ErrCandleShape {
		t.Fatalf("err = %v", err)
	}
}
