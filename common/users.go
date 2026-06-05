/*
FILE: common/users.go

DESCRIPTION:
Users sub-client — virtual sub-account + API-key management
(/api/v2/user/...). All signed. These manage MAIN-account virtual
sub-accounts (distinct from the broker/ sub-accounts).

create / modify (sub-account or API key) change account state and mint
real credentials — the SDK validates only the obvious client-side
preconditions. The returned SecretKey is available exactly once.

Request params verified against the Bitget V2 docs and the tiagosiebler
reference client. The create-subaccount response uses the venue's
"subaAccountUid"/"subaAccountName" spelling; both spellings are accepted
defensively.
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

// UserClient — virtual sub-account management sub-client.
type UserClient struct {
	c *Client
}

func newUserClient(c *Client) *UserClient {
	return &UserClient{c: c}
}

// ---------------------------------------------------------------------
// CreateSubAccounts — user/create-virtual-subaccount.
// ---------------------------------------------------------------------

type createVirtualSubBody struct {
	SubAccountList []string `json:"subAccountList"`
}

type createVirtualSubRow struct {
	// venue spells these "suba…"; accept the plain spelling too.
	SubaAccountUID  string   `json:"subaAccountUid"`
	SubaAccountName string   `json:"subaAccountName"`
	SubAccountUID   string   `json:"subAccountUid"`
	SubAccountName  string   `json:"subAccountName"`
	Status          string   `json:"status"`
	Label           string   `json:"label"`
	PermList        []string `json:"permList"`
	CTime           string   `json:"cTime"`
	UTime           string   `json:"uTime"`
}

type createVirtualSubEnvelope struct {
	FailureList []struct {
		SubaAccountName string `json:"subaAccountName"`
		SubAccountName  string `json:"subAccountName"`
	} `json:"failureList"`
	SuccessList []createVirtualSubRow `json:"successList"`
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// CreateSubAccounts creates one or more virtual sub-accounts by name.
// names is required. NOTE: changes account state.
func (u *UserClient) CreateSubAccounts(ctx context.Context, names []string) (commontypes.CreateVirtualSubResult, error) {
	var out commontypes.CreateVirtualSubResult
	if len(names) == 0 {
		return out, errInvalid("Users.CreateSubAccounts", "subAccountList is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = u.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/user/create-virtual-subaccount",
		Body:   createVirtualSubBody{SubAccountList: names},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var env createVirtualSubEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("Users.CreateSubAccounts", err)
	}
	var i int
	out.SuccessList = make([]commontypes.VirtualSubCreated, 0, len(env.SuccessList))
	for i = 0; i < len(env.SuccessList); i++ {
		var r = env.SuccessList[i]
		var c commontypes.VirtualSubCreated = commontypes.VirtualSubCreated{
			SubAccountUID:  firstNonEmpty(r.SubaAccountUID, r.SubAccountUID),
			SubAccountName: firstNonEmpty(r.SubaAccountName, r.SubAccountName),
			Status:         r.Status,
			Label:          r.Label,
			PermList:       r.PermList,
		}
		c.CTimeMs, _ = bgcommon.ParseInt64OrZero(r.CTime)
		c.UTimeMs, _ = bgcommon.ParseInt64OrZero(r.UTime)
		out.SuccessList = append(out.SuccessList, c)
	}
	out.FailureList = make([]string, 0, len(env.FailureList))
	for i = 0; i < len(env.FailureList); i++ {
		out.FailureList = append(out.FailureList, firstNonEmpty(env.FailureList[i].SubaAccountName, env.FailureList[i].SubAccountName))
	}
	return out, nil
}

// ---------------------------------------------------------------------
// ModifySubAccount — user/modify-virtual-subaccount.
// ---------------------------------------------------------------------

// ModifySubAccountRequest — body for ModifySubAccount.
type ModifySubAccountRequest struct {
	SubAccountUID string
	PermList      []string
	Status        string
}

type modifyVirtualSubBody struct {
	SubAccountUID string   `json:"subAccountUid"`
	PermList      []string `json:"permList"`
	Status        string   `json:"status"`
}

// ModifySubAccount updates a virtual sub-account's permissions / status.
// SubAccountUID and Status are required. Returns the venue's result
// string. NOTE: changes account state.
func (u *UserClient) ModifySubAccount(ctx context.Context, req ModifySubAccountRequest) (string, error) {
	switch {
	case req.SubAccountUID == "":
		return "", errInvalid("Users.ModifySubAccount", "subAccountUid is required")
	case req.Status == "":
		return "", errInvalid("Users.ModifySubAccount", "status is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = u.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/user/modify-virtual-subaccount",
		Body: modifyVirtualSubBody{
			SubAccountUID: req.SubAccountUID,
			PermList:      req.PermList,
			Status:        req.Status,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row struct {
		Result string `json:"result"`
	}
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("Users.ModifySubAccount", err)
	}
	return row.Result, nil
}

// ---------------------------------------------------------------------
// BatchCreateSubAccountAndAPIKey — user/batch-create-subaccount-and-apikey.
// ---------------------------------------------------------------------

// CreateSubAndKeyRequest — body for BatchCreateSubAccountAndAPIKey.
type CreateSubAndKeyRequest struct {
	SubAccountName string
	Passphrase     string
	Label          string
	IPList         []string
	PermList       []string
}

type batchCreateBody struct {
	SubAccountName string   `json:"subAccountName"`
	Passphrase     string   `json:"passphrase"`
	Label          string   `json:"label"`
	IPList         []string `json:"ipList,omitempty"`
	PermList       []string `json:"permList,omitempty"`
}

type virtualSubKeyRow struct {
	SubAccountUID  string   `json:"subAccountUid"`
	SubAccountName string   `json:"subAccountName"`
	Label          string   `json:"label"`
	SubAccountKey  string   `json:"subAccountApiKey"`
	SecretKey      string   `json:"secretKey"`
	PermList       []string `json:"permList"`
	IPList         []string `json:"ipList"`
}

func (r virtualSubKeyRow) toDomain() commontypes.VirtualSubAPIKey {
	return commontypes.VirtualSubAPIKey{
		SubAccountUID:  r.SubAccountUID,
		SubAccountName: r.SubAccountName,
		Label:          r.Label,
		APIKey:         r.SubAccountKey,
		SecretKey:      r.SecretKey,
		PermList:       r.PermList,
		IPList:         r.IPList,
	}
}

// BatchCreateSubAccountAndAPIKey creates a virtual sub-account together
// with its first API key in one call. SubAccountName and Passphrase are
// required. The returned SecretKey(s) are available only here. NOTE:
// changes account state and mints real credentials.
func (u *UserClient) BatchCreateSubAccountAndAPIKey(ctx context.Context, req CreateSubAndKeyRequest) ([]commontypes.VirtualSubAPIKey, error) {
	switch {
	case req.SubAccountName == "":
		return nil, errInvalid("Users.BatchCreateSubAccountAndAPIKey", "subAccountName is required")
	case req.Passphrase == "":
		return nil, errInvalid("Users.BatchCreateSubAccountAndAPIKey", "passphrase is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = u.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/user/batch-create-subaccount-and-apikey",
		Body: batchCreateBody{
			SubAccountName: req.SubAccountName,
			Passphrase:     req.Passphrase,
			Label:          req.Label,
			IPList:         req.IPList,
			PermList:       req.PermList,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []virtualSubKeyRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Users.BatchCreateSubAccountAndAPIKey", err)
	}
	var out []commontypes.VirtualSubAPIKey = make([]commontypes.VirtualSubAPIKey, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		out = append(out, rows[i].toDomain())
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetSubAccounts — user/virtual-subaccount-list (cursor).
// ---------------------------------------------------------------------

type virtualSubRow struct {
	SubAccountUID  string   `json:"subAccountUid"`
	SubAccountName string   `json:"subAccountName"`
	Status         string   `json:"status"`
	PermList       []string `json:"permList"`
	Label          string   `json:"label"`
	AccountType    string   `json:"accountType"`
	BindingTime    string   `json:"bindingTime"`
	CTime          string   `json:"cTime"`
	UTime          string   `json:"uTime"`
}

type virtualSubListEnvelope struct {
	EndID          string          `json:"endId"`
	SubAccountList []virtualSubRow `json:"subAccountList"`
}

// GetSubAccounts lists the caller's virtual sub-accounts. status filters
// ("normal" | "freeze", optional). Walks the idLessThan/endId cursor.
func (u *UserClient) GetSubAccounts(ctx context.Context, status string) ([]commontypes.VirtualSubAccount, error) {
	var rows []virtualSubRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "common.Users.GetSubAccounts",
		func(idLessThan string, limit int) ([]virtualSubRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if status != "" {
				query.Set("status", status)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			var resp rest.Response
			var ferr error
			resp, _, ferr = u.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/user/virtual-subaccount-list",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env virtualSubListEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Users.GetSubAccounts", ferr)
			}
			return env.SubAccountList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}
	var out []commontypes.VirtualSubAccount = make([]commontypes.VirtualSubAccount, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var v commontypes.VirtualSubAccount = commontypes.VirtualSubAccount{
			SubAccountUID:  r.SubAccountUID,
			SubAccountName: r.SubAccountName,
			Status:         r.Status,
			Label:          r.Label,
			AccountType:    r.AccountType,
			PermList:       r.PermList,
		}
		v.BindingTimeMs, _ = bgcommon.ParseInt64OrZero(r.BindingTime)
		v.CTimeMs, _ = bgcommon.ParseInt64OrZero(r.CTime)
		v.UTimeMs, _ = bgcommon.ParseInt64OrZero(r.UTime)
		out = append(out, v)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// CreateAPIKey — user/create-virtual-subaccount-apikey.
// ---------------------------------------------------------------------

// CreateSubAPIKeyRequest — body for CreateAPIKey.
type CreateSubAPIKeyRequest struct {
	SubAccountUID string
	Passphrase    string
	Label         string
	IPList        []string
	PermList      []string
}

type createSubKeyBody struct {
	SubAccountUID string   `json:"subAccountUid"`
	Passphrase    string   `json:"passphrase"`
	Label         string   `json:"label"`
	IPList        []string `json:"ipList,omitempty"`
	PermList      []string `json:"permList,omitempty"`
}

// CreateAPIKey mints a new API key for a virtual sub-account.
// SubAccountUID and Passphrase are required. SecretKey is returned only
// here. NOTE: mints real credentials.
func (u *UserClient) CreateAPIKey(ctx context.Context, req CreateSubAPIKeyRequest) (commontypes.VirtualSubAPIKey, error) {
	var out commontypes.VirtualSubAPIKey
	switch {
	case req.SubAccountUID == "":
		return out, errInvalid("Users.CreateAPIKey", "subAccountUid is required")
	case req.Passphrase == "":
		return out, errInvalid("Users.CreateAPIKey", "passphrase is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = u.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/user/create-virtual-subaccount-apikey",
		Body: createSubKeyBody{
			SubAccountUID: req.SubAccountUID,
			Passphrase:    req.Passphrase,
			Label:         req.Label,
			IPList:        req.IPList,
			PermList:      req.PermList,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row virtualSubKeyRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Users.CreateAPIKey", err)
	}
	return row.toDomain(), nil
}

// ---------------------------------------------------------------------
// ModifyAPIKey — user/modify-virtual-subaccount-apikey.
// ---------------------------------------------------------------------

// ModifySubAPIKeyRequest — body for ModifyAPIKey.
type ModifySubAPIKeyRequest struct {
	SubAccountUID string
	APIKey        string
	Passphrase    string
	Label         string
	IPList        []string
	PermList      []string
}

type modifySubKeyBody struct {
	SubAccountUID string   `json:"subAccountUid"`
	APIKey        string   `json:"subAccountApiKey"`
	Passphrase    string   `json:"passphrase"`
	Label         string   `json:"label"`
	IPList        []string `json:"ipList,omitempty"`
	PermList      []string `json:"permList,omitempty"`
}

// ModifyAPIKey updates a virtual sub-account API key's permissions / IPs /
// label. SubAccountUID, APIKey and Passphrase are required. NOTE: changes
// credential settings.
func (u *UserClient) ModifyAPIKey(ctx context.Context, req ModifySubAPIKeyRequest) (commontypes.VirtualSubAPIKey, error) {
	var out commontypes.VirtualSubAPIKey
	switch {
	case req.SubAccountUID == "":
		return out, errInvalid("Users.ModifyAPIKey", "subAccountUid is required")
	case req.APIKey == "":
		return out, errInvalid("Users.ModifyAPIKey", "subAccountApiKey is required")
	case req.Passphrase == "":
		return out, errInvalid("Users.ModifyAPIKey", "passphrase is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = u.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/user/modify-virtual-subaccount-apikey",
		Body: modifySubKeyBody{
			SubAccountUID: req.SubAccountUID,
			APIKey:        req.APIKey,
			Passphrase:    req.Passphrase,
			Label:         req.Label,
			IPList:        req.IPList,
			PermList:      req.PermList,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row virtualSubKeyRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Users.ModifyAPIKey", err)
	}
	return row.toDomain(), nil
}

// ---------------------------------------------------------------------
// GetAPIKeys — user/virtual-subaccount-apikey-list.
// ---------------------------------------------------------------------

type virtualSubKeyItemRow struct {
	SubAccountUID string   `json:"subAccountUid"`
	Label         string   `json:"label"`
	SubAccountKey string   `json:"subAccountApiKey"`
	PermList      []string `json:"permList"`
	IPList        []string `json:"ipList"`
}

// GetAPIKeys lists a virtual sub-account's API keys (no SecretKey).
// subAccountUID is required.
func (u *UserClient) GetAPIKeys(ctx context.Context, subAccountUID string) ([]commontypes.VirtualSubAPIKeyItem, error) {
	if subAccountUID == "" {
		return nil, errInvalid("Users.GetAPIKeys", "subAccountUid is required")
	}

	var query url.Values = url.Values{}
	query.Set("subAccountUid", subAccountUID)

	var resp rest.Response
	var err error
	resp, _, err = u.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/user/virtual-subaccount-apikey-list",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []virtualSubKeyItemRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Users.GetAPIKeys", err)
	}
	var out []commontypes.VirtualSubAPIKeyItem = make([]commontypes.VirtualSubAPIKeyItem, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		out = append(out, commontypes.VirtualSubAPIKeyItem{
			SubAccountUID: rows[i].SubAccountUID,
			Label:         rows[i].Label,
			APIKey:        rows[i].SubAccountKey,
			PermList:      rows[i].PermList,
			IPList:        rows[i].IPList,
		})
	}
	return out, nil
}
