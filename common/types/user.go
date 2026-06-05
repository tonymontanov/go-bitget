/*
FILE: common/types/user.go

DESCRIPTION:
Domain types for COMMON virtual sub-account + API-key management
(/api/v2/user/*). These are MAIN-account user management (virtual
sub-accounts created under the caller), distinct from the broker
sub-accounts in the broker/ package. Field mapping verified against the
Bitget V2 docs and the tiagosiebler reference types (VirtualSubAccountV2 /
CreateVirtualSubAccount(AndApiKey)V2 / SubAccountApiKeyItemV2).
*/

package types

// VirtualSubAccount — one row of GET user/virtual-subaccount-list.
type VirtualSubAccount struct {
	SubAccountUID  string
	SubAccountName string
	Status         string
	Label          string
	AccountType    string
	PermList       []string
	BindingTimeMs  int64
	CTimeMs        int64
	UTimeMs        int64
}

// VirtualSubCreated — one successfully created virtual sub-account.
type VirtualSubCreated struct {
	SubAccountUID  string
	SubAccountName string
	Status         string
	Label          string
	PermList       []string
	CTimeMs        int64
	UTimeMs        int64
}

// CreateVirtualSubResult — result of POST user/create-virtual-subaccount:
// the created accounts plus any names that failed.
type CreateVirtualSubResult struct {
	SuccessList []VirtualSubCreated
	FailureList []string // failed sub-account names
}

// VirtualSubAPIKey — a freshly minted / modified virtual sub-account API
// key. SecretKey is only returned at create / modify time — store it.
type VirtualSubAPIKey struct {
	SubAccountUID  string
	SubAccountName string
	Label          string
	APIKey         string
	SecretKey      string
	PermList       []string
	IPList         []string
}

// VirtualSubAPIKeyItem — one row of GET user/virtual-subaccount-apikey-list
// (no SecretKey).
type VirtualSubAPIKeyItem struct {
	SubAccountUID string
	Label         string
	APIKey        string
	PermList      []string
	IPList        []string
}
