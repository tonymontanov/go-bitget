/*
FILE: common/types/p2p.go

DESCRIPTION:
Domain types for the COMMON P2P merchant endpoints (/api/v2/p2p/*). Field
mapping verified against the Bitget V2 docs and the tiagosiebler reference
types (P2PMerchantV2 / P2PMerchantInfoV2 / P2PMerchantOrderV2 /
P2PMerchantAdvertismentV2). Money / amounts use decimal, timestamps int64
ms, counts int64; precisions / durations / opaque labels stay as strings.
*/

package types

import "github.com/shopspring/decimal"

// P2PMerchant — one row of GET p2p/merchantList.
type P2PMerchant struct {
	NickName            string
	IsOnline            string
	RegisterTimeMs      int64
	AvgPaymentTime      string
	AvgReleaseTime      string
	TotalTrades         int64
	TotalBuy            int64
	TotalSell           int64
	TotalCompletionRate decimal.Decimal
	Trades30d           int64
	Sell30d             int64
	Buy30d              int64
	CompletionRate30d   decimal.Decimal
}

// P2PMerchantInfo — GET p2p/merchantInfo: the caller's own merchant
// profile (superset of P2PMerchant with identity / binding fields).
type P2PMerchantInfo struct {
	MerchantID          string
	NickName            string
	RegisterTimeMs      int64
	AvgPaymentTime      string
	AvgReleaseTime      string
	TotalTrades         int64
	TotalBuy            int64
	TotalSell           int64
	TotalCompletionRate decimal.Decimal
	Trades30d           int64
	Sell30d             int64
	Buy30d              int64
	CompletionRate30d   decimal.Decimal
	KycStatus           bool
	EmailBindStatus     bool
	MobileBindStatus    bool
	Email               string
	Mobile              string
}

// P2POrderPaymethodField — one key/value field of an order's payment
// method.
type P2POrderPaymethodField struct {
	Name     string
	Required string
	Type     string
	Value    string
}

// P2POrderPaymentInfo — the payment method attached to a P2P order.
type P2POrderPaymentInfo struct {
	PaymethodName string
	PaymethodID   string
	PaymethodInfo []P2POrderPaymethodField
}

// P2PMerchantOrder — one row of GET p2p/orderList.
type P2PMerchantOrder struct {
	OrderID         string
	OrderNo         string
	AdvNo           string
	Side            string
	Coin            string
	Fiat            string
	Count           decimal.Decimal
	Price           decimal.Decimal
	Amount          decimal.Decimal
	Status          string
	BuyerRealName   string
	SellerRealName  string
	WithdrawTimeMs  int64
	RepresentTimeMs int64
	ReleaseTimeMs   int64
	PaymentTimeMs   int64
	CTimeMs         int64
	UTimeMs         int64
	PaymentInfo     P2POrderPaymentInfo
}

// P2PAdUserLimit — the user-eligibility limits of an advertisement.
type P2PAdUserLimit struct {
	MinCompleteNum     string
	MaxCompleteNum     string
	PlaceOrderNum      string
	AllowMerchantPlace string
	CompleteRate30d    string
	Country            string
}

// P2PAdPaymentField — one field descriptor of an ad payment method.
type P2PAdPaymentField struct {
	Name     string
	Required bool
	Type     string
}

// P2PAdPaymentMethod — one payment method offered by an advertisement.
type P2PAdPaymentMethod struct {
	PaymentMethod string
	PaymentID     string
	PaymentInfo   []P2PAdPaymentField
}

// P2PAdCertified — one merchant-certification badge on an advertisement.
type P2PAdCertified struct {
	ImageURL string
	Desc     string
}

// P2PMerchantAd — one row of GET p2p/advList.
type P2PMerchantAd struct {
	AdvID          string
	AdvNo          string
	Side           string
	Coin           string
	Fiat           string
	FiatSymbol     string
	Status         string
	Hide           string
	Label          string
	CoinPrecision  string
	FiatPrecision  string
	PayDuration    string
	AdvSize        decimal.Decimal
	Size           decimal.Decimal
	Price          decimal.Decimal
	MaxTradeAmount decimal.Decimal
	MinTradeAmount decimal.Decimal
	TurnoverNum    int64
	TurnoverRate   decimal.Decimal
	UserLimit      P2PAdUserLimit
	PaymentMethods []P2PAdPaymentMethod
	Certified      []P2PAdCertified
	CTimeMs        int64
	UTimeMs        int64
}
