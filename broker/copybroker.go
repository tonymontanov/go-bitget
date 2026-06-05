/*
FILE: broker/copybroker.go

DESCRIPTION:
CopyBroker sub-client — copy-trading broker reads
(/api/v2/copy/mix-broker/{query-traders,query-history-traces,
query-current-traces}). All reads; require the account to be an approved
copy-trading broker. pageNo/pageSize paginated, fully walked.

Upstream references type these as `any`; the row shapes follow the
documented copy-trading trader / order-trace schema and decode leniently.

Request params verified against the Bitget V2 changelog (legacy
trace/traderList + report/order/{history,current}List mapping).
*/

package broker

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	brokertypes "github.com/tonymontanov/go-bitget/v2/broker/types"
)

// CopyBrokerClient — copy-trading broker reads sub-client.
type CopyBrokerClient struct {
	c *Client
}

func newCopyBrokerClient(c *Client) *CopyBrokerClient {
	return &CopyBrokerClient{c: c}
}

const copyBrokerPageSize = 50 // venue max for these endpoints

// getList runs a GET pageNo/pageSize call returning a JSON array. dst must
// be a pointer to a slice of row structs.
func (b *CopyBrokerClient) getList(ctx context.Context, path string, query url.Values, dst any, scope string) error {
	var resp rest.Response
	var err error
	resp, _, err = b.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return err
	}
	if err = resp.UnmarshalData(dst); err != nil {
		return errParse(scope, err)
	}
	return nil
}

// ---------------------------------------------------------------------
// GetTraders — copy/mix-broker/query-traders.
// ---------------------------------------------------------------------

type brokerTraderRow struct {
	TraderID               string   `json:"traderId"`
	TraderName             string   `json:"traderName"`
	CertificationType      string   `json:"certificationType"`
	MaxFollowLimit         string   `json:"maxFollowLimit"`
	BGBMaxFollowLimit      string   `json:"bgbMaxFollowLimit"`
	FollowCount            string   `json:"followCount"`
	BGBFollowCount         string   `json:"bgbFollowCount"`
	TraceTotalMarginAmount string   `json:"traceTotalMarginAmount"`
	TraceTotalNetProfit    string   `json:"traceTotalNetProfit"`
	TraceTotalProfit       string   `json:"traceTotalProfit"`
	CurrentTradingPairs    []string `json:"currentTradingPairs"`
	FollowerTime           string   `json:"followerTime"`
}

// GetTraders lists the traders visible under the broker with their follow
// limits and aggregate copy stats. Fully walks pageNo/pageSize.
func (b *CopyBrokerClient) GetTraders(ctx context.Context, q BrokerReportQuery) ([]brokertypes.BrokerTrader, error) {
	var rows []brokerTraderRow
	var err error
	rows, err = paginateByPageNo(ctx, copyBrokerPageSize,
		func(pageNo, pageSize int) ([]brokerTraderRow, error) {
			var query url.Values = url.Values{}
			q.apply(query)
			query.Set("pageNo", strconv.Itoa(pageNo))
			query.Set("pageSize", strconv.Itoa(pageSize))
			var page []brokerTraderRow
			if ferr := b.getList(ctx, "/api/v2/copy/mix-broker/query-traders", query, &page, "CopyBroker.GetTraders"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.BrokerTrader = make([]brokertypes.BrokerTrader, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var tr brokertypes.BrokerTrader = brokertypes.BrokerTrader{
			TraderID:            r.TraderID,
			TraderName:          r.TraderName,
			CertificationType:   r.CertificationType,
			CurrentTradingPairs: r.CurrentTradingPairs,
		}
		tr.MaxFollowLimit, _ = bgcommon.ParseInt64OrZero(r.MaxFollowLimit)
		tr.BGBMaxFollowLimit, _ = bgcommon.ParseInt64OrZero(r.BGBMaxFollowLimit)
		tr.FollowCount, _ = bgcommon.ParseInt64OrZero(r.FollowCount)
		tr.BGBFollowCount, _ = bgcommon.ParseInt64OrZero(r.BGBFollowCount)
		tr.FollowerTimeMs, _ = bgcommon.ParseInt64OrZero(r.FollowerTime)
		if err = decAll("CopyBroker.GetTraders",
			decPair{&tr.TraceTotalMarginAmount, r.TraceTotalMarginAmount},
			decPair{&tr.TraceTotalNetProfit, r.TraceTotalNetProfit},
			decPair{&tr.TraceTotalProfit, r.TraceTotalProfit},
		); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistoricalOrders / GetPendingOrders — copy/mix-broker/query-{history,current}-traces.
// ---------------------------------------------------------------------

type brokerTraceRow struct {
	TrackingNo    string `json:"trackingNo"`
	TraderID      string `json:"traderId"`
	TraderName    string `json:"traderName"`
	Symbol        string `json:"symbol"`
	PosSide       string `json:"posSide"`
	HoldSide      string `json:"holdSide"`
	OpenOrderID   string `json:"openOrderId"`
	CloseOrderID  string `json:"closeOrderId"`
	OpenLeverage  string `json:"openLeverage"`
	OpenPriceAvg  string `json:"openPriceAvg"`
	OpenSize      string `json:"openSize"`
	OpenFee       string `json:"openFee"`
	OpenTime      string `json:"openTime"`
	ClosePriceAvg string `json:"closePriceAvg"`
	CloseSize     string `json:"closeSize"`
	CloseFee      string `json:"closeFee"`
	CloseTime     string `json:"closeTime"`
	NetProfit     string `json:"netProfit"`
	ProfitRate    string `json:"profitRate"`
	AchievedPL    string `json:"achievedProfits"`
}

func (r brokerTraceRow) toDomain(scope string) (brokertypes.BrokerOrderTrace, error) {
	var t brokertypes.BrokerOrderTrace = brokertypes.BrokerOrderTrace{
		TrackingNo:   r.TrackingNo,
		TraderID:     r.TraderID,
		TraderName:   r.TraderName,
		Symbol:       r.Symbol,
		OpenOrderID:  r.OpenOrderID,
		CloseOrderID: r.CloseOrderID,
	}
	t.PosSide = r.PosSide
	if t.PosSide == "" {
		t.PosSide = r.HoldSide
	}
	t.OpenTimeMs, _ = bgcommon.ParseInt64OrZero(r.OpenTime)
	t.CloseTimeMs, _ = bgcommon.ParseInt64OrZero(r.CloseTime)
	var err error
	if err = decAll(scope,
		decPair{&t.OpenLeverage, r.OpenLeverage},
		decPair{&t.OpenPriceAvg, r.OpenPriceAvg},
		decPair{&t.OpenSize, r.OpenSize},
		decPair{&t.OpenFee, r.OpenFee},
		decPair{&t.ClosePriceAvg, r.ClosePriceAvg},
		decPair{&t.CloseSize, r.CloseSize},
		decPair{&t.CloseFee, r.CloseFee},
		decPair{&t.NetProfit, r.NetProfit},
		decPair{&t.ProfitRate, r.ProfitRate},
		decPair{&t.AchievedPL, r.AchievedPL},
	); err != nil {
		return t, err
	}
	return t, nil
}

// GetHistoricalOrders lists the broker's closed copy-order traces. Fully
// walks pageNo/pageSize.
func (b *CopyBrokerClient) GetHistoricalOrders(ctx context.Context, q BrokerReportQuery) ([]brokertypes.BrokerOrderTrace, error) {
	return b.traces(ctx, "/api/v2/copy/mix-broker/query-history-traces", q, true, "CopyBroker.GetHistoricalOrders")
}

// GetPendingOrders lists the broker's live copy-order traces. The
// query-current-traces endpoint ignores the time window. Fully walks
// pageNo/pageSize.
func (b *CopyBrokerClient) GetPendingOrders(ctx context.Context, q BrokerReportQuery) ([]brokertypes.BrokerOrderTrace, error) {
	return b.traces(ctx, "/api/v2/copy/mix-broker/query-current-traces", q, false, "CopyBroker.GetPendingOrders")
}

// traces backs GetHistoricalOrders / GetPendingOrders. withWindow controls
// whether the start/end time filters are sent (current-traces ignores them).
func (b *CopyBrokerClient) traces(ctx context.Context, path string, q BrokerReportQuery, withWindow bool, scope string) ([]brokertypes.BrokerOrderTrace, error) {
	var rows []brokerTraceRow
	var err error
	rows, err = paginateByPageNo(ctx, copyBrokerPageSize,
		func(pageNo, pageSize int) ([]brokerTraceRow, error) {
			var query url.Values = url.Values{}
			if withWindow {
				q.apply(query)
			}
			query.Set("pageNo", strconv.Itoa(pageNo))
			query.Set("pageSize", strconv.Itoa(pageSize))
			var page []brokerTraceRow
			if ferr := b.getList(ctx, path, query, &page, scope); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.BrokerOrderTrace = make([]brokertypes.BrokerOrderTrace, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var t brokertypes.BrokerOrderTrace
		if t, err = rows[i].toDomain(scope); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
