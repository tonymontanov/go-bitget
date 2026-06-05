/*
FILE: uta/helpers.go

DESCRIPTION:
Shared helpers for the UTA sub-clients: typed error constructors, the
rate-limit request meta, and a small decimal-parse helper. Kept
profile-local because the error-message prefix encodes the sub-client path.
*/

package uta

import (
	"context"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// errInvalid builds a client-side validation error. `scope` is the
// sub-client + method (e.g. "Trade.PlaceOrder").
func errInvalid(scope, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "uta."+scope+": "+msg, nil)
}

// errParse wraps a response-decode failure.
func errParse(scope string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "uta."+scope+": parse", cause)
}

// marketMeta tags an unsigned public market-data read (per-IP limits).
func marketMeta() rest.RequestMeta {
	return rest.RequestMeta{Category: string(bitget.RateLimitCategoryMarketData)}
}

// queryMeta tags a private GET / non-trading POST.
func queryMeta() rest.RequestMeta {
	return rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)}
}

// flowMeta tags an order-flow call (place / amend / cancel) with its
// category, order count and affected symbols so the desk's external
// limiter can model the venue limits.
func flowMeta(cat bitget.RateLimitCategory, orderCount int, symbols ...string) rest.RequestMeta {
	return rest.RequestMeta{
		Category:   string(cat),
		OrderCount: orderCount,
		Symbols:    symbols,
	}
}

// callSigned runs a signed REST call and (optionally) decodes the envelope
// data into dst. Shared by every private sub-client.
func (c *Client) callSigned(ctx context.Context, opts rest.Options, scope string, dst any) error {
	opts.Signed = true
	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, opts)
	if err != nil {
		return err
	}
	if dst != nil {
		if err = resp.UnmarshalData(dst); err != nil {
			return errParse(scope, err)
		}
	}
	return nil
}

// dec parses raw into *dst, wrapping a failure as a scoped parse error.
func dec(scope string, dst *decimal.Decimal, raw string) error {
	var err error
	if *dst, err = bgcommon.ParseDecimalOrZero(raw); err != nil {
		return errParse(scope, err)
	}
	return nil
}
