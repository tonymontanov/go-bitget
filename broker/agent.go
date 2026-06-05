/*
FILE: broker/agent.go

DESCRIPTION:
Agent sub-client — affiliate/referral reporting
(/api/v2/broker/customer-*, sub-customer-list, agent-commission). All
reads; require the account to be an approved Bitget agent (a non-agent
account gets a 4xx).

Mixed pagination & verbs (per the venue):
  - GET  customer-commissions / sub-customer-list / customer-kyc-result /
         agent-commission — idLessThan/endId (or minId) cursor
  - POST customer-trade-volume / customer-list / customer-deposit /
         customer-asset — pageNo/pageSize body

Request params verified against the Bitget V2 broker docs and the
tiagosiebler reference client.
*/

package broker

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	brokertypes "github.com/tonymontanov/go-bitget/v2/broker/types"
)

// AgentClient — affiliate/referral reporting sub-client.
type AgentClient struct {
	c *Client
}

func newAgentClient(c *Client) *AgentClient {
	return &AgentClient{c: c}
}

const agentPageSize = 100

// decPair / decAll batch-parse several decimal fields with one scope.
type decPair struct {
	dst *decimal.Decimal
	raw string
}

func decAll(scope string, ps ...decPair) error {
	var i int
	for i = 0; i < len(ps); i++ {
		if err := decErr(scope, ps[i].dst, ps[i].raw); err != nil {
			return err
		}
	}
	return nil
}

// AgentQuery — common filters for the time-windowed agent reads
// (sub-customer-list, customer-kyc-result, customer-trade-volume,
// customer-deposit).
type AgentQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
	UID         string
	ShowSub     string
}

func (q AgentQuery) applyQuery(v url.Values) {
	if q.StartTimeMs > 0 {
		v.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	}
	if q.EndTimeMs > 0 {
		v.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	}
	if q.UID != "" {
		v.Set("uid", q.UID)
	}
	if q.ShowSub != "" {
		v.Set("showSub", q.ShowSub)
	}
}

func (q AgentQuery) applyBody(m map[string]any) {
	if q.StartTimeMs > 0 {
		m["startTime"] = strconv.FormatInt(q.StartTimeMs, 10)
	}
	if q.EndTimeMs > 0 {
		m["endTime"] = strconv.FormatInt(q.EndTimeMs, 10)
	}
	if q.UID != "" {
		m["uid"] = q.UID
	}
	if q.ShowSub != "" {
		m["showSub"] = q.ShowSub
	}
}

// postPageList runs a POST pageNo/pageSize report returning a flat array,
// fully walked. body holds the static filters; pageNo/pageSize are added
// per page. dst must be a pointer to a slice of row structs.
func (a *AgentClient) postPageList(ctx context.Context, path string, base map[string]any, pageNo, pageSize int, dst any, scope string) error {
	var body = make(map[string]any, len(base)+2)
	for k, v := range base {
		body[k] = v
	}
	body["pageNo"] = strconv.Itoa(pageNo)
	body["pageSize"] = strconv.Itoa(pageSize)

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   path,
		Body:   body,
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
// GetCustomerCommissions — broker/customer-commissions (cursor).
// ---------------------------------------------------------------------

// AgentCustomerCommissionsQuery — filters for GetCustomerCommissions.
type AgentCustomerCommissionsQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
	UID         string
	Coin        string
	Symbol      string
	ShowSub     string
}

type agentCustomerCommissionRow struct {
	UID                    string `json:"uid"`
	Date                   string `json:"date"`
	Coin                   string `json:"coin"`
	Symbol                 string `json:"symbol"`
	ProductType            string `json:"productType"`
	DealAmount             string `json:"dealAmount"`
	Fee                    string `json:"fee"`
	FeeDeduction           string `json:"feeDeduction"`
	ActivityBonusDeduct    string `json:"activityBonusDeduct"`
	SpotCouponDeduct       string `json:"spotCouponDeduct"`
	FuturesCouponDeduct    string `json:"futuresCouponDeduct"`
	SpotFeeDiscountDeduct  string `json:"spotFeeDiscountDeduct"`
	NegativeMakerFeeDeduct string `json:"negativeMakerFeeDeduct"`
	FeePaid                string `json:"feePaid"`
	RebateAmount           string `json:"rebateAmount"`
	UserTotalRebateAmount  string `json:"userTotalRebateAmount"`
	DayTotalRebateAmount   string `json:"dayTotalRebateAmount"`
	TotalRebateAmount      string `json:"totalRebateAmount"`
}

type agentCustomerCommissionsEnvelope struct {
	EndID          string                       `json:"endId"`
	CommissionList []agentCustomerCommissionRow `json:"commissionList"`
}

// GetCustomerCommissions lists per-customer commission rows. Walks the
// idLessThan/endId cursor.
func (a *AgentClient) GetCustomerCommissions(ctx context.Context, q AgentCustomerCommissionsQuery) ([]brokertypes.AgentCustomerCommission, error) {
	var rows []agentCustomerCommissionRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.Agent.GetCustomerCommissions",
		func(idLessThan string, limit int) ([]agentCustomerCommissionRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if q.StartTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			}
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if q.UID != "" {
				query.Set("uid", q.UID)
			}
			if q.Coin != "" {
				query.Set("coin", q.Coin)
			}
			if q.Symbol != "" {
				query.Set("symbol", q.Symbol)
			}
			if q.ShowSub != "" {
				query.Set("showSub", q.ShowSub)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/broker/customer-commissions",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env agentCustomerCommissionsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Agent.GetCustomerCommissions", ferr)
			}
			return env.CommissionList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCustomerCommission = make([]brokertypes.AgentCustomerCommission, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var c brokertypes.AgentCustomerCommission = brokertypes.AgentCustomerCommission{
			UID: r.UID, Date: r.Date, Coin: r.Coin, Symbol: r.Symbol, ProductType: r.ProductType,
		}
		if err = decAll("Agent.GetCustomerCommissions",
			decPair{&c.DealAmount, r.DealAmount},
			decPair{&c.Fee, r.Fee},
			decPair{&c.FeeDeduction, r.FeeDeduction},
			decPair{&c.ActivityBonusDeduct, r.ActivityBonusDeduct},
			decPair{&c.SpotCouponDeduct, r.SpotCouponDeduct},
			decPair{&c.FuturesCouponDeduct, r.FuturesCouponDeduct},
			decPair{&c.SpotFeeDiscountDeduct, r.SpotFeeDiscountDeduct},
			decPair{&c.NegativeMakerFeeDeduct, r.NegativeMakerFeeDeduct},
			decPair{&c.FeePaid, r.FeePaid},
			decPair{&c.RebateAmount, r.RebateAmount},
			decPair{&c.UserTotalRebateAmount, r.UserTotalRebateAmount},
			decPair{&c.DayTotalRebateAmount, r.DayTotalRebateAmount},
			decPair{&c.TotalRebateAmount, r.TotalRebateAmount},
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetSubCustomerList — broker/sub-customer-list (minId cursor).
// ---------------------------------------------------------------------

type agentSubCustomerRow struct {
	UID          string `json:"uid"`
	RegisterTime string `json:"registerTime"`
}

type agentSubCustomerEnvelope struct {
	List  []agentSubCustomerRow `json:"list"`
	MinID string                `json:"minId"`
}

// GetSubCustomerList lists the agent's sub-customers. Walks the
// idLessThan/minId cursor.
func (a *AgentClient) GetSubCustomerList(ctx context.Context, q AgentQuery) ([]brokertypes.AgentSubCustomer, error) {
	var rows []agentSubCustomerRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.Agent.GetSubCustomerList",
		func(idLessThan string, limit int) ([]agentSubCustomerRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			q.applyQuery(query)
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/broker/sub-customer-list",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env agentSubCustomerEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Agent.GetSubCustomerList", ferr)
			}
			return env.List, env.MinID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentSubCustomer = make([]brokertypes.AgentSubCustomer, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var sc brokertypes.AgentSubCustomer = brokertypes.AgentSubCustomer{UID: rows[i].UID}
		sc.RegisterTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].RegisterTime)
		out = append(out, sc)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCustomerTradeVolume — POST broker/customer-trade-volume (pageNo).
// ---------------------------------------------------------------------

type agentTradeVolumeRow struct {
	UID           string `json:"uid"`
	Volumn        string `json:"volumn"`
	SpotVolume    string `json:"spotVolume"`
	FuturesVolume string `json:"futuresVolume"`
	Time          string `json:"time"`
}

// GetCustomerTradeVolume lists per-customer trade volume. Fully walks
// pageNo/pageSize (POST).
func (a *AgentClient) GetCustomerTradeVolume(ctx context.Context, q AgentQuery) ([]brokertypes.AgentCustomerTradeVolume, error) {
	var base = map[string]any{}
	q.applyBody(base)
	var rows []agentTradeVolumeRow
	var err error
	rows, err = paginateByPageNo(ctx, agentPageSize,
		func(pageNo, pageSize int) ([]agentTradeVolumeRow, error) {
			var page []agentTradeVolumeRow
			if ferr := a.postPageList(ctx, "/api/v2/broker/customer-trade-volume", base, pageNo, pageSize, &page, "Agent.GetCustomerTradeVolume"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCustomerTradeVolume = make([]brokertypes.AgentCustomerTradeVolume, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var v brokertypes.AgentCustomerTradeVolume = brokertypes.AgentCustomerTradeVolume{UID: rows[i].UID}
		if err = decAll("Agent.GetCustomerTradeVolume",
			decPair{&v.Volume, rows[i].Volumn},
			decPair{&v.SpotVolume, rows[i].SpotVolume},
			decPair{&v.FuturesVolume, rows[i].FuturesVolume},
		); err != nil {
			return nil, err
		}
		v.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].Time)
		out = append(out, v)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCustomerList — POST broker/customer-list (pageNo).
// ---------------------------------------------------------------------

// AgentCustomerListQuery — filters for GetCustomerList.
type AgentCustomerListQuery struct {
	StartTimeMs  int64
	EndTimeMs    int64
	UID          string
	ReferralCode string
	ShowSub      string
}

type agentCustomerListRow struct {
	UID          string `json:"uid"`
	RegisterTime string `json:"registerTime"`
}

// GetCustomerList lists the agent's direct customers. Fully walks
// pageNo/pageSize (POST).
func (a *AgentClient) GetCustomerList(ctx context.Context, q AgentCustomerListQuery) ([]brokertypes.AgentCustomerListItem, error) {
	var base = map[string]any{}
	if q.StartTimeMs > 0 {
		base["startTime"] = strconv.FormatInt(q.StartTimeMs, 10)
	}
	if q.EndTimeMs > 0 {
		base["endTime"] = strconv.FormatInt(q.EndTimeMs, 10)
	}
	if q.UID != "" {
		base["uid"] = q.UID
	}
	if q.ReferralCode != "" {
		base["referralCode"] = q.ReferralCode
	}
	if q.ShowSub != "" {
		base["showSub"] = q.ShowSub
	}
	var rows []agentCustomerListRow
	var err error
	rows, err = paginateByPageNo(ctx, agentPageSize,
		func(pageNo, pageSize int) ([]agentCustomerListRow, error) {
			var page []agentCustomerListRow
			if ferr := a.postPageList(ctx, "/api/v2/broker/customer-list", base, pageNo, pageSize, &page, "Agent.GetCustomerList"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCustomerListItem = make([]brokertypes.AgentCustomerListItem, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var it brokertypes.AgentCustomerListItem = brokertypes.AgentCustomerListItem{UID: rows[i].UID}
		it.RegisterTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].RegisterTime)
		out = append(out, it)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCustomerKycResult — broker/customer-kyc-result (cursor).
// ---------------------------------------------------------------------

type agentKycRow struct {
	UID       string `json:"uid"`
	KycResult string `json:"kycResult"`
}

type agentKycEnvelope struct {
	UserList []agentKycRow `json:"userList"`
	EndID    string        `json:"endId"`
}

// GetCustomerKycResult lists customer KYC status. Walks the
// idLessThan/endId cursor.
func (a *AgentClient) GetCustomerKycResult(ctx context.Context, q AgentQuery) ([]brokertypes.AgentCustomerKyc, error) {
	var rows []agentKycRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.Agent.GetCustomerKycResult",
		func(idLessThan string, limit int) ([]agentKycRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			q.applyQuery(query)
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/broker/customer-kyc-result",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env agentKycEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Agent.GetCustomerKycResult", ferr)
			}
			return env.UserList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCustomerKyc = make([]brokertypes.AgentCustomerKyc, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		out = append(out, brokertypes.AgentCustomerKyc{UID: rows[i].UID, KycResult: rows[i].KycResult})
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCustomerDeposits — POST broker/customer-deposit (pageNo).
// ---------------------------------------------------------------------

type agentDepositRow struct {
	OrderID       string `json:"orderId"`
	UID           string `json:"uid"`
	DepositTime   string `json:"depositTime"`
	DepositCoin   string `json:"depositCoin"`
	DepositAmount string `json:"depositAmount"`
}

// GetCustomerDeposits lists per-customer deposits. Fully walks
// pageNo/pageSize (POST).
func (a *AgentClient) GetCustomerDeposits(ctx context.Context, q AgentQuery) ([]brokertypes.AgentCustomerDeposit, error) {
	var base = map[string]any{}
	q.applyBody(base)
	var rows []agentDepositRow
	var err error
	rows, err = paginateByPageNo(ctx, agentPageSize,
		func(pageNo, pageSize int) ([]agentDepositRow, error) {
			var page []agentDepositRow
			if ferr := a.postPageList(ctx, "/api/v2/broker/customer-deposit", base, pageNo, pageSize, &page, "Agent.GetCustomerDeposits"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCustomerDeposit = make([]brokertypes.AgentCustomerDeposit, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var d brokertypes.AgentCustomerDeposit = brokertypes.AgentCustomerDeposit{
			OrderID: rows[i].OrderID, UID: rows[i].UID, DepositCoin: rows[i].DepositCoin,
		}
		d.DepositTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].DepositTime)
		if err = decErr("Agent.GetCustomerDeposits", &d.DepositAmount, rows[i].DepositAmount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCustomerAssets — POST broker/customer-asset (pageNo).
// ---------------------------------------------------------------------

// AgentAssetQuery — filters for GetCustomerAssets.
type AgentAssetQuery struct {
	UID     string
	ShowSub string
}

type agentAssetRow struct {
	Balance string `json:"balance"`
	UID     string `json:"uid"`
	UTime   string `json:"uTime"`
	Remark  string `json:"remark"`
}

// GetCustomerAssets lists per-customer balances. Fully walks
// pageNo/pageSize (POST).
func (a *AgentClient) GetCustomerAssets(ctx context.Context, q AgentAssetQuery) ([]brokertypes.AgentCustomerAsset, error) {
	var base = map[string]any{}
	if q.UID != "" {
		base["uid"] = q.UID
	}
	if q.ShowSub != "" {
		base["showSub"] = q.ShowSub
	}
	var rows []agentAssetRow
	var err error
	rows, err = paginateByPageNo(ctx, agentPageSize,
		func(pageNo, pageSize int) ([]agentAssetRow, error) {
			var page []agentAssetRow
			if ferr := a.postPageList(ctx, "/api/v2/broker/customer-asset", base, pageNo, pageSize, &page, "Agent.GetCustomerAssets"); ferr != nil {
				return nil, ferr
			}
			return page, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCustomerAsset = make([]brokertypes.AgentCustomerAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var as brokertypes.AgentCustomerAsset = brokertypes.AgentCustomerAsset{UID: rows[i].UID, Remark: rows[i].Remark}
		if err = decErr("Agent.GetCustomerAssets", &as.Balance, rows[i].Balance); err != nil {
			return nil, err
		}
		as.UTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, as)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetCommissionDetail — broker/agent-commission (cursor).
// ---------------------------------------------------------------------

type agentCommissionDetailRow struct {
	UID                     string `json:"uid"`
	BizType                 string `json:"bizType"`
	SubBizType              string `json:"subBizType"`
	Symbol                  string `json:"symbol"`
	Coin                    string `json:"coin"`
	Fee                     string `json:"fee"`
	Volume                  string `json:"volume"`
	ActivityBonusDeduct     string `json:"activityBonusDeduct"`
	SpotCouponDeduct        string `json:"spotCouponDeduct"`
	FuturesCouponDeduct     string `json:"futuresCouponDeduct"`
	SpotFeeDiscountDeduct   string `json:"spotFeeDiscountDeduct"`
	NegativeMakerFeeDeduct  string `json:"negativeMakerFeeDeduct"`
	FeePaid                 string `json:"feePaid"`
	DirectCommission        string `json:"directCommission"`
	SubCommission           string `json:"subCommission"`
	PartnerCommission       string `json:"partnerCommission"`
	PartnerActualCommission string `json:"partnerActualCommission"`
	TraderType              string `json:"traderType"`
	APIType                 string `json:"apiType"`
	Status                  string `json:"status"`
	StartCalculationTime    string `json:"startCalculationTime"`
	EndCalculationTime      string `json:"endCalculationTime"`
}

type agentCommissionDetailEnvelope struct {
	EndID          string                     `json:"endId"`
	CommissionList []agentCommissionDetailRow `json:"commissionList"`
}

// GetCommissionDetail lists the agent's own commission detail rows. Walks
// the idLessThan/endId cursor.
func (a *AgentClient) GetCommissionDetail(ctx context.Context, q BrokerReportQuery) ([]brokertypes.AgentCommissionDetail, error) {
	var rows []agentCommissionDetailRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.Agent.GetCommissionDetail",
		func(idLessThan string, limit int) ([]agentCommissionDetailRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if q.StartTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			}
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/broker/agent-commission",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env agentCommissionDetailEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Agent.GetCommissionDetail", ferr)
			}
			return env.CommissionList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AgentCommissionDetail = make([]brokertypes.AgentCommissionDetail, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var d brokertypes.AgentCommissionDetail = brokertypes.AgentCommissionDetail{
			UID: r.UID, BizType: r.BizType, SubBizType: r.SubBizType, Symbol: r.Symbol, Coin: r.Coin,
			TraderType: r.TraderType, APIType: r.APIType, Status: r.Status,
		}
		if err = decAll("Agent.GetCommissionDetail",
			decPair{&d.Fee, r.Fee},
			decPair{&d.Volume, r.Volume},
			decPair{&d.ActivityBonusDeduct, r.ActivityBonusDeduct},
			decPair{&d.SpotCouponDeduct, r.SpotCouponDeduct},
			decPair{&d.FuturesCouponDeduct, r.FuturesCouponDeduct},
			decPair{&d.SpotFeeDiscountDeduct, r.SpotFeeDiscountDeduct},
			decPair{&d.NegativeMakerFeeDeduct, r.NegativeMakerFeeDeduct},
			decPair{&d.FeePaid, r.FeePaid},
			decPair{&d.DirectCommission, r.DirectCommission},
			decPair{&d.SubCommission, r.SubCommission},
			decPair{&d.PartnerCommission, r.PartnerCommission},
			decPair{&d.PartnerActualCommission, r.PartnerActualCommission},
		); err != nil {
			return nil, err
		}
		d.StartCalculationTimeMs, _ = bgcommon.ParseInt64OrZero(r.StartCalculationTime)
		d.EndCalculationTimeMs, _ = bgcommon.ParseInt64OrZero(r.EndCalculationTime)
		out = append(out, d)
	}
	return out, nil
}
