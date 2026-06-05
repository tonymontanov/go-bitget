/*
FILE: broker/subaccounts.go

DESCRIPTION:
Sub-account sub-client — /api/v2/broker/account/... plus the ND-broker
deposit/withdrawal record reads (/api/v2/broker/subaccount-deposit,
/api/v2/broker/subaccount-withdrawal, /api/v2/broker/all-sub-deposit-withdrawal).

Covers the full managed sub-account lifecycle: quota info, create, list,
modify, email get/set, spot/futures asset snapshots, deposit-address
creation, withdrawal, auto-transfer config, and the three record feeds.

Request params verified against the Bitget V2 broker docs and the
tiagosiebler reference client. Mutating calls (Create / Withdraw /
SetAutoTransfer / Modify) move funds or change account state — the SDK
only validates the obvious client-side preconditions.
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

// SubAccountClient — broker sub-account lifecycle sub-client.
type SubAccountClient struct {
	c *Client
}

func newSubAccountClient(c *Client) *SubAccountClient {
	return &SubAccountClient{c: c}
}

// decErr parses raw into *dst, wrapping a failure as a scoped parse error.
func decErr(scope string, dst *decimal.Decimal, raw string) error {
	var err error
	if *dst, err = bgcommon.ParseDecimalOrZero(raw); err != nil {
		return errParse(scope, err)
	}
	return nil
}

// ---------------------------------------------------------------------
// GetInfo — broker/account/info.
// ---------------------------------------------------------------------

type brokerInfoRow struct {
	SubAccountSize    string `json:"subAccountSize"`
	MaxSubAccountSize string `json:"maxSubAccountSize"`
	UTime             string `json:"uTime"`
}

// GetInfo returns the broker's sub-account quota (current / max).
func (s *SubAccountClient) GetInfo(ctx context.Context) (brokertypes.BrokerInfo, error) {
	var out brokertypes.BrokerInfo
	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/account/info",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row brokerInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SubAccounts.GetInfo", err)
	}
	out.SubAccountSize, _ = bgcommon.ParseInt64OrZero(row.SubAccountSize)
	out.MaxSubAccountSize, _ = bgcommon.ParseInt64OrZero(row.MaxSubAccountSize)
	out.UTimeMs, _ = bgcommon.ParseInt64OrZero(row.UTime)
	return out, nil
}

// ---------------------------------------------------------------------
// Sub-account row decode shared by Create / List / Modify.
// ---------------------------------------------------------------------

type subAccountRow struct {
	SubUID         string   `json:"subUid"`
	SubaccountName string   `json:"subaccountName"`
	Status         string   `json:"status"`
	Label          string   `json:"label"`
	Language       string   `json:"language"`
	PermList       []string `json:"permList"`
	CTime          string   `json:"cTime"`
	UTime          string   `json:"uTime"`
}

func (r subAccountRow) toDomain() brokertypes.SubAccount {
	var a brokertypes.SubAccount = brokertypes.SubAccount{
		SubUID:         r.SubUID,
		SubaccountName: r.SubaccountName,
		Status:         r.Status,
		Label:          r.Label,
		Language:       r.Language,
		PermList:       r.PermList,
	}
	a.CTimeMs, _ = bgcommon.ParseInt64OrZero(r.CTime)
	a.UTimeMs, _ = bgcommon.ParseInt64OrZero(r.UTime)
	return a
}

// ---------------------------------------------------------------------
// Create — broker/account/create-subaccount.
// ---------------------------------------------------------------------

type createSubBody struct {
	SubaccountName string `json:"subaccountName"`
	Label          string `json:"label"`
}

// Create provisions a new managed sub-account. subaccountName is required;
// label is an optional free-text tag. NOTE: this creates real account
// state under the broker.
func (s *SubAccountClient) Create(ctx context.Context, subaccountName, label string) (brokertypes.SubAccount, error) {
	var out brokertypes.SubAccount
	if subaccountName == "" {
		return out, errInvalid("SubAccounts.Create", "subaccountName is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/account/create-subaccount",
		Body:   createSubBody{SubaccountName: subaccountName, Label: label},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row subAccountRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SubAccounts.Create", err)
	}
	return row.toDomain(), nil
}

// ---------------------------------------------------------------------
// List — broker/account/subaccount-list (hasNextPage / idLessThan paged).
// ---------------------------------------------------------------------

// SubAccountsQuery — optional filters for List.
//
//	Status: normal | freeze | del
type SubAccountsQuery struct {
	Status      string
	StartTimeMs int64
	EndTimeMs   int64
}

type subAccountListEnvelope struct {
	HasNextPage bool            `json:"hasNextPage"`
	IDLessThan  int64           `json:"idLessThan"`
	SubList     []subAccountRow `json:"subList"`
}

// List returns the broker's managed sub-accounts. Walks the
// hasNextPage / idLessThan cursor until the server reports no next page
// (bounded by the page ceiling).
func (s *SubAccountClient) List(ctx context.Context, q SubAccountsQuery) ([]brokertypes.SubAccount, error) {
	const pageLimit = 100
	var out []brokertypes.SubAccount
	var idLessThan string
	var page int
	for page = 1; page <= pageNoMaxPages; page++ {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}

		var query url.Values = url.Values{}
		query.Set("limit", strconv.Itoa(pageLimit))
		if q.Status != "" {
			query.Set("status", q.Status)
		}
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
		var err error
		resp, _, err = s.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/broker/account/subaccount-list",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}
		var env subAccountListEnvelope
		if err = resp.UnmarshalData(&env); err != nil {
			return nil, errParse("SubAccounts.List", err)
		}
		var i int
		for i = 0; i < len(env.SubList); i++ {
			out = append(out, env.SubList[i].toDomain())
		}
		if !env.HasNextPage || len(env.SubList) == 0 || env.IDLessThan <= 0 {
			break
		}
		idLessThan = strconv.FormatInt(env.IDLessThan, 10)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Modify — broker/account/modify-subaccount.
// ---------------------------------------------------------------------

type modifySubBody struct {
	SubUID   string   `json:"subUid"`
	PermList []string `json:"permList"`
	Status   string   `json:"status"`
	Language string   `json:"language,omitempty"`
}

// Modify updates a sub-account's permissions / status / language. SubUID,
// PermList and Status are required.
func (s *SubAccountClient) Modify(ctx context.Context, req brokertypes.ModifySubAccountRequest) (brokertypes.SubAccount, error) {
	var out brokertypes.SubAccount
	switch {
	case req.SubUID == "":
		return out, errInvalid("SubAccounts.Modify", "subUid is required")
	case len(req.PermList) == 0:
		return out, errInvalid("SubAccounts.Modify", "permList is required")
	case req.Status == "":
		return out, errInvalid("SubAccounts.Modify", "status is required (normal | freeze)")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/account/modify-subaccount",
		Body: modifySubBody{
			SubUID:   req.SubUID,
			PermList: req.PermList,
			Status:   req.Status,
			Language: req.Language,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row subAccountRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SubAccounts.Modify", err)
	}
	return row.toDomain(), nil
}

// ---------------------------------------------------------------------
// ModifyEmail / GetEmail — broker/account/{modify-subaccount-email,subaccount-email}.
// ---------------------------------------------------------------------

type modifyEmailBody struct {
	SubUID          string `json:"subUid"`
	SubaccountEmail string `json:"subaccountEmail"`
}

// ModifyEmail binds (or rebinds) the sub-account's email. Both args are
// required.
func (s *SubAccountClient) ModifyEmail(ctx context.Context, subUID, email string) error {
	switch {
	case subUID == "":
		return errInvalid("SubAccounts.ModifyEmail", "subUid is required")
	case email == "":
		return errInvalid("SubAccounts.ModifyEmail", "subaccountEmail is required")
	}
	var _, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/account/modify-subaccount-email",
		Body:   modifyEmailBody{SubUID: subUID, SubaccountEmail: email},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

type subEmailRow struct {
	SubUID          string `json:"subUid"`
	SubaccountName  string `json:"subaccountName"`
	SubaccountEmail string `json:"subaccountEmail"`
	CTime           string `json:"cTime"`
	UTime           string `json:"uTime"`
}

// GetEmail returns the email bound to the sub-account. subUID is required.
func (s *SubAccountClient) GetEmail(ctx context.Context, subUID string) (brokertypes.SubAccountEmail, error) {
	var out brokertypes.SubAccountEmail
	if subUID == "" {
		return out, errInvalid("SubAccounts.GetEmail", "subUid is required")
	}

	var query url.Values = url.Values{}
	query.Set("subUid", subUID)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/account/subaccount-email",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row subEmailRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SubAccounts.GetEmail", err)
	}
	out.SubUID = row.SubUID
	out.SubaccountName = row.SubaccountName
	out.SubaccountEmail = row.SubaccountEmail
	out.CTimeMs, _ = bgcommon.ParseInt64OrZero(row.CTime)
	out.UTimeMs, _ = bgcommon.ParseInt64OrZero(row.UTime)
	return out, nil
}

// ---------------------------------------------------------------------
// GetSpotAssets — broker/account/subaccount-spot-assets.
// ---------------------------------------------------------------------

type spotAssetRow struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	Frozen    string `json:"frozen"`
	Locked    string `json:"locked"`
	UTime     string `json:"uTime"`
}

type spotAssetsEnvelope struct {
	AssetsList []spotAssetRow `json:"assetsList"`
}

// GetSpotAssets returns the sub-account spot balances. subUID is required;
// coin filters to one asset; assetType is "hold_only" | "all" (optional).
func (s *SubAccountClient) GetSpotAssets(ctx context.Context, subUID, coin, assetType string) ([]brokertypes.SubAccountSpotAsset, error) {
	if subUID == "" {
		return nil, errInvalid("SubAccounts.GetSpotAssets", "subUid is required")
	}

	var query url.Values = url.Values{}
	query.Set("subUid", subUID)
	if coin != "" {
		query.Set("coin", coin)
	}
	if assetType != "" {
		query.Set("assetType", assetType)
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/account/subaccount-spot-assets",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var env spotAssetsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, errParse("SubAccounts.GetSpotAssets", err)
	}
	var out []brokertypes.SubAccountSpotAsset = make([]brokertypes.SubAccountSpotAsset, 0, len(env.AssetsList))
	var i int
	for i = 0; i < len(env.AssetsList); i++ {
		var a brokertypes.SubAccountSpotAsset = brokertypes.SubAccountSpotAsset{Coin: env.AssetsList[i].Coin}
		if err = decErr("SubAccounts.GetSpotAssets", &a.Available, env.AssetsList[i].Available); err != nil {
			return nil, err
		}
		if err = decErr("SubAccounts.GetSpotAssets", &a.Frozen, env.AssetsList[i].Frozen); err != nil {
			return nil, err
		}
		if err = decErr("SubAccounts.GetSpotAssets", &a.Locked, env.AssetsList[i].Locked); err != nil {
			return nil, err
		}
		a.UTimeMs, _ = bgcommon.ParseInt64OrZero(env.AssetsList[i].UTime)
		out = append(out, a)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetFuturesAssets — broker/account/subaccount-future-assets.
// ---------------------------------------------------------------------

type futureAssetRow struct {
	MarginCoin           string `json:"marginCoin"`
	Available            string `json:"available"`
	Frozen               string `json:"frozen"`
	Locked               string `json:"locked"`
	CrossedMaxAvailable  string `json:"crossedMaxAvailable"`
	IsolatedMaxAvailable string `json:"isolatedMaxAvailable"`
	MaxTransferOut       string `json:"maxTransferOut"`
	AccountEquity        string `json:"accountEquity"`
	UsdtEquity           string `json:"usdtEquity"`
	BtcEquity            string `json:"btcEquity"`
	UTime                string `json:"uTime"`
}

type futureAssetsEnvelope struct {
	AssetsList []futureAssetRow `json:"assetsList"`
}

// GetFuturesAssets returns the sub-account futures balances for the given
// productType. subUID and productType are required (productType is the
// native value, e.g. "USDT-FUTURES").
func (s *SubAccountClient) GetFuturesAssets(ctx context.Context, subUID, productType string) ([]brokertypes.SubAccountFuturesAsset, error) {
	switch {
	case subUID == "":
		return nil, errInvalid("SubAccounts.GetFuturesAssets", "subUid is required")
	case productType == "":
		return nil, errInvalid("SubAccounts.GetFuturesAssets", "productType is required")
	}

	var query url.Values = url.Values{}
	query.Set("subUid", subUID)
	query.Set("productType", productType)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/account/subaccount-future-assets",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var env futureAssetsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, errParse("SubAccounts.GetFuturesAssets", err)
	}
	var out []brokertypes.SubAccountFuturesAsset = make([]brokertypes.SubAccountFuturesAsset, 0, len(env.AssetsList))
	var i int
	for i = 0; i < len(env.AssetsList); i++ {
		var r futureAssetRow = env.AssetsList[i]
		var a brokertypes.SubAccountFuturesAsset = brokertypes.SubAccountFuturesAsset{MarginCoin: r.MarginCoin}
		var scope string = "SubAccounts.GetFuturesAssets"
		if err = decErr(scope, &a.Available, r.Available); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.Frozen, r.Frozen); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.Locked, r.Locked); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.CrossedMaxAvailable, r.CrossedMaxAvailable); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.IsolatedMaxAvailable, r.IsolatedMaxAvailable); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.MaxTransferOut, r.MaxTransferOut); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.AccountEquity, r.AccountEquity); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.USDTEquity, r.UsdtEquity); err != nil {
			return nil, err
		}
		if err = decErr(scope, &a.BTCEquity, r.BtcEquity); err != nil {
			return nil, err
		}
		a.UTimeMs, _ = bgcommon.ParseInt64OrZero(r.UTime)
		out = append(out, a)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// CreateDepositAddress — broker/account/subaccount-address.
// ---------------------------------------------------------------------

type createAddrBody struct {
	SubUID string `json:"subUid"`
	Coin   string `json:"coin"`
	Chain  string `json:"chain,omitempty"`
}

type createAddrRow struct {
	SubUID  string `json:"subUid"`
	Coin    string `json:"coin"`
	Address string `json:"address"`
	Chain   string `json:"chain"`
	Tag     string `json:"tag"`
	URL     string `json:"url"`
	CTime   string `json:"cTime"`
}

// CreateDepositAddress provisions a deposit address for the sub-account.
// subUID and coin are required; chain is optional (defaults to the coin's
// primary chain).
func (s *SubAccountClient) CreateDepositAddress(ctx context.Context, subUID, coin, chain string) (brokertypes.SubAccountDepositAddress, error) {
	var out brokertypes.SubAccountDepositAddress
	switch {
	case subUID == "":
		return out, errInvalid("SubAccounts.CreateDepositAddress", "subUid is required")
	case coin == "":
		return out, errInvalid("SubAccounts.CreateDepositAddress", "coin is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/account/subaccount-address",
		Body:   createAddrBody{SubUID: subUID, Coin: coin, Chain: chain},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row createAddrRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SubAccounts.CreateDepositAddress", err)
	}
	out.SubUID = row.SubUID
	out.Coin = row.Coin
	out.Address = row.Address
	out.Chain = row.Chain
	out.Tag = row.Tag
	out.URL = row.URL
	out.CTimeMs, _ = bgcommon.ParseInt64OrZero(row.CTime)
	return out, nil
}

// ---------------------------------------------------------------------
// Withdraw — broker/account/subaccount-withdrawal.
// ---------------------------------------------------------------------

type subWithdrawBody struct {
	SubUID    string `json:"subUid"`
	Coin      string `json:"coin"`
	Dest      string `json:"dest"`
	Chain     string `json:"chain,omitempty"`
	Address   string `json:"address"`
	Amount    string `json:"amount"`
	Tag       string `json:"tag,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

type subWithdrawRow struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// Withdraw initiates a withdrawal from a sub-account. SubUID, Coin, Dest
// ("on_chain" | "internal_transfer"), Address and Amount are required;
// Chain is required for on-chain withdrawals. NOTE: this moves real funds.
func (s *SubAccountClient) Withdraw(ctx context.Context, req brokertypes.SubWithdrawalRequest) (brokertypes.SubWithdrawalResult, error) {
	var out brokertypes.SubWithdrawalResult
	switch {
	case req.SubUID == "":
		return out, errInvalid("SubAccounts.Withdraw", "subUid is required")
	case req.Coin == "":
		return out, errInvalid("SubAccounts.Withdraw", "coin is required")
	case req.Dest == "":
		return out, errInvalid("SubAccounts.Withdraw", "dest is required (on_chain | internal_transfer)")
	case req.Address == "":
		return out, errInvalid("SubAccounts.Withdraw", "address is required")
	case req.Amount == "":
		return out, errInvalid("SubAccounts.Withdraw", "amount is required")
	case req.Dest == "on_chain" && req.Chain == "":
		return out, errInvalid("SubAccounts.Withdraw", "chain is required for on_chain withdrawals")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/account/subaccount-withdrawal",
		Body: subWithdrawBody{
			SubUID:    req.SubUID,
			Coin:      req.Coin,
			Dest:      req.Dest,
			Chain:     req.Chain,
			Address:   req.Address,
			Amount:    req.Amount,
			Tag:       req.Tag,
			ClientOid: req.ClientOid,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row subWithdrawRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SubAccounts.Withdraw", err)
	}
	out.OrderID = row.OrderID
	out.ClientOid = row.ClientOid
	return out, nil
}

// ---------------------------------------------------------------------
// SetAutoTransfer — broker/account/set-subaccount-autotransfer.
// ---------------------------------------------------------------------

type autoTransferBody struct {
	SubUID        string `json:"subUid"`
	Coin          string `json:"coin"`
	ToAccountType string `json:"toAccountType"`
}

// SetAutoTransfer configures auto-transfer of a coin from the sub-account
// to the given account type. All three args are required.
func (s *SubAccountClient) SetAutoTransfer(ctx context.Context, subUID, coin, toAccountType string) error {
	switch {
	case subUID == "":
		return errInvalid("SubAccounts.SetAutoTransfer", "subUid is required")
	case coin == "":
		return errInvalid("SubAccounts.SetAutoTransfer", "coin is required")
	case toAccountType == "":
		return errInvalid("SubAccounts.SetAutoTransfer", "toAccountType is required")
	}
	var _, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/account/set-subaccount-autotransfer",
		Body:   autoTransferBody{SubUID: subUID, Coin: coin, ToAccountType: toAccountType},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// Record feeds — broker/subaccount-{deposit,withdrawal} + all-sub.
// ---------------------------------------------------------------------

// SubRecordsQuery — optional filters for GetDepositRecords /
// GetWithdrawalRecords.
type SubRecordsQuery struct {
	OrderID     string
	UserID      string
	StartTimeMs int64
	EndTimeMs   int64
}

func (q SubRecordsQuery) apply(v url.Values) {
	if q.OrderID != "" {
		v.Set("orderId", q.OrderID)
	}
	if q.UserID != "" {
		v.Set("userId", q.UserID)
	}
	if q.StartTimeMs > 0 {
		v.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	}
	if q.EndTimeMs > 0 {
		v.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	}
}

type subDepositRow struct {
	OrderID     string `json:"orderId"`
	TxID        string `json:"txId"`
	Coin        string `json:"coin"`
	Type        string `json:"type"`
	Dest        string `json:"dest"`
	Amount      string `json:"amount"`
	Status      string `json:"status"`
	FromAddress string `json:"fromAddress"`
	ToAddress   string `json:"toAddress"`
	Fee         string `json:"fee"`
	Chain       string `json:"chain"`
	Confirm     string `json:"confirm"`
	Tag         string `json:"tag"`
	UserID      string `json:"userId"`
	CTime       string `json:"cTime"`
	UTime       string `json:"uTime"`
}

type subRecordsEnvelope struct {
	ResultList []subDepositRow `json:"resultList"`
	EndID      string          `json:"endId"`
}

// GetDepositRecords returns ND-broker sub-account deposit records. Walks
// the idLessThan/endId cursor. (ND-broker main-account only.)
func (s *SubAccountClient) GetDepositRecords(ctx context.Context, q SubRecordsQuery) ([]brokertypes.SubDepositRecord, error) {
	var rows []subDepositRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.SubAccounts.GetDepositRecords",
		func(idLessThan string, limit int) ([]subDepositRow, string, error) {
			return s.fetchRecords(ctx, "/api/v2/broker/subaccount-deposit", q, idLessThan, limit)
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.SubDepositRecord = make([]brokertypes.SubDepositRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec brokertypes.SubDepositRecord = brokertypes.SubDepositRecord{
			OrderID:     rows[i].OrderID,
			TxID:        rows[i].TxID,
			Coin:        rows[i].Coin,
			Type:        rows[i].Type,
			Dest:        rows[i].Dest,
			Status:      rows[i].Status,
			FromAddress: rows[i].FromAddress,
			ToAddress:   rows[i].ToAddress,
			Chain:       rows[i].Chain,
			Confirm:     rows[i].Confirm,
			Tag:         rows[i].Tag,
		}
		if err = decErr("SubAccounts.GetDepositRecords", &rec.Amount, rows[i].Amount); err != nil {
			return nil, err
		}
		if err = decErr("SubAccounts.GetDepositRecords", &rec.Fee, rows[i].Fee); err != nil {
			return nil, err
		}
		rec.CTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

// GetWithdrawalRecords returns ND-broker sub-account withdrawal records.
// Walks the idLessThan/endId cursor. (ND-broker main-account only.)
func (s *SubAccountClient) GetWithdrawalRecords(ctx context.Context, q SubRecordsQuery) ([]brokertypes.SubWithdrawalRecord, error) {
	var rows []subDepositRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.SubAccounts.GetWithdrawalRecords",
		func(idLessThan string, limit int) ([]subDepositRow, string, error) {
			return s.fetchRecords(ctx, "/api/v2/broker/subaccount-withdrawal", q, idLessThan, limit)
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.SubWithdrawalRecord = make([]brokertypes.SubWithdrawalRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec brokertypes.SubWithdrawalRecord = brokertypes.SubWithdrawalRecord{
			OrderID:     rows[i].OrderID,
			TxID:        rows[i].TxID,
			Coin:        rows[i].Coin,
			Type:        rows[i].Type,
			Dest:        rows[i].Dest,
			Status:      rows[i].Status,
			FromAddress: rows[i].FromAddress,
			ToAddress:   rows[i].ToAddress,
			Chain:       rows[i].Chain,
			Confirm:     rows[i].Confirm,
			Tag:         rows[i].Tag,
			UserID:      rows[i].UserID,
		}
		if err = decErr("SubAccounts.GetWithdrawalRecords", &rec.Amount, rows[i].Amount); err != nil {
			return nil, err
		}
		if err = decErr("SubAccounts.GetWithdrawalRecords", &rec.Fee, rows[i].Fee); err != nil {
			return nil, err
		}
		rec.CTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

// fetchRecords backs GetDepositRecords / GetWithdrawalRecords — both
// return the {resultList,endId} envelope over the same row shape.
func (s *SubAccountClient) fetchRecords(ctx context.Context, path string, q SubRecordsQuery, idLessThan string, limit int) ([]subDepositRow, string, error) {
	var query url.Values = url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	q.apply(query)
	if idLessThan != "" {
		query.Set("idLessThan", idLessThan)
	}
	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, "", err
	}
	var env subRecordsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, "", errParse("SubAccounts.fetchRecords", err)
	}
	return env.ResultList, env.EndID, nil
}

// ---------------------------------------------------------------------
// GetAllRecords — broker/all-sub-deposit-withdrawal.
// ---------------------------------------------------------------------

// AllSubRecordsQuery — optional filters for GetAllRecords.
//
//	Type: all | deposit | withdrawal
type AllSubRecordsQuery struct {
	Type        string
	StartTimeMs int64
	EndTimeMs   int64
}

type allSubRow struct {
	UID     string `json:"uid"`
	TxID    string `json:"txId"`
	Type    string `json:"type"`
	SubType string `json:"subType"`
	Coin    string `json:"coin"`
	Amount  string `json:"amount"`
	Status  string `json:"status"`
	TS      string `json:"ts"`
}

type allSubEnvelope struct {
	List  []allSubRow `json:"list"`
	EndID string      `json:"endId"`
}

// GetAllRecords returns ND-broker deposit AND withdrawal records across
// all sub-accounts (within ~90 days). Walks the idLessThan/endId cursor.
func (s *SubAccountClient) GetAllRecords(ctx context.Context, q AllSubRecordsQuery) ([]brokertypes.AllSubRecord, error) {
	var rows []allSubRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "broker.SubAccounts.GetAllRecords",
		func(idLessThan string, limit int) ([]allSubRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if q.Type != "" {
				query.Set("type", q.Type)
			}
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
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/broker/all-sub-deposit-withdrawal",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env allSubEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SubAccounts.GetAllRecords", ferr)
			}
			return env.List, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []brokertypes.AllSubRecord = make([]brokertypes.AllSubRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec brokertypes.AllSubRecord = brokertypes.AllSubRecord{
			UID:     rows[i].UID,
			TxID:    rows[i].TxID,
			Type:    rows[i].Type,
			SubType: rows[i].SubType,
			Coin:    rows[i].Coin,
			Status:  rows[i].Status,
		}
		if err = decErr("SubAccounts.GetAllRecords", &rec.Amount, rows[i].Amount); err != nil {
			return nil, err
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}
