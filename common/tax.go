/*
FILE: common/tax.go

DESCRIPTION:
Tax sub-client — tax transaction records (/api/v2/tax/{spot,future,margin,
p2p}-record). All signed reads. Each endpoint requires a
[startTime,endTime] window and returns a flat array walked by the
idLessThan cursor (next cursor = the last row's id).

Request params verified against the Bitget V2 docs and the tiagosiebler
reference client.
*/

package common

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	commontypes "github.com/tonymontanov/go-bitget/v2/common/types"
)

// TaxClient — tax transaction-records sub-client.
type TaxClient struct {
	c *Client
}

func newTaxClient(c *Client) *TaxClient {
	return &TaxClient{c: c}
}

// TaxQuery — window + optional coin filter for the spot / p2p records.
// StartTimeMs and EndTimeMs are required by the venue.
type TaxQuery struct {
	Coin        string
	StartTimeMs int64
	EndTimeMs   int64
}

// FuturesTaxQuery — window + optional productType / marginCoin filter.
type FuturesTaxQuery struct {
	ProductType string
	MarginCoin  string
	StartTimeMs int64
	EndTimeMs   int64
}

// MarginTaxQuery — window + optional marginType / coin filter.
type MarginTaxQuery struct {
	MarginType  string
	Coin        string
	StartTimeMs int64
	EndTimeMs   int64
}

// fetchTax runs one signed tax GET and returns the raw rows + the next
// idLessThan cursor (the last row's id, or "" to stop).
func (t *TaxClient) fetchTax(ctx context.Context, path string, base url.Values, idLessThan string, limit int, scope string, dst any, lastID func() string) (string, error) {
	var query url.Values = url.Values{}
	for k, vs := range base {
		query[k] = vs
	}
	query.Set("limit", strconv.Itoa(limit))
	if idLessThan != "" {
		query.Set("idLessThan", idLessThan)
	}
	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	if err = resp.UnmarshalData(dst); err != nil {
		return "", errParse(scope, err)
	}
	return lastID(), nil
}

func requireWindow(scope string, start, end int64) error {
	if start <= 0 || end <= 0 {
		return errInvalid(scope, "startTime and endTime are required")
	}
	return nil
}

// ---------------------------------------------------------------------
// GetSpotRecords — tax/spot-record.
// ---------------------------------------------------------------------

type spotTaxRow struct {
	ID          string `json:"id"`
	Coin        string `json:"coin"`
	SpotTaxType string `json:"spotTaxType"`
	Amount      string `json:"amount"`
	Fee         string `json:"fee"`
	Balance     string `json:"balance"`
	TS          string `json:"ts"`
}

// GetSpotRecords lists spot tax transaction records in the window.
func (t *TaxClient) GetSpotRecords(ctx context.Context, q TaxQuery) ([]commontypes.SpotTaxRecord, error) {
	if err := requireWindow("Tax.GetSpotRecords", q.StartTimeMs, q.EndTimeMs); err != nil {
		return nil, err
	}
	var base url.Values = url.Values{}
	base.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	base.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	if q.Coin != "" {
		base.Set("coin", q.Coin)
	}

	var rows []spotTaxRow
	var page []spotTaxRow
	var err error
	rows, err = paginateTax(ctx, "common.Tax.GetSpotRecords",
		func(idLessThan string, limit int) ([]spotTaxRow, string, error) {
			page = nil
			var next string
			var ferr error
			next, ferr = t.fetchTax(ctx, "/api/v2/tax/spot-record", base, idLessThan, limit, "Tax.GetSpotRecords", &page,
				func() string {
					if len(page) == 0 {
						return ""
					}
					return page[len(page)-1].ID
				})
			return page, next, ferr
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.SpotTaxRecord = make([]commontypes.SpotTaxRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec commontypes.SpotTaxRecord = commontypes.SpotTaxRecord{ID: rows[i].ID, Coin: rows[i].Coin, TaxType: rows[i].SpotTaxType}
		if err = decErr("Tax.GetSpotRecords", &rec.Amount, rows[i].Amount); err != nil {
			return nil, err
		}
		if err = decErr("Tax.GetSpotRecords", &rec.Fee, rows[i].Fee); err != nil {
			return nil, err
		}
		if err = decErr("Tax.GetSpotRecords", &rec.Balance, rows[i].Balance); err != nil {
			return nil, err
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetFuturesRecords — tax/future-record.
// ---------------------------------------------------------------------

type futuresTaxRow struct {
	ID            string `json:"id"`
	Symbol        string `json:"symbol"`
	MarginCoin    string `json:"marginCoin"`
	FutureTaxType string `json:"futureTaxType"`
	Amount        string `json:"amount"`
	Fee           string `json:"fee"`
	TS            string `json:"ts"`
}

// GetFuturesRecords lists futures tax transaction records in the window.
func (t *TaxClient) GetFuturesRecords(ctx context.Context, q FuturesTaxQuery) ([]commontypes.FuturesTaxRecord, error) {
	if err := requireWindow("Tax.GetFuturesRecords", q.StartTimeMs, q.EndTimeMs); err != nil {
		return nil, err
	}
	var base url.Values = url.Values{}
	base.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	base.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	if q.ProductType != "" {
		base.Set("productType", q.ProductType)
	}
	if q.MarginCoin != "" {
		base.Set("marginCoin", q.MarginCoin)
	}

	var rows []futuresTaxRow
	var page []futuresTaxRow
	var err error
	rows, err = paginateTax(ctx, "common.Tax.GetFuturesRecords",
		func(idLessThan string, limit int) ([]futuresTaxRow, string, error) {
			page = nil
			var next string
			var ferr error
			next, ferr = t.fetchTax(ctx, "/api/v2/tax/future-record", base, idLessThan, limit, "Tax.GetFuturesRecords", &page,
				func() string {
					if len(page) == 0 {
						return ""
					}
					return page[len(page)-1].ID
				})
			return page, next, ferr
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.FuturesTaxRecord = make([]commontypes.FuturesTaxRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec commontypes.FuturesTaxRecord = commontypes.FuturesTaxRecord{
			ID: rows[i].ID, Symbol: rows[i].Symbol, MarginCoin: rows[i].MarginCoin, TaxType: rows[i].FutureTaxType,
		}
		if err = decErr("Tax.GetFuturesRecords", &rec.Amount, rows[i].Amount); err != nil {
			return nil, err
		}
		if err = decErr("Tax.GetFuturesRecords", &rec.Fee, rows[i].Fee); err != nil {
			return nil, err
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetMarginRecords — tax/margin-record.
// ---------------------------------------------------------------------

type marginTaxRow struct {
	ID            string `json:"id"`
	Coin          string `json:"coin"`
	MarginTaxType string `json:"marginTaxType"`
	Amount        string `json:"amount"`
	Fee           string `json:"fee"`
	Total         string `json:"total"`
	Symbol        string `json:"symbol"`
	TS            string `json:"ts"`
}

// GetMarginRecords lists margin tax transaction records in the window.
func (t *TaxClient) GetMarginRecords(ctx context.Context, q MarginTaxQuery) ([]commontypes.MarginTaxRecord, error) {
	if err := requireWindow("Tax.GetMarginRecords", q.StartTimeMs, q.EndTimeMs); err != nil {
		return nil, err
	}
	var base url.Values = url.Values{}
	base.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	base.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	if q.MarginType != "" {
		base.Set("marginType", q.MarginType)
	}
	if q.Coin != "" {
		base.Set("coin", q.Coin)
	}

	var rows []marginTaxRow
	var page []marginTaxRow
	var err error
	rows, err = paginateTax(ctx, "common.Tax.GetMarginRecords",
		func(idLessThan string, limit int) ([]marginTaxRow, string, error) {
			page = nil
			var next string
			var ferr error
			next, ferr = t.fetchTax(ctx, "/api/v2/tax/margin-record", base, idLessThan, limit, "Tax.GetMarginRecords", &page,
				func() string {
					if len(page) == 0 {
						return ""
					}
					return page[len(page)-1].ID
				})
			return page, next, ferr
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.MarginTaxRecord = make([]commontypes.MarginTaxRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec commontypes.MarginTaxRecord = commontypes.MarginTaxRecord{
			ID: rows[i].ID, Coin: rows[i].Coin, Symbol: rows[i].Symbol, TaxType: rows[i].MarginTaxType,
		}
		if err = decErr("Tax.GetMarginRecords", &rec.Amount, rows[i].Amount); err != nil {
			return nil, err
		}
		if err = decErr("Tax.GetMarginRecords", &rec.Fee, rows[i].Fee); err != nil {
			return nil, err
		}
		if err = decErr("Tax.GetMarginRecords", &rec.Total, rows[i].Total); err != nil {
			return nil, err
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetP2PRecords — tax/p2p-record.
// ---------------------------------------------------------------------

type p2pTaxRow struct {
	ID         string `json:"id"`
	Coin       string `json:"coin"`
	P2PTaxType string `json:"p2pTaxType"`
	Total      string `json:"total"`
	TS         string `json:"ts"`
}

// GetP2PRecords lists P2P tax transaction records in the window.
func (t *TaxClient) GetP2PRecords(ctx context.Context, q TaxQuery) ([]commontypes.P2PTaxRecord, error) {
	if err := requireWindow("Tax.GetP2PRecords", q.StartTimeMs, q.EndTimeMs); err != nil {
		return nil, err
	}
	var base url.Values = url.Values{}
	base.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	base.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	if q.Coin != "" {
		base.Set("coin", q.Coin)
	}

	var rows []p2pTaxRow
	var page []p2pTaxRow
	var err error
	rows, err = paginateTax(ctx, "common.Tax.GetP2PRecords",
		func(idLessThan string, limit int) ([]p2pTaxRow, string, error) {
			page = nil
			var next string
			var ferr error
			next, ferr = t.fetchTax(ctx, "/api/v2/tax/p2p-record", base, idLessThan, limit, "Tax.GetP2PRecords", &page,
				func() string {
					if len(page) == 0 {
						return ""
					}
					return page[len(page)-1].ID
				})
			return page, next, ferr
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.P2PTaxRecord = make([]commontypes.P2PTaxRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec commontypes.P2PTaxRecord = commontypes.P2PTaxRecord{ID: rows[i].ID, Coin: rows[i].Coin, TaxType: rows[i].P2PTaxType}
		if err = decErr("Tax.GetP2PRecords", &rec.Total, rows[i].Total); err != nil {
			return nil, err
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// paginateTax is a thin alias over bgcommon.PaginateByCursor to keep the
// per-endpoint call sites short.
func paginateTax[T any](ctx context.Context, label string, fetch func(idLessThan string, limit int) ([]T, string, error)) ([]T, error) {
	return bgcommon.PaginateByCursor(ctx, label, fetch)
}
