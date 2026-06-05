/*
FILE: uta/public_extras.go

DESCRIPTION:
Public sub-client EXTRAS — the remaining UNSIGNED V3 market-data reads:
fee groups, score weights, proof-of-reserves, open interest, funding
rates, risk reserve (+ hourly + all), discount rate, margin loans,
position tiers, OI limits and index components.

Request params verified against the Bitget V3 docs and the tiagosiebler
reference client.
*/

package uta

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// getJSON runs an unsigned market GET with the given query and decodes the
// envelope data into dst.
func (p *PublicClient) getJSON(ctx context.Context, path, scope string, query url.Values, dst any) error {
	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: false,
		Meta:   marketMeta(),
	})
	if err != nil {
		return err
	}
	if err = resp.UnmarshalData(dst); err != nil {
		return errParse(scope, err)
	}
	return nil
}

// decMany parses a batch of (dst, raw) pairs, short-circuiting on error.
func decMany(scope string, pairs []decPair) error {
	var i int
	for i = 0; i < len(pairs); i++ {
		var err error
		if *pairs[i].dst, err = parseDec(pairs[i].raw); err != nil {
			return errParse(scope, err)
		}
	}
	return nil
}

type decPair struct {
	dst *decimal.Decimal
	raw string
}

func parseDec(raw string) (decimal.Decimal, error) {
	if raw == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(raw)
}

// ---------------------------------------------------------------------
// GetMarketFeeGroup — market/fee-group.
// ---------------------------------------------------------------------

type feeGroupRow struct {
	Category  string `json:"category"`
	Group     string `json:"group"`
	LabelList []struct {
		Weight  string   `json:"weight"`
		Label   string   `json:"label"`
		Symbols []string `json:"symbols"`
	} `json:"labelList"`
	TierList []struct {
		Level        string `json:"level"`
		MakerFeeRate string `json:"makerFeeRate"`
	} `json:"tierList"`
}

// GetMarketFeeGroup returns the market-maker fee tiers / grouping.
// category ("SPOT" | "FUTURES") is required; group ("GROUP_A"...) optional.
func (p *PublicClient) GetMarketFeeGroup(ctx context.Context, category, group string) (utatypes.MarketFeeGroup, error) {
	var out utatypes.MarketFeeGroup
	if category == "" {
		return out, errInvalid("Public.GetMarketFeeGroup", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", category)
	if group != "" {
		query.Set("group", group)
	}
	var row feeGroupRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/fee-group", "Public.GetMarketFeeGroup", query, &row); err != nil {
		return out, err
	}
	out.Category = row.Category
	out.Group = row.Group
	var i int
	out.Labels = make([]utatypes.FeeGroupLabel, 0, len(row.LabelList))
	for i = 0; i < len(row.LabelList); i++ {
		var lbl utatypes.FeeGroupLabel = utatypes.FeeGroupLabel{Label: row.LabelList[i].Label, Symbols: row.LabelList[i].Symbols}
		if err = decMany("Public.GetMarketFeeGroup", []decPair{{&lbl.Weight, row.LabelList[i].Weight}}); err != nil {
			return out, err
		}
		out.Labels = append(out.Labels, lbl)
	}
	out.Tiers = make([]utatypes.FeeGroupTier, 0, len(row.TierList))
	for i = 0; i < len(row.TierList); i++ {
		var tier utatypes.FeeGroupTier = utatypes.FeeGroupTier{Level: row.TierList[i].Level}
		if err = decMany("Public.GetMarketFeeGroup", []decPair{{&tier.MakerFeeRate, row.TierList[i].MakerFeeRate}}); err != nil {
			return out, err
		}
		out.Tiers = append(out.Tiers, tier)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetScoreWeights — market/score-weights.
// ---------------------------------------------------------------------

type scoreWeightRow struct {
	Category       string `json:"category"`
	Label          string `json:"label"`
	Symbol         string `json:"symbol"`
	RequiredSpread string `json:"requiredSpread"`
	MinMakerVolume string `json:"minMakerVolume"`
	Weight         string `json:"weight"`
}

// GetScoreWeights returns the market-maker score weights. category
// ("SPOT" | "FUTURES") is optional.
func (p *PublicClient) GetScoreWeights(ctx context.Context, category string) ([]utatypes.ScoreWeight, error) {
	var query url.Values = url.Values{}
	if category != "" {
		query.Set("category", category)
	}
	var rows []scoreWeightRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/score-weights", "Public.GetScoreWeights", query, &rows); err != nil {
		return nil, err
	}
	var out []utatypes.ScoreWeight = make([]utatypes.ScoreWeight, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var s utatypes.ScoreWeight = utatypes.ScoreWeight{Category: rows[i].Category, Label: rows[i].Label, Symbol: rows[i].Symbol}
		if err = decMany("Public.GetScoreWeights", []decPair{
			{&s.RequiredSpread, rows[i].RequiredSpread}, {&s.MinMakerVolume, rows[i].MinMakerVolume}, {&s.Weight, rows[i].Weight},
		}); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetProofOfReserves — market/proof-of-reserves.
// ---------------------------------------------------------------------

type proofOfReservesRow struct {
	MerkleRootHash    string `json:"merkleRootHash"`
	TotalReserveRatio string `json:"totalReserveRatio"`
	List              []struct {
		Coin           string `json:"coin"`
		UserAssets     string `json:"userAssets"`
		PlatformAssets string `json:"platformAssets"`
		ReserveRatio   string `json:"reserveRatio"`
	} `json:"list"`
}

// GetProofOfReserves returns the platform proof-of-reserves snapshot.
func (p *PublicClient) GetProofOfReserves(ctx context.Context) (utatypes.ProofOfReserves, error) {
	var out utatypes.ProofOfReserves
	var row proofOfReservesRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/proof-of-reserves", "Public.GetProofOfReserves", url.Values{}, &row); err != nil {
		return out, err
	}
	out.MerkleRootHash = row.MerkleRootHash
	if err = decMany("Public.GetProofOfReserves", []decPair{{&out.TotalReserveRatio, row.TotalReserveRatio}}); err != nil {
		return out, err
	}
	var i int
	out.List = make([]utatypes.ProofOfReservesItem, 0, len(row.List))
	for i = 0; i < len(row.List); i++ {
		var it utatypes.ProofOfReservesItem = utatypes.ProofOfReservesItem{Coin: row.List[i].Coin}
		if err = decMany("Public.GetProofOfReserves", []decPair{
			{&it.UserAssets, row.List[i].UserAssets}, {&it.PlatformAssets, row.List[i].PlatformAssets}, {&it.ReserveRatio, row.List[i].ReserveRatio},
		}); err != nil {
			return out, err
		}
		out.List = append(out.List, it)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetOpenInterest — market/open-interest.
// ---------------------------------------------------------------------

type openInterestRow struct {
	TS   string `json:"ts"`
	List []struct {
		Symbol       string `json:"symbol"`
		OpenInterest string `json:"openInterest"`
	} `json:"list"`
}

// GetOpenInterest returns the open interest for a futures category. symbol
// is optional. category is required (futures only).
func (p *PublicClient) GetOpenInterest(ctx context.Context, category utatypes.Category, symbol string) (utatypes.OpenInterest, error) {
	var out utatypes.OpenInterest
	if category == "" {
		return out, errInvalid("Public.GetOpenInterest", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	var row openInterestRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/open-interest", "Public.GetOpenInterest", query, &row); err != nil {
		return out, err
	}
	out.TimeMs = i64(row.TS)
	var i int
	out.List = make([]utatypes.OpenInterestItem, 0, len(row.List))
	for i = 0; i < len(row.List); i++ {
		var it utatypes.OpenInterestItem = utatypes.OpenInterestItem{Symbol: row.List[i].Symbol}
		if err = decMany("Public.GetOpenInterest", []decPair{{&it.OpenInterest, row.List[i].OpenInterest}}); err != nil {
			return out, err
		}
		out.List = append(out.List, it)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCurrentFundingRate — market/current-fund-rate.
// ---------------------------------------------------------------------

type currentFundingRateRow struct {
	Symbol              string `json:"symbol"`
	FundingRate         string `json:"fundingRate"`
	FundingRateInterval string `json:"fundingRateInterval"`
	NextUpdate          string `json:"nextUpdate"`
	MinFundingRate      string `json:"minFundingRate"`
	MaxFundingRate      string `json:"maxFundingRate"`
}

// GetCurrentFundingRate returns the current funding rate for a symbol.
// symbol is required.
func (p *PublicClient) GetCurrentFundingRate(ctx context.Context, symbol string) (utatypes.CurrentFundingRate, error) {
	var out utatypes.CurrentFundingRate
	if symbol == "" {
		return out, errInvalid("Public.GetCurrentFundingRate", "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("symbol", symbol)
	var rows []currentFundingRateRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/current-fund-rate", "Public.GetCurrentFundingRate", query, &rows); err != nil {
		return out, err
	}
	if len(rows) == 0 {
		return out, nil
	}
	var r = rows[0]
	out.Symbol = r.Symbol
	out.FundingRateInterval = r.FundingRateInterval
	out.NextUpdateMs = i64(r.NextUpdate)
	if err = decMany("Public.GetCurrentFundingRate", []decPair{
		{&out.FundingRate, r.FundingRate}, {&out.MinFundingRate, r.MinFundingRate}, {&out.MaxFundingRate, r.MaxFundingRate},
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistoryFundingRate — market/history-fund-rate.
// ---------------------------------------------------------------------

type historyFundingRateRow struct {
	Symbol               string `json:"symbol"`
	FundingRate          string `json:"fundingRate"`
	FundingRateTimestamp string `json:"fundingRateTimestamp"`
}

// GetHistoryFundingRate returns one page of historical funding rates.
// category and symbol are required; cursor / limit are optional (the
// caller pages by passing the cursor of the next request).
func (p *PublicClient) GetHistoryFundingRate(ctx context.Context, category utatypes.Category, symbol, cursor string, limit int) ([]utatypes.HistoryFundingRate, error) {
	switch {
	case category == "":
		return nil, errInvalid("Public.GetHistoryFundingRate", "category is required")
	case symbol == "":
		return nil, errInvalid("Public.GetHistoryFundingRate", "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	query.Set("symbol", symbol)
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var rows []historyFundingRateRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/history-fund-rate", "Public.GetHistoryFundingRate", query, &rows); err != nil {
		return nil, err
	}
	var out []utatypes.HistoryFundingRate = make([]utatypes.HistoryFundingRate, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var h utatypes.HistoryFundingRate = utatypes.HistoryFundingRate{Symbol: rows[i].Symbol, TimeMs: i64(rows[i].FundingRateTimestamp)}
		if err = decMany("Public.GetHistoryFundingRate", []decPair{{&h.FundingRate, rows[i].FundingRate}}); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetRiskReserve / GetRiskReserveHour — market/risk-reserve[-hour].
// ---------------------------------------------------------------------

type riskReserveRow struct {
	TotalBalance       string `json:"totalBalance"`
	Coin               string `json:"coin"`
	RiskReserveRecords []struct {
		Balance string `json:"balance"`
		Amount  string `json:"amount"`
		TS      string `json:"ts"`
		Type    string `json:"type"`
	} `json:"riskReserveRecords"`
}

func (p *PublicClient) getRiskReserve(ctx context.Context, path, scope string, category utatypes.Category, symbol, marginCoin string) (utatypes.RiskReserve, error) {
	var out utatypes.RiskReserve
	switch {
	case category == "":
		return out, errInvalid(scope, "category is required")
	case symbol == "":
		return out, errInvalid(scope, "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	query.Set("symbol", symbol)
	if marginCoin != "" {
		query.Set("marginCoin", marginCoin)
	}
	var row riskReserveRow
	var err error
	if err = p.getJSON(ctx, path, scope, query, &row); err != nil {
		return out, err
	}
	out.Coin = row.Coin
	if err = decMany(scope, []decPair{{&out.TotalBalance, row.TotalBalance}}); err != nil {
		return out, err
	}
	var i int
	out.Records = make([]utatypes.RiskReserveRecord, 0, len(row.RiskReserveRecords))
	for i = 0; i < len(row.RiskReserveRecords); i++ {
		var rec utatypes.RiskReserveRecord = utatypes.RiskReserveRecord{TimeMs: i64(row.RiskReserveRecords[i].TS), Type: row.RiskReserveRecords[i].Type}
		if err = decMany(scope, []decPair{
			{&rec.Balance, row.RiskReserveRecords[i].Balance}, {&rec.Amount, row.RiskReserveRecords[i].Amount},
		}); err != nil {
			return out, err
		}
		out.Records = append(out.Records, rec)
	}
	return out, nil
}

// GetRiskReserve returns the insurance-fund balance series for a symbol.
// category and symbol are required; marginCoin is optional.
func (p *PublicClient) GetRiskReserve(ctx context.Context, category utatypes.Category, symbol, marginCoin string) (utatypes.RiskReserve, error) {
	return p.getRiskReserve(ctx, "/api/v3/market/risk-reserve", "Public.GetRiskReserve", category, symbol, marginCoin)
}

// GetRiskReserveHour returns the hourly insurance-fund balance series.
// category and symbol are required; marginCoin is optional.
func (p *PublicClient) GetRiskReserveHour(ctx context.Context, category utatypes.Category, symbol, marginCoin string) (utatypes.RiskReserve, error) {
	return p.getRiskReserve(ctx, "/api/v3/market/risk-reserve-hour", "Public.GetRiskReserveHour", category, symbol, marginCoin)
}

// ---------------------------------------------------------------------
// GetRiskReserveAll — market/risk-reserve-all.
// ---------------------------------------------------------------------

type riskReserveAllRow struct {
	List []struct {
		Symbols []string `json:"symbols"`
		Coin    string   `json:"coin"`
		Balance string   `json:"balance"`
	} `json:"list"`
}

// GetRiskReserveAll returns the aggregate insurance funds for a futures
// category. category is required.
func (p *PublicClient) GetRiskReserveAll(ctx context.Context, category utatypes.Category) ([]utatypes.RiskReserveAllItem, error) {
	if category == "" {
		return nil, errInvalid("Public.GetRiskReserveAll", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	var row riskReserveAllRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/risk-reserve-all", "Public.GetRiskReserveAll", query, &row); err != nil {
		return nil, err
	}
	var out []utatypes.RiskReserveAllItem = make([]utatypes.RiskReserveAllItem, 0, len(row.List))
	var i int
	for i = 0; i < len(row.List); i++ {
		var it utatypes.RiskReserveAllItem = utatypes.RiskReserveAllItem{Coin: row.List[i].Coin, Symbols: row.List[i].Symbols}
		if err = decMany("Public.GetRiskReserveAll", []decPair{{&it.Balance, row.List[i].Balance}}); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetDiscountRate — market/discount-rate.
// ---------------------------------------------------------------------

type discountRateRow struct {
	Coin string `json:"coin"`
	List []struct {
		TierStartValue string `json:"tierStartValue"`
		DiscountRate   string `json:"discountRate"`
	} `json:"list"`
}

// GetDiscountRate returns the collateral discount-rate curves for all
// coins.
func (p *PublicClient) GetDiscountRate(ctx context.Context) ([]utatypes.DiscountRate, error) {
	var rows []discountRateRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/discount-rate", "Public.GetDiscountRate", url.Values{}, &rows); err != nil {
		return nil, err
	}
	var out []utatypes.DiscountRate = make([]utatypes.DiscountRate, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var dr utatypes.DiscountRate = utatypes.DiscountRate{Coin: rows[i].Coin}
		var j int
		dr.List = make([]utatypes.DiscountRateTier, 0, len(rows[i].List))
		for j = 0; j < len(rows[i].List); j++ {
			var tier utatypes.DiscountRateTier
			if err = decMany("Public.GetDiscountRate", []decPair{
				{&tier.TierStartValue, rows[i].List[j].TierStartValue}, {&tier.DiscountRate, rows[i].List[j].DiscountRate},
			}); err != nil {
				return nil, err
			}
			dr.List = append(dr.List, tier)
		}
		out = append(out, dr)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetMarginLoans — market/margin-loans.
// ---------------------------------------------------------------------

type marginLoanRow struct {
	DailyInterest  string `json:"dailyInterest"`
	AnnualInterest string `json:"annualInterest"`
	Limit          string `json:"limit"`
}

// GetMarginLoans returns the margin-loan interest / limit for a coin.
// coin is required.
func (p *PublicClient) GetMarginLoans(ctx context.Context, coin string) (utatypes.MarginLoan, error) {
	var out utatypes.MarginLoan
	if coin == "" {
		return out, errInvalid("Public.GetMarginLoans", "coin is required")
	}
	var query url.Values = url.Values{}
	query.Set("coin", coin)
	var row marginLoanRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/margin-loans", "Public.GetMarginLoans", query, &row); err != nil {
		return out, err
	}
	if err = decMany("Public.GetMarginLoans", []decPair{
		{&out.DailyInterest, row.DailyInterest}, {&out.AnnualInterest, row.AnnualInterest}, {&out.Limit, row.Limit},
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetPositionTier — market/position-tier.
// ---------------------------------------------------------------------

type positionTierRow struct {
	Tier         string `json:"tier"`
	MinTierValue string `json:"minTierValue"`
	MaxTierValue string `json:"maxTierValue"`
	Leverage     string `json:"leverage"`
	MMR          string `json:"mmr"`
}

// GetPositionTier returns the position-tier (leverage / MMR) ladder.
// category is required; symbol / coin are optional.
func (p *PublicClient) GetPositionTier(ctx context.Context, category utatypes.Category, symbol, coin string) ([]utatypes.PositionTier, error) {
	if category == "" {
		return nil, errInvalid("Public.GetPositionTier", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	if coin != "" {
		query.Set("coin", coin)
	}
	var rows []positionTierRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/position-tier", "Public.GetPositionTier", query, &rows); err != nil {
		return nil, err
	}
	var out []utatypes.PositionTier = make([]utatypes.PositionTier, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var pt utatypes.PositionTier = utatypes.PositionTier{Tier: rows[i].Tier}
		if err = decMany("Public.GetPositionTier", []decPair{
			{&pt.MinTierValue, rows[i].MinTierValue}, {&pt.MaxTierValue, rows[i].MaxTierValue},
			{&pt.Leverage, rows[i].Leverage}, {&pt.MMR, rows[i].MMR},
		}); err != nil {
			return nil, err
		}
		out = append(out, pt)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetOpenInterestLimit — market/oi-limit.
// ---------------------------------------------------------------------

type contractOiRow struct {
	Symbol             string `json:"symbol"`
	NotionalValue      string `json:"notionalValue"`
	TotalNotionalValue string `json:"totalNotionalValue"`
}

// GetOpenInterestLimit returns the per-contract open-interest notional
// limits. category is required; symbol is optional.
func (p *PublicClient) GetOpenInterestLimit(ctx context.Context, category utatypes.Category, symbol string) ([]utatypes.ContractOi, error) {
	if category == "" {
		return nil, errInvalid("Public.GetOpenInterestLimit", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	var rows []contractOiRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/oi-limit", "Public.GetOpenInterestLimit", query, &rows); err != nil {
		return nil, err
	}
	var out []utatypes.ContractOi = make([]utatypes.ContractOi, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var co utatypes.ContractOi = utatypes.ContractOi{Symbol: rows[i].Symbol}
		if err = decMany("Public.GetOpenInterestLimit", []decPair{
			{&co.NotionalValue, rows[i].NotionalValue}, {&co.TotalNotionalValue, rows[i].TotalNotionalValue},
		}); err != nil {
			return nil, err
		}
		out = append(out, co)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetIndexComponents — market/index-components.
// ---------------------------------------------------------------------

type indexComponentsRow struct {
	Symbol        string `json:"symbol"`
	ComponentList []struct {
		Exchange        string `json:"exchange"`
		SpotPair        string `json:"spotPair"`
		EquivalentPrice string `json:"equivalentPrice"`
		Weight          string `json:"weight"`
	} `json:"componentList"`
}

// GetIndexComponents returns the index-price constituents for a symbol.
// symbol is required.
func (p *PublicClient) GetIndexComponents(ctx context.Context, symbol string) (utatypes.IndexPriceComponents, error) {
	var out utatypes.IndexPriceComponents
	if symbol == "" {
		return out, errInvalid("Public.GetIndexComponents", "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("symbol", symbol)
	var row indexComponentsRow
	var err error
	if err = p.getJSON(ctx, "/api/v3/market/index-components", "Public.GetIndexComponents", query, &row); err != nil {
		return out, err
	}
	out.Symbol = row.Symbol
	var i int
	out.Components = make([]utatypes.IndexComponent, 0, len(row.ComponentList))
	for i = 0; i < len(row.ComponentList); i++ {
		var comp utatypes.IndexComponent = utatypes.IndexComponent{Exchange: row.ComponentList[i].Exchange, SpotPair: row.ComponentList[i].SpotPair}
		if err = decMany("Public.GetIndexComponents", []decPair{
			{&comp.EquivalentPrice, row.ComponentList[i].EquivalentPrice}, {&comp.Weight, row.ComponentList[i].Weight},
		}); err != nil {
			return out, err
		}
		out.Components = append(out.Components, comp)
	}
	return out, nil
}
