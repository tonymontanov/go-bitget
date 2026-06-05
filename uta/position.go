/*
FILE: uta/position.go

DESCRIPTION:
Position sub-client — the V3 UTA positions surface: current positions,
position history, the pre-trade max-open-available probe and the ADL
ranking. All calls are SIGNED.

Positions carry holdMode / posSide so the same methods serve one-way and
hedge accounts. Request params verified against the Bitget V3 docs and the
tiagosiebler reference client.
*/

package uta

import (
	"context"
	"net/url"

	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// PositionClient — positions sub-client.
type PositionClient struct {
	c *Client
}

func newPositionClient(c *Client) *PositionClient {
	return &PositionClient{c: c}
}

// ---------------------------------------------------------------------
// GetCurrentPositions — position/current-position.
// ---------------------------------------------------------------------

type currentPositionRow struct {
	Category         string `json:"category"`
	Symbol           string `json:"symbol"`
	MarginCoin       string `json:"marginCoin"`
	HoldMode         string `json:"holdMode"`
	PosSide          string `json:"posSide"`
	MarginMode       string `json:"marginMode"`
	PositionBalance  string `json:"positionBalance"`
	Available        string `json:"available"`
	Frozen           string `json:"frozen"`
	Total            string `json:"total"`
	Leverage         string `json:"leverage"`
	CurRealisedPnl   string `json:"curRealisedPnl"`
	AvgPrice         string `json:"avgPrice"`
	PositionStatus   string `json:"positionStatus"`
	UnrealisedPnl    string `json:"unrealisedPnl"`
	LiquidationPrice string `json:"liquidationPrice"`
	MMR              string `json:"mmr"`
	ProfitRate       string `json:"profitRate"`
	MarkPrice        string `json:"markPrice"`
	BreakEvenPrice   string `json:"breakEvenPrice"`
	TotalFunding     string `json:"totalFunding"`
	OpenFeeTotal     string `json:"openFeeTotal"`
	CloseFeeTotal    string `json:"closeFeeTotal"`
	CreatedTime      string `json:"createdTime"`
	UpdatedTime      string `json:"updatedTime"`
}

type currentPositionPage struct {
	List []currentPositionRow `json:"list"`
}

// GetCurrentPositions returns the open positions for a futures category.
// category is required; symbol / posSide narrow the result.
func (p *PositionClient) GetCurrentPositions(ctx context.Context, category utatypes.Category, symbol, posSide string) ([]utatypes.CurrentPosition, error) {
	if category == "" {
		return nil, errInvalid("Position.GetCurrentPositions", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	if posSide != "" {
		query.Set("posSide", posSide)
	}
	var page currentPositionPage
	if err := p.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/position/current-position", Query: query, Meta: queryMeta(),
	}, "Position.GetCurrentPositions", &page); err != nil {
		return nil, err
	}
	var out []utatypes.CurrentPosition = make([]utatypes.CurrentPosition, 0, len(page.List))
	var i int
	for i = 0; i < len(page.List); i++ {
		var r = page.List[i]
		var pos utatypes.CurrentPosition = utatypes.CurrentPosition{
			Category: r.Category, Symbol: r.Symbol, MarginCoin: r.MarginCoin, HoldMode: r.HoldMode,
			PosSide: r.PosSide, MarginMode: r.MarginMode, PositionStatus: r.PositionStatus,
			CreatedTime: i64(r.CreatedTime), UpdatedTime: i64(r.UpdatedTime),
		}
		if err := decMany("Position.GetCurrentPositions", []decPair{
			{&pos.PositionBalance, r.PositionBalance}, {&pos.Available, r.Available}, {&pos.Frozen, r.Frozen},
			{&pos.Total, r.Total}, {&pos.Leverage, r.Leverage}, {&pos.CurRealisedPnl, r.CurRealisedPnl},
			{&pos.AvgPrice, r.AvgPrice}, {&pos.UnrealisedPnl, r.UnrealisedPnl}, {&pos.LiquidationPrice, r.LiquidationPrice},
			{&pos.MMR, r.MMR}, {&pos.ProfitRate, r.ProfitRate}, {&pos.MarkPrice, r.MarkPrice},
			{&pos.BreakEvenPrice, r.BreakEvenPrice}, {&pos.TotalFunding, r.TotalFunding},
			{&pos.OpenFeeTotal, r.OpenFeeTotal}, {&pos.CloseFeeTotal, r.CloseFeeTotal},
		}); err != nil {
			return nil, err
		}
		out = append(out, pos)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetPositionHistory — position/history-position.
// ---------------------------------------------------------------------

type positionHistoryRow struct {
	PositionID     string `json:"positionId"`
	Category       string `json:"category"`
	Symbol         string `json:"symbol"`
	MarginCoin     string `json:"marginCoin"`
	HoldMode       string `json:"holdMode"`
	PosSide        string `json:"posSide"`
	MarginMode     string `json:"marginMode"`
	OpenPriceAvg   string `json:"openPriceAvg"`
	ClosePriceAvg  string `json:"closePriceAvg"`
	OpenTotalPos   string `json:"openTotalPos"`
	CloseTotalPos  string `json:"closeTotalPos"`
	CumRealisedPnl string `json:"cumRealisedPnl"`
	NetProfit      string `json:"netProfit"`
	TotalFunding   string `json:"totalFunding"`
	OpenFeeTotal   string `json:"openFeeTotal"`
	CloseFeeTotal  string `json:"closeFeeTotal"`
	CreatedTime    string `json:"createdTime"`
	UpdatedTime    string `json:"updatedTime"`
}

type positionHistoryPage struct {
	List   []positionHistoryRow `json:"list"`
	Cursor string               `json:"cursor"`
}

// GetPositionHistory returns one page of closed positions plus the next
// cursor. Category is required (futures only).
func (p *PositionClient) GetPositionHistory(ctx context.Context, q OrdersQuery) ([]utatypes.PositionHistory, string, error) {
	var query url.Values
	var err error
	if query, err = q.values(true, "Position.GetPositionHistory"); err != nil {
		return nil, "", err
	}
	var page positionHistoryPage
	if err = p.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/position/history-position", Query: query, Meta: queryMeta(),
	}, "Position.GetPositionHistory", &page); err != nil {
		return nil, "", err
	}
	var out []utatypes.PositionHistory = make([]utatypes.PositionHistory, 0, len(page.List))
	var i int
	for i = 0; i < len(page.List); i++ {
		var r = page.List[i]
		var ph utatypes.PositionHistory = utatypes.PositionHistory{
			PositionID: r.PositionID, Category: r.Category, Symbol: r.Symbol, MarginCoin: r.MarginCoin,
			HoldMode: r.HoldMode, PosSide: r.PosSide, MarginMode: r.MarginMode,
			CreatedTime: i64(r.CreatedTime), UpdatedTime: i64(r.UpdatedTime),
		}
		if err = decMany("Position.GetPositionHistory", []decPair{
			{&ph.OpenPriceAvg, r.OpenPriceAvg}, {&ph.ClosePriceAvg, r.ClosePriceAvg},
			{&ph.OpenTotalPos, r.OpenTotalPos}, {&ph.CloseTotalPos, r.CloseTotalPos},
			{&ph.CumRealisedPnl, r.CumRealisedPnl}, {&ph.NetProfit, r.NetProfit}, {&ph.TotalFunding, r.TotalFunding},
			{&ph.OpenFeeTotal, r.OpenFeeTotal}, {&ph.CloseFeeTotal, r.CloseFeeTotal},
		}); err != nil {
			return nil, "", err
		}
		out = append(out, ph)
	}
	return out, page.Cursor, nil
}

// ---------------------------------------------------------------------
// GetMaxOpenAvailable — account/max-open-available (POST).
// ---------------------------------------------------------------------

// MaxOpenAvailableRequest — parameters for GetMaxOpenAvailable. Category,
// Symbol, OrderType and Side are required; Price / Size refine the probe.
type MaxOpenAvailableRequest struct {
	Category  utatypes.Category `json:"category"`
	Symbol    string            `json:"symbol"`
	OrderType string            `json:"orderType"`
	Side      string            `json:"side"`
	Price     string            `json:"price,omitempty"`
	Size      string            `json:"size,omitempty"`
}

type maxOpenAvailableRow struct {
	Available    string `json:"available"`
	MaxOpen      string `json:"maxOpen"`
	BuyOpenCost  string `json:"buyOpenCost"`
	SellOpenCost string `json:"sellOpenCost"`
	MaxBuyOpen   string `json:"maxBuyOpen"`
	MaxSellOpen  string `json:"maxSellOpen"`
}

// GetMaxOpenAvailable probes how much can be opened for a prospective
// order. Category / Symbol / OrderType / Side are required.
func (p *PositionClient) GetMaxOpenAvailable(ctx context.Context, req MaxOpenAvailableRequest) (utatypes.MaxOpenAvailable, error) {
	var out utatypes.MaxOpenAvailable
	switch {
	case req.Category == "":
		return out, errInvalid("Position.GetMaxOpenAvailable", "category is required")
	case req.Symbol == "":
		return out, errInvalid("Position.GetMaxOpenAvailable", "symbol is required")
	case req.OrderType == "":
		return out, errInvalid("Position.GetMaxOpenAvailable", "orderType is required")
	case req.Side == "":
		return out, errInvalid("Position.GetMaxOpenAvailable", "side is required")
	}
	var row maxOpenAvailableRow
	if err := p.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/account/max-open-available", Body: req, Meta: queryMeta(),
	}, "Position.GetMaxOpenAvailable", &row); err != nil {
		return out, err
	}
	if err := decMany("Position.GetMaxOpenAvailable", []decPair{
		{&out.Available, row.Available}, {&out.MaxOpen, row.MaxOpen},
		{&out.BuyOpenCost, row.BuyOpenCost}, {&out.SellOpenCost, row.SellOpenCost},
		{&out.MaxBuyOpen, row.MaxBuyOpen}, {&out.MaxSellOpen, row.MaxSellOpen},
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAdlRank — position/adlRank.
// ---------------------------------------------------------------------

type adlRankRow struct {
	Symbol     string `json:"symbol"`
	MarginCoin string `json:"marginCoin"`
	AdlRank    string `json:"adlRank"`
	HoldSide   string `json:"holdSide"`
}

// GetAdlRank returns the auto-deleveraging ranking of the open positions.
func (p *PositionClient) GetAdlRank(ctx context.Context) ([]utatypes.PositionAdlRank, error) {
	var rows []adlRankRow
	if err := p.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/position/adlRank", Meta: queryMeta(),
	}, "Position.GetAdlRank", &rows); err != nil {
		return nil, err
	}
	var out []utatypes.PositionAdlRank = make([]utatypes.PositionAdlRank, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		out = append(out, utatypes.PositionAdlRank{
			Symbol: rows[i].Symbol, MarginCoin: rows[i].MarginCoin, AdlRank: rows[i].AdlRank, HoldSide: rows[i].HoldSide,
		})
	}
	return out, nil
}
