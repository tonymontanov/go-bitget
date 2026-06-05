/*
FILE: common/p2p.go

DESCRIPTION:
P2P sub-client — P2P merchant info / orders / advertisements
(/api/v2/p2p/{merchantList,merchantInfo,orderList,advList}). All signed
reads; orderList / advList require a merchant account. The list endpoints
walk the idLessThan cursor (next = the response's minMerchantId /
minOrderId / minAdvId).

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

// P2PClient — P2P merchant sub-client.
type P2PClient struct {
	c *Client
}

func newP2PClient(c *Client) *P2PClient {
	return &P2PClient{c: c}
}

func i64(s string) int64 {
	var v int64
	v, _ = bgcommon.ParseInt64OrZero(s)
	return v
}

// ---------------------------------------------------------------------
// GetMerchants — p2p/merchantList (cursor).
// ---------------------------------------------------------------------

type p2pMerchantRow struct {
	RegisterTime        string `json:"registerTime"`
	NickName            string `json:"nickName"`
	IsOnline            string `json:"isOnline"`
	AvgPaymentTime      string `json:"avgPaymentTime"`
	AvgReleaseTime      string `json:"avgReleaseTime"`
	TotalTrades         string `json:"totalTrades"`
	TotalBuy            string `json:"totalBuy"`
	TotalSell           string `json:"totalSell"`
	TotalCompletionRate string `json:"totalCompletionRate"`
	Trades30d           string `json:"trades30d"`
	Sell30d             string `json:"sell30d"`
	Buy30d              string `json:"buy30d"`
	CompletionRate30d   string `json:"completionRate30d"`
}

type p2pMerchantListEnvelope struct {
	MerchantList  []p2pMerchantRow `json:"merchantList"`
	MinMerchantID string           `json:"minMerchantId"`
}

// GetMerchants lists P2P merchants. online filters to online merchants
// ("yes" | "no", optional). Walks the idLessThan/minMerchantId cursor.
func (p *P2PClient) GetMerchants(ctx context.Context, online string) ([]commontypes.P2PMerchant, error) {
	var rows []p2pMerchantRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "common.P2P.GetMerchants",
		func(idLessThan string, limit int) ([]p2pMerchantRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if online != "" {
				query.Set("online", online)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = p.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/p2p/merchantList",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env p2pMerchantListEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("P2P.GetMerchants", ferr)
			}
			return env.MerchantList, env.MinMerchantID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.P2PMerchant = make([]commontypes.P2PMerchant, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var m commontypes.P2PMerchant = commontypes.P2PMerchant{
			NickName:       r.NickName,
			IsOnline:       r.IsOnline,
			RegisterTimeMs: i64(r.RegisterTime),
			AvgPaymentTime: r.AvgPaymentTime,
			AvgReleaseTime: r.AvgReleaseTime,
			TotalTrades:    i64(r.TotalTrades),
			TotalBuy:       i64(r.TotalBuy),
			TotalSell:      i64(r.TotalSell),
			Trades30d:      i64(r.Trades30d),
			Sell30d:        i64(r.Sell30d),
			Buy30d:         i64(r.Buy30d),
		}
		if err = decErr("P2P.GetMerchants", &m.TotalCompletionRate, r.TotalCompletionRate); err != nil {
			return nil, err
		}
		if err = decErr("P2P.GetMerchants", &m.CompletionRate30d, r.CompletionRate30d); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetMerchantInfo — p2p/merchantInfo.
// ---------------------------------------------------------------------

type p2pMerchantInfoRow struct {
	RegisterTime        string `json:"registerTime"`
	NickName            string `json:"nickName"`
	MerchantID          string `json:"merchantId"`
	AvgPaymentTime      string `json:"avgPaymentTime"`
	AvgReleaseTime      string `json:"avgReleaseTime"`
	TotalTrades         string `json:"totalTrades"`
	TotalBuy            string `json:"totalBuy"`
	TotalSell           string `json:"totalSell"`
	TotalCompletionRate string `json:"totalCompletionRate"`
	Trades30d           string `json:"trades30d"`
	Sell30d             string `json:"sell30d"`
	Buy30d              string `json:"buy30d"`
	CompletionRate30d   string `json:"completionRate30d"`
	KycStatus           bool   `json:"kycStatus"`
	EmailBindStatus     bool   `json:"emailBindStatus"`
	MobileBindStatus    bool   `json:"mobileBindStatus"`
	Email               string `json:"email"`
	Mobile              string `json:"mobile"`
}

// GetMerchantInfo returns the caller's own P2P merchant profile.
func (p *P2PClient) GetMerchantInfo(ctx context.Context) (commontypes.P2PMerchantInfo, error) {
	var out commontypes.P2PMerchantInfo
	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/p2p/merchantInfo",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var r p2pMerchantInfoRow
	if err = resp.UnmarshalData(&r); err != nil {
		return out, errParse("P2P.GetMerchantInfo", err)
	}
	out = commontypes.P2PMerchantInfo{
		MerchantID:       r.MerchantID,
		NickName:         r.NickName,
		RegisterTimeMs:   i64(r.RegisterTime),
		AvgPaymentTime:   r.AvgPaymentTime,
		AvgReleaseTime:   r.AvgReleaseTime,
		TotalTrades:      i64(r.TotalTrades),
		TotalBuy:         i64(r.TotalBuy),
		TotalSell:        i64(r.TotalSell),
		Trades30d:        i64(r.Trades30d),
		Sell30d:          i64(r.Sell30d),
		Buy30d:           i64(r.Buy30d),
		KycStatus:        r.KycStatus,
		EmailBindStatus:  r.EmailBindStatus,
		MobileBindStatus: r.MobileBindStatus,
		Email:            r.Email,
		Mobile:           r.Mobile,
	}
	if err = decErr("P2P.GetMerchantInfo", &out.TotalCompletionRate, r.TotalCompletionRate); err != nil {
		return out, err
	}
	if err = decErr("P2P.GetMerchantInfo", &out.CompletionRate30d, r.CompletionRate30d); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetOrders — p2p/orderList (cursor).
// ---------------------------------------------------------------------

// P2POrdersQuery — filters for GetOrders. StartTimeMs, AdvNo and Language
// are required by the venue.
type P2POrdersQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
	AdvNo       string
	Language    string
	Status      string
	Side        string
	Coin        string
	Fiat        string
	OrderNo     string
}

type p2pPaymethodFieldRow struct {
	Name     string `json:"name"`
	Required string `json:"required"`
	Type     string `json:"type"`
	Value    string `json:"value"`
}

type p2pOrderRow struct {
	OrderID        string `json:"orderId"`
	OrderNo        string `json:"orderNo"`
	AdvNo          string `json:"advNo"`
	Side           string `json:"side"`
	Count          string `json:"count"`
	Coin           string `json:"coin"`
	Price          string `json:"price"`
	Fiat           string `json:"fiat"`
	WithdrawTime   string `json:"withdrawTime"`
	RepresentTime  string `json:"representTime"`
	ReleaseTime    string `json:"releaseTime"`
	PaymentTime    string `json:"paymentTime"`
	Amount         string `json:"amount"`
	Status         string `json:"status"`
	BuyerRealName  string `json:"buyerRealName"`
	SellerRealName string `json:"sellerRealName"`
	Ctime          string `json:"ctime"`
	Utime          string `json:"utime"`
	PaymentInfo    struct {
		PaymethodName string                 `json:"paymethodName"`
		PaymethodID   string                 `json:"paymethodId"`
		PaymethodInfo []p2pPaymethodFieldRow `json:"paymethodInfo"`
	} `json:"paymentInfo"`
}

type p2pOrderListEnvelope struct {
	OrderList  []p2pOrderRow `json:"orderList"`
	MinOrderID string        `json:"minOrderId"`
}

// GetOrders lists the merchant's P2P orders. StartTimeMs, AdvNo and
// Language are required. Walks the idLessThan/minOrderId cursor.
func (p *P2PClient) GetOrders(ctx context.Context, q P2POrdersQuery) ([]commontypes.P2PMerchantOrder, error) {
	switch {
	case q.StartTimeMs <= 0:
		return nil, errInvalid("P2P.GetOrders", "startTime is required")
	case q.AdvNo == "":
		return nil, errInvalid("P2P.GetOrders", "advNo is required")
	case q.Language == "":
		return nil, errInvalid("P2P.GetOrders", "language is required")
	}

	var rows []p2pOrderRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "common.P2P.GetOrders",
		func(idLessThan string, limit int) ([]p2pOrderRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			query.Set("advNo", q.AdvNo)
			query.Set("language", q.Language)
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if q.Status != "" {
				query.Set("status", q.Status)
			}
			if q.Side != "" {
				query.Set("side", q.Side)
			}
			if q.Coin != "" {
				query.Set("coin", q.Coin)
			}
			if q.Fiat != "" {
				query.Set("fiat", q.Fiat)
			}
			if q.OrderNo != "" {
				query.Set("orderNo", q.OrderNo)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = p.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/p2p/orderList",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env p2pOrderListEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("P2P.GetOrders", ferr)
			}
			return env.OrderList, env.MinOrderID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.P2PMerchantOrder = make([]commontypes.P2PMerchantOrder, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var o commontypes.P2PMerchantOrder = commontypes.P2PMerchantOrder{
			OrderID: r.OrderID, OrderNo: r.OrderNo, AdvNo: r.AdvNo, Side: r.Side,
			Coin: r.Coin, Fiat: r.Fiat, Status: r.Status,
			BuyerRealName: r.BuyerRealName, SellerRealName: r.SellerRealName,
			WithdrawTimeMs: i64(r.WithdrawTime), RepresentTimeMs: i64(r.RepresentTime),
			ReleaseTimeMs: i64(r.ReleaseTime), PaymentTimeMs: i64(r.PaymentTime),
			CTimeMs: i64(r.Ctime), UTimeMs: i64(r.Utime),
		}
		if err = decErr("P2P.GetOrders", &o.Count, r.Count); err != nil {
			return nil, err
		}
		if err = decErr("P2P.GetOrders", &o.Price, r.Price); err != nil {
			return nil, err
		}
		if err = decErr("P2P.GetOrders", &o.Amount, r.Amount); err != nil {
			return nil, err
		}
		o.PaymentInfo.PaymethodName = r.PaymentInfo.PaymethodName
		o.PaymentInfo.PaymethodID = r.PaymentInfo.PaymethodID
		var j int
		o.PaymentInfo.PaymethodInfo = make([]commontypes.P2POrderPaymethodField, 0, len(r.PaymentInfo.PaymethodInfo))
		for j = 0; j < len(r.PaymentInfo.PaymethodInfo); j++ {
			var f = r.PaymentInfo.PaymethodInfo[j]
			o.PaymentInfo.PaymethodInfo = append(o.PaymentInfo.PaymethodInfo, commontypes.P2POrderPaymethodField{
				Name: f.Name, Required: f.Required, Type: f.Type, Value: f.Value,
			})
		}
		out = append(out, o)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAdvertisements — p2p/advList (cursor).
// ---------------------------------------------------------------------

// P2PAdsQuery — filters for GetAdvertisements. StartTimeMs, Status, Side,
// Coin and Fiat are required by the venue.
type P2PAdsQuery struct {
	StartTimeMs int64
	EndTimeMs   int64
	Status      string
	Side        string
	Coin        string
	Fiat        string
	AdvNo       string
	Language    string
	OrderBy     string
	PayMethodID string
	SourceType  string
}

type p2pAdPaymentFieldRow struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Type     string `json:"type"`
}

type p2pAdRow struct {
	AdvID          string `json:"advId"`
	AdvNo          string `json:"advNo"`
	Side           string `json:"side"`
	AdvSize        string `json:"advSize"`
	Size           string `json:"size"`
	Coin           string `json:"coin"`
	Price          string `json:"price"`
	CoinPrecision  string `json:"coinPrecision"`
	Fiat           string `json:"fiat"`
	FiatPrecision  string `json:"fiatPrecision"`
	FiatSymbol     string `json:"fiatSymbol"`
	Status         string `json:"status"`
	Hide           string `json:"hide"`
	MaxTradeAmount string `json:"maxTradeAmount"`
	MinTradeAmount string `json:"minTradeAmount"`
	PayDuration    string `json:"payDuration"`
	TurnoverNum    string `json:"turnoverNum"`
	TurnoverRate   string `json:"turnoverRate"`
	Label          string `json:"label"`
	UserLimitList  struct {
		MinCompleteNum     string `json:"minCompleteNum"`
		MaxCompleteNum     string `json:"maxCompleteNum"`
		PlaceOrderNum      string `json:"placeOrderNum"`
		AllowMerchantPlace string `json:"allowMerchantPlace"`
		CompleteRate30d    string `json:"completeRate30d"`
		Country            string `json:"country"`
	} `json:"userLimitList"`
	PaymentMethodList []struct {
		PaymentMethod string                 `json:"paymentMethod"`
		PaymentID     string                 `json:"paymentId"`
		PaymentInfo   []p2pAdPaymentFieldRow `json:"paymentInfo"`
	} `json:"paymentMethodList"`
	MerchantCertifiedList []struct {
		ImageURL string `json:"imageUrl"`
		Desc     string `json:"desc"`
	} `json:"merchantCertifiedList"`
	Utime string `json:"utime"`
	Ctime string `json:"ctime"`
}

type p2pAdListEnvelope struct {
	AdvList  []p2pAdRow `json:"advList"`
	MinAdvID string     `json:"minAdvId"`
}

// GetAdvertisements lists P2P advertisements. StartTimeMs, Status, Side,
// Coin and Fiat are required. Walks the idLessThan/minAdvId cursor.
func (p *P2PClient) GetAdvertisements(ctx context.Context, q P2PAdsQuery) ([]commontypes.P2PMerchantAd, error) {
	switch {
	case q.StartTimeMs <= 0:
		return nil, errInvalid("P2P.GetAdvertisements", "startTime is required")
	case q.Status == "":
		return nil, errInvalid("P2P.GetAdvertisements", "status is required")
	case q.Side == "":
		return nil, errInvalid("P2P.GetAdvertisements", "side is required")
	case q.Coin == "":
		return nil, errInvalid("P2P.GetAdvertisements", "coin is required")
	case q.Fiat == "":
		return nil, errInvalid("P2P.GetAdvertisements", "fiat is required")
	}

	var rows []p2pAdRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "common.P2P.GetAdvertisements",
		func(idLessThan string, limit int) ([]p2pAdRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			query.Set("status", q.Status)
			query.Set("side", q.Side)
			query.Set("coin", q.Coin)
			query.Set("fiat", q.Fiat)
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if q.AdvNo != "" {
				query.Set("advNo", q.AdvNo)
			}
			if q.Language != "" {
				query.Set("language", q.Language)
			}
			if q.OrderBy != "" {
				query.Set("orderBy", q.OrderBy)
			}
			if q.PayMethodID != "" {
				query.Set("payMethodId", q.PayMethodID)
			}
			if q.SourceType != "" {
				query.Set("sourceType", q.SourceType)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = p.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/p2p/advList",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env p2pAdListEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("P2P.GetAdvertisements", ferr)
			}
			return env.AdvList, env.MinAdvID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.P2PMerchantAd = make([]commontypes.P2PMerchantAd, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var ad commontypes.P2PMerchantAd = commontypes.P2PMerchantAd{
			AdvID: r.AdvID, AdvNo: r.AdvNo, Side: r.Side, Coin: r.Coin, Fiat: r.Fiat,
			FiatSymbol: r.FiatSymbol, Status: r.Status, Hide: r.Hide, Label: r.Label,
			CoinPrecision: r.CoinPrecision, FiatPrecision: r.FiatPrecision, PayDuration: r.PayDuration,
			TurnoverNum: i64(r.TurnoverNum), CTimeMs: i64(r.Ctime), UTimeMs: i64(r.Utime),
		}
		var scope = "P2P.GetAdvertisements"
		if err = decErr(scope, &ad.AdvSize, r.AdvSize); err != nil {
			return nil, err
		}
		if err = decErr(scope, &ad.Size, r.Size); err != nil {
			return nil, err
		}
		if err = decErr(scope, &ad.Price, r.Price); err != nil {
			return nil, err
		}
		if err = decErr(scope, &ad.MaxTradeAmount, r.MaxTradeAmount); err != nil {
			return nil, err
		}
		if err = decErr(scope, &ad.MinTradeAmount, r.MinTradeAmount); err != nil {
			return nil, err
		}
		if err = decErr(scope, &ad.TurnoverRate, r.TurnoverRate); err != nil {
			return nil, err
		}
		ad.UserLimit = commontypes.P2PAdUserLimit{
			MinCompleteNum: r.UserLimitList.MinCompleteNum, MaxCompleteNum: r.UserLimitList.MaxCompleteNum,
			PlaceOrderNum: r.UserLimitList.PlaceOrderNum, AllowMerchantPlace: r.UserLimitList.AllowMerchantPlace,
			CompleteRate30d: r.UserLimitList.CompleteRate30d, Country: r.UserLimitList.Country,
		}
		var j int
		ad.PaymentMethods = make([]commontypes.P2PAdPaymentMethod, 0, len(r.PaymentMethodList))
		for j = 0; j < len(r.PaymentMethodList); j++ {
			var pm = r.PaymentMethodList[j]
			var method commontypes.P2PAdPaymentMethod = commontypes.P2PAdPaymentMethod{
				PaymentMethod: pm.PaymentMethod, PaymentID: pm.PaymentID,
			}
			var k int
			method.PaymentInfo = make([]commontypes.P2PAdPaymentField, 0, len(pm.PaymentInfo))
			for k = 0; k < len(pm.PaymentInfo); k++ {
				method.PaymentInfo = append(method.PaymentInfo, commontypes.P2PAdPaymentField{
					Name: pm.PaymentInfo[k].Name, Required: pm.PaymentInfo[k].Required, Type: pm.PaymentInfo[k].Type,
				})
			}
			ad.PaymentMethods = append(ad.PaymentMethods, method)
		}
		ad.Certified = make([]commontypes.P2PAdCertified, 0, len(r.MerchantCertifiedList))
		for j = 0; j < len(r.MerchantCertifiedList); j++ {
			ad.Certified = append(ad.Certified, commontypes.P2PAdCertified{
				ImageURL: r.MerchantCertifiedList[j].ImageURL, Desc: r.MerchantCertifiedList[j].Desc,
			})
		}
		out = append(out, ad)
	}
	return out, nil
}
