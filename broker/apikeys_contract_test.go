/*
FILE: broker/apikeys_contract_test.go

DESCRIPTION:
Contract tests for the broker APIKeys sub-client: create returns the
secretKey, list omits it, modify echoes the updated perms, request bodies
carry JSON-array ipList/permList, and the required-field guards fire.
*/

package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	brokertypes "github.com/tonymontanov/go-bitget/v2/broker/types"
)

func TestContract_APIKeys_Create(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	var routes = map[string]string{
		"/api/v2/broker/manage/create-subaccount-apikey": `{"code":"00000","msg":"success","data":{"subUid":"123","apiKey":"ak","secretKey":"sk","label":"algo","ipList":["1.2.3.4"],"permType":"read_write","permList":["spot_trade"]}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/broker/manage/create-subaccount-apikey" {
			if r.Method != http.MethodPost {
				t.Errorf("want POST, got %s", r.Method)
			}
			_ = json.Unmarshal(body, &gotBody)
		}
	})
	var bc = brokerClient(t, client)

	var key, err = bc.APIKeys().Create(context.Background(), brokertypes.CreateAPIKeyRequest{
		SubUID:     "123",
		Passphrase: "pass",
		Label:      "algo",
		IPList:     []string{"1.2.3.4"},
		PermType:   "read_write",
		PermList:   []string{"spot_trade"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key.APIKey != "ak" || key.SecretKey != "sk" {
		t.Fatalf("want apiKey/secretKey populated, got %+v", key)
	}
	if _, ok := gotBody["ipList"].([]any); !ok {
		t.Fatalf("ipList must be a JSON array: %+v", gotBody["ipList"])
	}
	if _, ok := gotBody["permList"].([]any); !ok {
		t.Fatalf("permList must be a JSON array: %+v", gotBody["permList"])
	}

	// Guards.
	if _, err = bc.APIKeys().Create(context.Background(), brokertypes.CreateAPIKeyRequest{Passphrase: "p", IPList: []string{"x"}, PermType: "t", PermList: []string{"p"}}); err == nil {
		t.Error("Create(no subUid): want guard error")
	}
	if _, err = bc.APIKeys().Create(context.Background(), brokertypes.CreateAPIKeyRequest{SubUID: "1", Passphrase: "p", PermType: "t", PermList: []string{"p"}}); err == nil {
		t.Error("Create(no ipList): want guard error")
	}
}

func TestContract_APIKeys_List(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/manage/subaccount-apikey-list": `{"code":"00000","msg":"success","data":[{"subUid":"123","label":"a","apiKey":"ak1","permType":"read_only","permList":["read"],"ipList":["1.1.1.1"]},{"subUid":"123","label":"b","apiKey":"ak2","permType":"read_write","permList":["spot_trade"],"ipList":[]}]}`,
	}
	var sawSubUID string
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/broker/manage/subaccount-apikey-list" {
			sawSubUID = r.URL.Query().Get("subUid")
		}
	})
	var bc = brokerClient(t, client)

	var keys, err = bc.APIKeys().List(context.Background(), "123")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if sawSubUID != "123" {
		t.Errorf("subUid query mismatch: %q", sawSubUID)
	}
	if len(keys) != 2 || keys[0].APIKey != "ak1" || keys[1].PermType != "read_write" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
	if keys[0].SecretKey != "" {
		t.Error("list must not include secretKey")
	}

	if _, err = bc.APIKeys().List(context.Background(), ""); err == nil {
		t.Error("List(\"\"): want guard error")
	}
}

func TestContract_APIKeys_Modify(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/manage/modify-subaccount-apikey": `{"code":"00000","msg":"success","data":{"subUid":"123","apiKey":"ak","label":"new","ipList":["2.2.2.2"],"permType":"read_write","permList":["spot_trade","contract_trade"]}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var key, err = bc.APIKeys().Modify(context.Background(), brokertypes.ModifyAPIKeyRequest{
		SubUID:     "123",
		APIKey:     "ak",
		Passphrase: "pass",
		PermList:   []string{"spot_trade", "contract_trade"},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if len(key.PermList) != 2 || key.Label != "new" {
		t.Fatalf("unexpected key: %+v", key)
	}

	// Guard: missing apiKey.
	if _, err = bc.APIKeys().Modify(context.Background(), brokertypes.ModifyAPIKeyRequest{SubUID: "1", Passphrase: "p", PermList: []string{"x"}}); err == nil {
		t.Error("Modify(no apiKey): want guard error")
	}
}
