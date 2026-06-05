/*
FILE: uta/public_extras_contract_test.go

DESCRIPTION:
Contract tests for the V3 UTA Public EXTRAS reads: fee groups, score
weights, proof-of-reserves, open interest, funding rates, risk reserve
(+hour +all), discount rate, margin loans, position tiers, OI limits and
index components — parsing + category/symbol/coin guards.
*/

package uta

import (
	"context"
	"testing"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestContract_Public_FeeGroupAndScore(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/market/fee-group":     `{"code":"00000","msg":"success","data":{"category":"FUTURES","group":"GROUP_A","labelList":[{"weight":"1.5","label":"core","symbols":["BTCUSDT","ETHUSDT"]}],"tierList":[{"level":"1","makerFeeRate":"-0.0001"}]}}`,
		"/api/v3/market/score-weights": `{"code":"00000","msg":"success","data":[{"category":"FUTURES","label":"core","symbol":"BTCUSDT","requiredSpread":"0.0005","minMakerVolume":"1000","weight":"2"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var fg, err = uc.Public().GetMarketFeeGroup(ctx, "FUTURES", "GROUP_A")
	if err != nil {
		t.Fatalf("GetMarketFeeGroup: %v", err)
	}
	if fg.Group != "GROUP_A" || len(fg.Labels) != 1 || !fg.Labels[0].Weight.Equal(decv("1.5")) || len(fg.Labels[0].Symbols) != 2 {
		t.Fatalf("unexpected fee group: %+v", fg)
	}
	if len(fg.Tiers) != 1 || !fg.Tiers[0].MakerFeeRate.Equal(decv("-0.0001")) {
		t.Fatalf("unexpected tiers: %+v", fg.Tiers)
	}

	var sw, serr = uc.Public().GetScoreWeights(ctx, "FUTURES")
	if serr != nil || len(sw) != 1 || !sw[0].Weight.Equal(decv("2")) {
		t.Fatalf("GetScoreWeights: %v %+v", serr, sw)
	}

	if _, err = uc.Public().GetMarketFeeGroup(ctx, "", ""); err == nil {
		t.Error("GetMarketFeeGroup(no category): want guard")
	}
}

func TestContract_Public_ReservesAndOI(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/market/proof-of-reserves": `{"code":"00000","msg":"success","data":{"merkleRootHash":"0xabc","totalReserveRatio":"1.05","list":[{"coin":"BTC","userAssets":"100","platformAssets":"105","reserveRatio":"1.05"}]}}`,
		"/api/v3/market/open-interest":     `{"code":"00000","msg":"success","data":{"ts":"1700000000000","list":[{"symbol":"BTCUSDT","openInterest":"5000"}]}}`,
		"/api/v3/market/oi-limit":          `{"code":"00000","msg":"success","data":[{"symbol":"BTCUSDT","notionalValue":"1000000","totalNotionalValue":"50000000"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var por, err = uc.Public().GetProofOfReserves(ctx)
	if err != nil || por.MerkleRootHash != "0xabc" || len(por.List) != 1 || !por.List[0].ReserveRatio.Equal(decv("1.05")) {
		t.Fatalf("GetProofOfReserves: %v %+v", err, por)
	}

	var oi, oerr = uc.Public().GetOpenInterest(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT")
	if oerr != nil || oi.TimeMs != 1700000000000 || len(oi.List) != 1 || !oi.List[0].OpenInterest.Equal(decv("5000")) {
		t.Fatalf("GetOpenInterest: %v %+v", oerr, oi)
	}

	var lim, lerr = uc.Public().GetOpenInterestLimit(ctx, utatypes.CategoryUSDTFutures, "")
	if lerr != nil || len(lim) != 1 || !lim[0].TotalNotionalValue.Equal(decv("50000000")) {
		t.Fatalf("GetOpenInterestLimit: %v %+v", lerr, lim)
	}

	if _, err = uc.Public().GetOpenInterest(ctx, "", ""); err == nil {
		t.Error("GetOpenInterest(no category): want guard")
	}
}

func TestContract_Public_Funding(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/market/current-fund-rate": `{"code":"00000","msg":"success","data":[{"symbol":"BTCUSDT","fundingRate":"0.0001","fundingRateInterval":"8","nextUpdate":"1700000000000","minFundingRate":"-0.003","maxFundingRate":"0.003"}]}`,
		"/api/v3/market/history-fund-rate": `{"code":"00000","msg":"success","data":[{"symbol":"BTCUSDT","fundingRate":"0.00012","fundingRateTimestamp":"1699990000000"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var cur, err = uc.Public().GetCurrentFundingRate(ctx, "BTCUSDT")
	if err != nil || cur.Symbol != "BTCUSDT" || !cur.FundingRate.Equal(decv("0.0001")) || cur.NextUpdateMs != 1700000000000 {
		t.Fatalf("GetCurrentFundingRate: %v %+v", err, cur)
	}

	var hist, herr = uc.Public().GetHistoryFundingRate(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT", "", 50)
	if herr != nil || len(hist) != 1 || !hist[0].FundingRate.Equal(decv("0.00012")) || hist[0].TimeMs != 1699990000000 {
		t.Fatalf("GetHistoryFundingRate: %v %+v", herr, hist)
	}

	if _, err = uc.Public().GetCurrentFundingRate(ctx, ""); err == nil {
		t.Error("GetCurrentFundingRate(no symbol): want guard")
	}
	if _, err = uc.Public().GetHistoryFundingRate(ctx, "", "BTCUSDT", "", 0); err == nil {
		t.Error("GetHistoryFundingRate(no category): want guard")
	}
}

func TestContract_Public_RiskReserveAndRates(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/market/risk-reserve":      `{"code":"00000","msg":"success","data":{"totalBalance":"1000","coin":"USDT","riskReserveRecords":[{"balance":"1000","amount":"10","ts":"1700000000000","type":"inject"}]}}`,
		"/api/v3/market/risk-reserve-hour": `{"code":"00000","msg":"success","data":{"coin":"USDT","riskReserveRecords":[{"balance":"999","amount":"-1","ts":"1700000003600"}]}}`,
		"/api/v3/market/risk-reserve-all":  `{"code":"00000","msg":"success","data":{"list":[{"symbols":["BTCUSDT"],"coin":"USDT","balance":"500000"}]}}`,
		"/api/v3/market/discount-rate":     `{"code":"00000","msg":"success","data":[{"coin":"BTC","list":[{"tierStartValue":"0","discountRate":"0.95"}]}]}`,
		"/api/v3/market/margin-loans":      `{"code":"00000","msg":"success","data":{"dailyInterest":"0.0001","annualInterest":"0.0365","limit":"1000000"}}`,
		"/api/v3/market/position-tier":     `{"code":"00000","msg":"success","data":[{"tier":"1","minTierValue":"0","maxTierValue":"50000","leverage":"125","mmr":"0.004"}]}`,
		"/api/v3/market/index-components":  `{"code":"00000","msg":"success","data":{"symbol":"BTCUSDT","componentList":[{"exchange":"binance","spotPair":"BTC/USDT","equivalentPrice":"60000","weight":"0.5"}]}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()
	var cat = utatypes.CategoryUSDTFutures

	var rr, err = uc.Public().GetRiskReserve(ctx, cat, "BTCUSDT", "USDT")
	if err != nil || rr.Coin != "USDT" || !rr.TotalBalance.Equal(decv("1000")) || len(rr.Records) != 1 || rr.Records[0].Type != "inject" {
		t.Fatalf("GetRiskReserve: %v %+v", err, rr)
	}
	var rh, herr = uc.Public().GetRiskReserveHour(ctx, cat, "BTCUSDT", "")
	if herr != nil || len(rh.Records) != 1 || !rh.Records[0].Amount.Equal(decv("-1")) {
		t.Fatalf("GetRiskReserveHour: %v %+v", herr, rh)
	}
	var ra, aerr = uc.Public().GetRiskReserveAll(ctx, cat)
	if aerr != nil || len(ra) != 1 || !ra[0].Balance.Equal(decv("500000")) || len(ra[0].Symbols) != 1 {
		t.Fatalf("GetRiskReserveAll: %v %+v", aerr, ra)
	}
	var dr, derr = uc.Public().GetDiscountRate(ctx)
	if derr != nil || len(dr) != 1 || dr[0].Coin != "BTC" || !dr[0].List[0].DiscountRate.Equal(decv("0.95")) {
		t.Fatalf("GetDiscountRate: %v %+v", derr, dr)
	}
	var ml, merr = uc.Public().GetMarginLoans(ctx, "USDT")
	if merr != nil || !ml.AnnualInterest.Equal(decv("0.0365")) {
		t.Fatalf("GetMarginLoans: %v %+v", merr, ml)
	}
	var pt, perr = uc.Public().GetPositionTier(ctx, cat, "BTCUSDT", "")
	if perr != nil || len(pt) != 1 || !pt[0].Leverage.Equal(decv("125")) || !pt[0].MMR.Equal(decv("0.004")) {
		t.Fatalf("GetPositionTier: %v %+v", perr, pt)
	}
	var ic, ierr = uc.Public().GetIndexComponents(ctx, "BTCUSDT")
	if ierr != nil || ic.Symbol != "BTCUSDT" || len(ic.Components) != 1 || !ic.Components[0].EquivalentPrice.Equal(decv("60000")) {
		t.Fatalf("GetIndexComponents: %v %+v", ierr, ic)
	}

	// Guards.
	if _, err = uc.Public().GetRiskReserve(ctx, cat, "", ""); err == nil {
		t.Error("GetRiskReserve(no symbol): want guard")
	}
	if _, err = uc.Public().GetMarginLoans(ctx, ""); err == nil {
		t.Error("GetMarginLoans(no coin): want guard")
	}
	if _, err = uc.Public().GetIndexComponents(ctx, ""); err == nil {
		t.Error("GetIndexComponents(no symbol): want guard")
	}
}
