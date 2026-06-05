/*
FILE: common/users_contract_test.go

DESCRIPTION:
Contract tests for the common Users sub-client (virtual sub-accounts +
API keys): POST body shaping, response decoding (incl. the venue's
"subaAccount*" spelling), the idLessThan/endId cursor on the list read,
and the client-side guards on the write endpoints.
*/

package common

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestContract_Users_CreateSubAccounts(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	var routes = map[string]string{
		// note the venue's "suba…" spelling in the success rows.
		"/api/v2/user/create-virtual-subaccount": `{"code":"00000","msg":"success","data":{"failureList":[{"subaAccountName":"bad"}],"successList":[{"subaAccountUid":"u1","subaAccountName":"ok","status":"normal","label":"L","permList":["spot_trade"],"cTime":"1700000000000","uTime":"1700000000001"}]}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/user/create-virtual-subaccount" {
			_ = json.Unmarshal(body, &gotBody)
		}
	})
	var cc = commonClient(t, client)

	var res, err = cc.Users().CreateSubAccounts(context.Background(), []string{"ok", "bad"})
	if err != nil {
		t.Fatalf("CreateSubAccounts: %v", err)
	}
	if _, ok := gotBody["subAccountList"]; !ok {
		t.Errorf("body must carry subAccountList, got %v", gotBody)
	}
	if len(res.SuccessList) != 1 || res.SuccessList[0].SubAccountUID != "u1" || res.SuccessList[0].SubAccountName != "ok" {
		t.Fatalf("unexpected success: %+v", res.SuccessList)
	}
	if res.SuccessList[0].CTimeMs != 1700000000000 {
		t.Fatalf("cTime mismatch: %+v", res.SuccessList[0])
	}
	if len(res.FailureList) != 1 || res.FailureList[0] != "bad" {
		t.Fatalf("unexpected failure: %+v", res.FailureList)
	}

	if _, err = cc.Users().CreateSubAccounts(context.Background(), nil); err == nil {
		t.Error("CreateSubAccounts(nil): want guard error")
	}
}

func TestContract_Users_ModifyAndList(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/user/modify-virtual-subaccount": `{"code":"00000","msg":"success","data":{"result":"success"}}`,
		"/api/v2/user/virtual-subaccount-list":   `{"code":"00000","msg":"success","data":{"endId":"","subAccountList":[{"subAccountUid":"u1","subAccountName":"alice","status":"normal","permList":["spot_trade"],"label":"L","accountType":"1","bindingTime":"1700000000000","cTime":"1700000000001","uTime":"1700000000002"}]}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)
	var ctx = context.Background()

	var res, err = cc.Users().ModifySubAccount(ctx, ModifySubAccountRequest{SubAccountUID: "u1", Status: "normal", PermList: []string{"spot_trade"}})
	if err != nil || res != "success" {
		t.Fatalf("ModifySubAccount: %v %q", err, res)
	}

	var subs, lerr = cc.Users().GetSubAccounts(ctx, "normal")
	if lerr != nil {
		t.Fatalf("GetSubAccounts: %v", lerr)
	}
	if len(subs) != 1 || subs[0].SubAccountUID != "u1" || subs[0].BindingTimeMs != 1700000000000 {
		t.Fatalf("unexpected subs: %+v", subs)
	}

	// Guards.
	if _, err = cc.Users().ModifySubAccount(ctx, ModifySubAccountRequest{Status: "normal"}); err == nil {
		t.Error("ModifySubAccount(no uid): want guard")
	}
	if _, err = cc.Users().ModifySubAccount(ctx, ModifySubAccountRequest{SubAccountUID: "u1"}); err == nil {
		t.Error("ModifySubAccount(no status): want guard")
	}
}

func TestContract_Users_APIKeys(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/user/batch-create-subaccount-and-apikey": `{"code":"00000","msg":"success","data":[{"subAccountUid":"u1","subAccountName":"alice","label":"L","subAccountApiKey":"ak","secretKey":"sk","permList":["spot_trade"],"ipList":["1.2.3.4"]}]}`,
		"/api/v2/user/create-virtual-subaccount-apikey":   `{"code":"00000","msg":"success","data":{"subAccountUid":"u1","label":"L","subAccountApiKey":"ak2","secretKey":"sk2","permList":["spot_trade"],"ipList":[]}}`,
		"/api/v2/user/modify-virtual-subaccount-apikey":   `{"code":"00000","msg":"success","data":{"subAccountUid":"u1","label":"L2","subAccountApiKey":"ak2","secretKey":"sk2","permList":["read"],"ipList":[]}}`,
		"/api/v2/user/virtual-subaccount-apikey-list":     `{"code":"00000","msg":"success","data":[{"subAccountUid":"u1","label":"L","subAccountApiKey":"ak","permList":["spot_trade"],"ipList":["1.2.3.4"]}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)
	var ctx = context.Background()

	var batch, berr = cc.Users().BatchCreateSubAccountAndAPIKey(ctx, CreateSubAndKeyRequest{SubAccountName: "alice", Passphrase: "pass1234"})
	if berr != nil || len(batch) != 1 || batch[0].APIKey != "ak" || batch[0].SecretKey != "sk" {
		t.Fatalf("batch create: %v %+v", berr, batch)
	}

	var created, cerr = cc.Users().CreateAPIKey(ctx, CreateSubAPIKeyRequest{SubAccountUID: "u1", Passphrase: "pass1234"})
	if cerr != nil || created.APIKey != "ak2" || created.SecretKey != "sk2" {
		t.Fatalf("create key: %v %+v", cerr, created)
	}

	var modified, merr = cc.Users().ModifyAPIKey(ctx, ModifySubAPIKeyRequest{SubAccountUID: "u1", APIKey: "ak2", Passphrase: "pass1234", PermList: []string{"read"}})
	if merr != nil || modified.Label != "L2" || len(modified.PermList) != 1 || modified.PermList[0] != "read" {
		t.Fatalf("modify key: %v %+v", merr, modified)
	}

	var keys, kerr = cc.Users().GetAPIKeys(ctx, "u1")
	if kerr != nil || len(keys) != 1 || keys[0].APIKey != "ak" || len(keys[0].IPList) != 1 {
		t.Fatalf("list keys: %v %+v", kerr, keys)
	}

	// Guards.
	if _, err := cc.Users().BatchCreateSubAccountAndAPIKey(ctx, CreateSubAndKeyRequest{Passphrase: "x"}); err == nil {
		t.Error("batch(no name): want guard")
	}
	if _, err := cc.Users().CreateAPIKey(ctx, CreateSubAPIKeyRequest{SubAccountUID: "u1"}); err == nil {
		t.Error("createKey(no passphrase): want guard")
	}
	if _, err := cc.Users().ModifyAPIKey(ctx, ModifySubAPIKeyRequest{SubAccountUID: "u1", Passphrase: "p"}); err == nil {
		t.Error("modifyKey(no apiKey): want guard")
	}
	if _, err := cc.Users().GetAPIKeys(ctx, ""); err == nil {
		t.Error("getKeys(no uid): want guard")
	}
}
