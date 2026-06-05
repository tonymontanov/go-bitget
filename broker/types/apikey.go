/*
FILE: broker/types/apikey.go

DESCRIPTION:
Domain types for the BROKER sub-account API-key lifecycle
(/api/v2/broker/manage/...). Field mapping verified against the Bitget V2
broker docs and the tiagosiebler reference types
(CreateSubaccountApiKeyResponseV2 / SubaccountApiKeyV2 /
ModifySubaccountApiKeyResponseV2 + the create/modify request shapes).
*/

package types

// SubAccountAPIKey — a sub-account API key. SecretKey is only populated on
// create (the venue never returns it again); list/modify leave it empty.
//
//	PermType: read_only | read_write (venue-defined)
//	PermList: spot_trade | contract_trade | ... (venue-defined)
type SubAccountAPIKey struct {
	SubUID    string
	Label     string
	APIKey    string
	SecretKey string
	PermType  string
	PermList  []string
	IPList    []string
}

// CreateAPIKeyRequest — POST broker/manage/create-subaccount-apikey.
// SubUID, Passphrase, IPList, PermType and PermList are required.
type CreateAPIKeyRequest struct {
	SubUID     string
	Passphrase string
	Label      string
	IPList     []string
	PermType   string
	PermList   []string
}

// ModifyAPIKeyRequest — POST broker/manage/modify-subaccount-apikey.
// SubUID, APIKey, Passphrase and PermList are required; the rest are
// optional overrides.
type ModifyAPIKeyRequest struct {
	SubUID     string
	APIKey     string
	Label      string
	Passphrase string
	IPList     []string
	PermType   string
	PermList   []string
}
