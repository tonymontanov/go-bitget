/*
FILE: broker/subaccounts_contract_test.go

DESCRIPTION:
Contract tests for the broker SubAccounts sub-client: response parsing,
client-side guards, request-body shape for the mutating calls, the
hasNextPage/idLessThan list cursor and the idLessThan/endId record cursors.
*/

package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	brokertypes "github.com/tonymontanov/go-bitget/v2/broker/types"
)

func TestContract_SubAccounts_GetInfo(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/account/info": `{"code":"00000","msg":"success","data":{"subAccountSize":"3","maxSubAccountSize":"20","uTime":"1700000000000"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var info, err = bc.SubAccounts().GetInfo(context.Background())
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if info.SubAccountSize != 3 || info.MaxSubAccountSize != 20 || info.UTimeMs != 1700000000000 {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestContract_SubAccounts_Create(t *testing.T) {
	t.Parallel()
	var gotName, gotLabel string
	var routes = map[string]string{
		"/api/v2/broker/account/create-subaccount": `{"code":"00000","msg":"success","data":{"subUid":"123","subaccountName":"sub01","status":"normal","permList":["read","spot_trade"],"label":"algo","cTime":"1700000000000"}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/broker/account/create-subaccount" {
			if r.Method != http.MethodPost {
				t.Errorf("want POST, got %s", r.Method)
			}
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			gotName, _ = m["subaccountName"].(string)
			gotLabel, _ = m["label"].(string)
		}
	})
	var bc = brokerClient(t, client)

	var acc, err = bc.SubAccounts().Create(context.Background(), "sub01", "algo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotName != "sub01" || gotLabel != "algo" {
		t.Fatalf("body mismatch: name=%q label=%q", gotName, gotLabel)
	}
	if acc.SubUID != "123" || acc.Status != "normal" || len(acc.PermList) != 2 || acc.CTimeMs != 1700000000000 {
		t.Fatalf("unexpected acc: %+v", acc)
	}

	// Guard: empty name.
	if _, err = bc.SubAccounts().Create(context.Background(), "", "x"); err == nil {
		t.Error("Create(\"\"): want guard error")
	}
}

func TestContract_SubAccounts_List_Paginates(t *testing.T) {
	t.Parallel()
	// Page 1: hasNextPage true; page 2: hasNextPage false.
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/account/subaccount-list" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		var idLessThan = r.URL.Query().Get("idLessThan")
		if idLessThan == "" {
			return `{"code":"00000","msg":"success","data":{"hasNextPage":true,"idLessThan":200,"subList":[{"subUid":"300","subaccountName":"a","status":"normal","permList":["read"],"cTime":"1","uTime":"2"},{"subUid":"200","subaccountName":"b","status":"freeze","permList":["read"],"cTime":"1","uTime":"2"}]}}`
		}
		return `{"code":"00000","msg":"success","data":{"hasNextPage":false,"idLessThan":0,"subList":[{"subUid":"100","subaccountName":"c","status":"normal","permList":["read"],"cTime":"1","uTime":"2"}]}}`
	})
	var bc = brokerClient(t, client)

	var subs, err = bc.SubAccounts().List(context.Background(), SubAccountsQuery{Status: "normal"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("want 3 stitched rows, got %d", len(subs))
	}
	if subs[0].SubUID != "300" || subs[2].SubUID != "100" {
		t.Fatalf("unexpected order: %+v", subs)
	}
}

func TestContract_SubAccounts_Modify(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	var routes = map[string]string{
		"/api/v2/broker/account/modify-subaccount": `{"code":"00000","msg":"success","data":{"subUid":"123","subaccountName":"sub01","status":"freeze","permList":["read"],"language":"en_US","cTime":"1","uTime":"2"}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/broker/account/modify-subaccount" {
			_ = json.Unmarshal(body, &gotBody)
		}
	})
	var bc = brokerClient(t, client)

	var acc, err = bc.SubAccounts().Modify(context.Background(), brokertypes.ModifySubAccountRequest{
		SubUID: "123", PermList: []string{"read"}, Status: "freeze", Language: "en_US",
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if acc.Status != "freeze" || acc.Language != "en_US" {
		t.Fatalf("unexpected acc: %+v", acc)
	}
	if gotBody["subUid"] != "123" || gotBody["status"] != "freeze" {
		t.Fatalf("body mismatch: %+v", gotBody)
	}
	if _, ok := gotBody["permList"].([]any); !ok {
		t.Fatalf("permList must be a JSON array: %+v", gotBody["permList"])
	}

	// Guard: missing status.
	var _, err2 = bc.SubAccounts().Modify(context.Background(), brokertypes.ModifySubAccountRequest{
		SubUID: "123", PermList: []string{"read"},
	})
	if err2 == nil {
		t.Error("Modify(no status): want guard error")
	}
}

func TestContract_SubAccounts_Assets(t *testing.T) {
	t.Parallel()
	var sawProductType bool
	var routes = map[string]string{
		"/api/v2/broker/account/subaccount-spot-assets":   `{"code":"00000","msg":"success","data":{"assetsList":[{"coin":"USDT","available":"100.5","frozen":"1.0","locked":"0.5","uTime":"1700000000000"}]}}`,
		"/api/v2/broker/account/subaccount-future-assets": `{"code":"00000","msg":"success","data":{"assetsList":[{"marginCoin":"USDT","available":"50","frozen":"0","locked":"0","crossedMaxAvailable":"50","isolatedMaxAvailable":"50","maxTransferOut":"50","accountEquity":"50","usdtEquity":"50","btcEquity":"0.001","uTime":"1700000000000"}]}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		// Spot assets are account-level: no productType must be sent.
		if r.URL.Path == "/api/v2/broker/account/subaccount-spot-assets" {
			if r.URL.Query().Get("productType") != "" {
				sawProductType = true
			}
		}
	})
	var bc = brokerClient(t, client)

	var spot, err = bc.SubAccounts().GetSpotAssets(context.Background(), "123", "USDT", "all")
	if err != nil {
		t.Fatalf("GetSpotAssets: %v", err)
	}
	if len(spot) != 1 || !spot[0].Available.Equal(dec("100.5")) {
		t.Fatalf("unexpected spot: %+v", spot)
	}
	if sawProductType {
		t.Error("spot-assets must not send productType")
	}

	var fut, ferr = bc.SubAccounts().GetFuturesAssets(context.Background(), "123", "USDT-FUTURES")
	if ferr != nil {
		t.Fatalf("GetFuturesAssets: %v", ferr)
	}
	if len(fut) != 1 || !fut[0].BTCEquity.Equal(dec("0.001")) || !fut[0].AccountEquity.Equal(dec("50")) {
		t.Fatalf("unexpected fut: %+v", fut)
	}

	// Guard: futures requires productType.
	if _, err = bc.SubAccounts().GetFuturesAssets(context.Background(), "123", ""); err == nil {
		t.Error("GetFuturesAssets(no productType): want guard error")
	}
}

func TestContract_SubAccounts_Withdraw_Guards(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/account/subaccount-withdrawal": `{"code":"00000","msg":"success","data":{"orderId":"o1","clientOid":"c1"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	// on_chain without chain → guard.
	var _, err = bc.SubAccounts().Withdraw(context.Background(), brokertypes.SubWithdrawalRequest{
		SubUID: "123", Coin: "USDT", Dest: "on_chain", Address: "0xabc", Amount: "10",
	})
	if err == nil {
		t.Error("Withdraw(on_chain no chain): want guard error")
	}

	// valid internal_transfer.
	var res, err2 = bc.SubAccounts().Withdraw(context.Background(), brokertypes.SubWithdrawalRequest{
		SubUID: "123", Coin: "USDT", Dest: "internal_transfer", Address: "987654321", Amount: "10",
	})
	if err2 != nil {
		t.Fatalf("Withdraw: %v", err2)
	}
	if res.OrderID != "o1" || res.ClientOid != "c1" {
		t.Fatalf("unexpected res: %+v", res)
	}
}

func TestContract_SubAccounts_Records_Cursor(t *testing.T) {
	t.Parallel()
	// Deposit records: 1 full page (limit reached) then a short page.
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		var id = r.URL.Query().Get("idLessThan")
		if r.URL.Path == "/api/v2/broker/subaccount-deposit" {
			if id == "" {
				// First page: emit exactly `limit` rows so the cursor advances.
				var limit = r.URL.Query().Get("limit")
				return depositPage(limit, "row", "50")
			}
			return `{"code":"00000","msg":"success","data":{"resultList":[{"orderId":"d-last","coin":"BTC","amount":"0.5","fee":"0.0001","status":"success","cTime":"1","uTime":"2"}],"endId":""}}`
		}
		return `{"code":"40404","msg":"nf","data":null}`
	})
	var bc = brokerClient(t, client)

	var recs, err = bc.SubAccounts().GetDepositRecords(context.Background(), SubRecordsQuery{})
	if err != nil {
		t.Fatalf("GetDepositRecords: %v", err)
	}
	if len(recs) == 0 || recs[len(recs)-1].OrderID != "d-last" {
		t.Fatalf("expected stitched pages ending with d-last, got %d rows", len(recs))
	}
	if !recs[len(recs)-1].Amount.Equal(dec("0.5")) {
		t.Fatalf("amount parse mismatch: %+v", recs[len(recs)-1])
	}
}

func TestContract_SubAccounts_GetAllRecords(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/all-sub-deposit-withdrawal": `{"code":"00000","msg":"success","data":{"list":[{"uid":"1","txId":"tx","type":"deposit","subType":"onchain","coin":"USDT","amount":"10","status":"success","ts":"1700000000000"}],"endId":""}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var recs, err = bc.SubAccounts().GetAllRecords(context.Background(), AllSubRecordsQuery{Type: "all"})
	if err != nil {
		t.Fatalf("GetAllRecords: %v", err)
	}
	if len(recs) != 1 || recs[0].Type != "deposit" || !recs[0].Amount.Equal(dec("10")) {
		t.Fatalf("unexpected recs: %+v", recs)
	}
}

// --- small fixture builders (keep the table literals readable) ----------

// depositPage emits exactly one row whose count equals the requested
// limit so PaginateByCursor advances to the next (short) page.
func depositPage(limit, orderPrefix, endID string) string {
	var n int
	switch limit {
	case "":
		n = 100
	default:
		n = atoiOr(limit, 100)
	}
	var b strings.Builder
	b.WriteString(`{"code":"00000","msg":"success","data":{"resultList":[`)
	var i int
	for i = 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"orderId":"`)
		b.WriteString(orderPrefix)
		b.WriteString(`","coin":"BTC","amount":"0.1","fee":"0","status":"success","cTime":"1","uTime":"2"}`)
	}
	b.WriteString(`],"endId":"`)
	b.WriteString(endID)
	b.WriteString(`"}}`)
	return b.String()
}
