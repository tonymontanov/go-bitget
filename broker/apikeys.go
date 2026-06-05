/*
FILE: broker/apikeys.go

DESCRIPTION:
API-key sub-client — /api/v2/broker/manage/... Sub-account API-key
lifecycle: Create (returns the secretKey once), List and Modify.

Request params verified against the Bitget V2 broker docs and the
tiagosiebler reference client. Create / Modify mint or rebind real
credentials — the SDK validates the obvious client-side preconditions.
*/

package broker

import (
	"context"
	"net/url"

	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	brokertypes "github.com/tonymontanov/go-bitget/v2/broker/types"
)

// APIKeyClient — broker sub-account API-key sub-client.
type APIKeyClient struct {
	c *Client
}

func newAPIKeyClient(c *Client) *APIKeyClient {
	return &APIKeyClient{c: c}
}

type apiKeyRow struct {
	SubUID    string   `json:"subUid"`
	Label     string   `json:"label"`
	APIKey    string   `json:"apiKey"`
	SecretKey string   `json:"secretKey"`
	PermType  string   `json:"permType"`
	PermList  []string `json:"permList"`
	IPList    []string `json:"ipList"`
}

func (r apiKeyRow) toDomain() brokertypes.SubAccountAPIKey {
	return brokertypes.SubAccountAPIKey{
		SubUID:    r.SubUID,
		Label:     r.Label,
		APIKey:    r.APIKey,
		SecretKey: r.SecretKey,
		PermType:  r.PermType,
		PermList:  r.PermList,
		IPList:    r.IPList,
	}
}

// ---------------------------------------------------------------------
// Create — broker/manage/create-subaccount-apikey.
// ---------------------------------------------------------------------

type createAPIKeyBody struct {
	SubUID     string   `json:"subUid"`
	Passphrase string   `json:"passphrase"`
	Label      string   `json:"label,omitempty"`
	IPList     []string `json:"ipList"`
	PermType   string   `json:"permType"`
	PermList   []string `json:"permList"`
}

// Create mints a new API key for the sub-account. SubUID, Passphrase,
// IPList, PermType and PermList are required. The returned SecretKey is
// only available here — store it immediately. NOTE: mints real credentials.
func (a *APIKeyClient) Create(ctx context.Context, req brokertypes.CreateAPIKeyRequest) (brokertypes.SubAccountAPIKey, error) {
	var out brokertypes.SubAccountAPIKey
	switch {
	case req.SubUID == "":
		return out, errInvalid("APIKeys.Create", "subUid is required")
	case req.Passphrase == "":
		return out, errInvalid("APIKeys.Create", "passphrase is required")
	case len(req.IPList) == 0:
		return out, errInvalid("APIKeys.Create", "ipList is required")
	case req.PermType == "":
		return out, errInvalid("APIKeys.Create", "permType is required")
	case len(req.PermList) == 0:
		return out, errInvalid("APIKeys.Create", "permList is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/manage/create-subaccount-apikey",
		Body: createAPIKeyBody{
			SubUID:     req.SubUID,
			Passphrase: req.Passphrase,
			Label:      req.Label,
			IPList:     req.IPList,
			PermType:   req.PermType,
			PermList:   req.PermList,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row apiKeyRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("APIKeys.Create", err)
	}
	return row.toDomain(), nil
}

// ---------------------------------------------------------------------
// List — broker/manage/subaccount-apikey-list.
// ---------------------------------------------------------------------

// List returns the sub-account's API keys (SecretKey is not included).
// subUID is required.
func (a *APIKeyClient) List(ctx context.Context, subUID string) ([]brokertypes.SubAccountAPIKey, error) {
	if subUID == "" {
		return nil, errInvalid("APIKeys.List", "subUid is required")
	}

	var query url.Values = url.Values{}
	query.Set("subUid", subUID)

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/broker/manage/subaccount-apikey-list",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []apiKeyRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("APIKeys.List", err)
	}
	var out []brokertypes.SubAccountAPIKey = make([]brokertypes.SubAccountAPIKey, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		out = append(out, rows[i].toDomain())
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Modify — broker/manage/modify-subaccount-apikey.
// ---------------------------------------------------------------------

type modifyAPIKeyBody struct {
	SubUID     string   `json:"subUid"`
	APIKey     string   `json:"apiKey"`
	Label      string   `json:"label,omitempty"`
	Passphrase string   `json:"passphrase"`
	IPList     []string `json:"ipList,omitempty"`
	PermType   string   `json:"permType,omitempty"`
	PermList   []string `json:"permList"`
}

// Modify updates an existing sub-account API key's permissions / IPs /
// label. SubUID, APIKey, Passphrase and PermList are required.
func (a *APIKeyClient) Modify(ctx context.Context, req brokertypes.ModifyAPIKeyRequest) (brokertypes.SubAccountAPIKey, error) {
	var out brokertypes.SubAccountAPIKey
	switch {
	case req.SubUID == "":
		return out, errInvalid("APIKeys.Modify", "subUid is required")
	case req.APIKey == "":
		return out, errInvalid("APIKeys.Modify", "apiKey is required")
	case req.Passphrase == "":
		return out, errInvalid("APIKeys.Modify", "passphrase is required")
	case len(req.PermList) == 0:
		return out, errInvalid("APIKeys.Modify", "permList is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/broker/manage/modify-subaccount-apikey",
		Body: modifyAPIKeyBody{
			SubUID:     req.SubUID,
			APIKey:     req.APIKey,
			Label:      req.Label,
			Passphrase: req.Passphrase,
			IPList:     req.IPList,
			PermType:   req.PermType,
			PermList:   req.PermList,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row apiKeyRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("APIKeys.Modify", err)
	}
	return row.toDomain(), nil
}
