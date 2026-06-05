/*
FILE: examples/common/main.go

DESCRIPTION:
Read-only demo of the COMMON / PUBLIC utility profile. It prints the
server time and announcements (UNSIGNED — work without credentials), the
account-wide assets (funding / bot / all-account balance) and trade-rate,
a recent slice of tax records, the P2P merchant profile / merchant list,
and the caller's virtual sub-accounts.

The example is deliberately READ-ONLY: it never creates / modifies virtual
sub-accounts or API keys (those mint real credentials and change account
state) — only the read endpoints are exercised.

The signed sections require valid credentials; tax / P2P / virtual
sub-account reads further require the relevant account features. A
non-eligible key gets a 4xx, which the demo logs as an expected note
rather than failing.

The blank import of the common package registers its lazy factory so
bitget.Client.Common() resolves.

USAGE:

	# public (unsigned) section works with no credentials:
	go run ./examples/common

	# signed sections need the usual triple:
	export BITGET_API_KEY=...
	export BITGET_SECRET_KEY=...
	export BITGET_PASSPHRASE=...
	go run ./examples/common
*/

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/common"
)

func main() {
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = os.Getenv("BITGET_API_KEY")
	cfg.SecretKey = os.Getenv("BITGET_SECRET_KEY")
	cfg.Passphrase = os.Getenv("BITGET_PASSPHRASE")

	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		log.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	var cm *common.Client = c.Common().(*common.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	publicSection(ctx, cm)

	var signed bool = cfg.APIKey != "" && cfg.SecretKey != "" && cfg.Passphrase != ""
	if !signed {
		fmt.Println("\n(no credentials set — skipping signed sections; set BITGET_API_KEY/SECRET_KEY/PASSPHRASE to see them)")
		return
	}

	accountSection(ctx, cm)
	taxSection(ctx, cm)
	p2pSection(ctx, cm)
	usersSection(ctx, cm)
}

func publicSection(ctx context.Context, cm *common.Client) {
	fmt.Println("== PUBLIC (unsigned) ==")

	if ms, err := cm.Public().GetServerTime(ctx); err != nil {
		logCallError("Common.Public.GetServerTime", err)
	} else {
		fmt.Printf("server time: %d (%s)\n", ms, time.UnixMilli(ms).UTC().Format(time.RFC3339))
	}

	if anns, err := cm.Public().GetAnnouncements(ctx, common.AnnouncementsQuery{Language: "en_US"}); err != nil {
		logCallError("Common.Public.GetAnnouncements", err)
	} else {
		fmt.Printf("announcements: %d\n", len(anns))
		for i := 0; i < len(anns) && i < 3; i++ {
			fmt.Printf("  %s — %s\n", anns[i].AnnID, anns[i].AnnTitle)
		}
	}
}

func accountSection(ctx context.Context, cm *common.Client) {
	fmt.Println("\n== ACCOUNT-WIDE ==")

	if bal, err := cm.Account().GetAllAccountBalance(ctx); err != nil {
		logCallError("Common.Account.GetAllAccountBalance", err)
	} else {
		fmt.Printf("account balances: %d\n", len(bal))
		for i := 0; i < len(bal); i++ {
			fmt.Printf("  %-16s %s USDT\n", bal[i].AccountType, bal[i].USDTBalance)
		}
	}

	if fa, err := cm.Account().GetFundingAssets(ctx, ""); err != nil {
		logCallError("Common.Account.GetFundingAssets", err)
	} else {
		fmt.Printf("funding assets: %d coin(s)\n", len(fa))
	}

	if tr, err := cm.Account().GetTradeRate(ctx, "BTCUSDT", "spot"); err != nil {
		logCallError("Common.Account.GetTradeRate", err)
	} else {
		fmt.Printf("trade rate BTCUSDT/spot: maker=%s taker=%s\n", tr.MakerFeeRate, tr.TakerFeeRate)
	}
}

func taxSection(ctx context.Context, cm *common.Client) {
	fmt.Println("\n== TAX (last 7d) ==")

	var end int64 = time.Now().UnixMilli()
	var start int64 = end - 7*24*60*60*1000

	if recs, err := cm.Tax().GetSpotRecords(ctx, common.TaxQuery{StartTimeMs: start, EndTimeMs: end}); err != nil {
		logCallError("Common.Tax.GetSpotRecords", err)
	} else {
		fmt.Printf("spot tax records: %d\n", len(recs))
	}
}

func p2pSection(ctx context.Context, cm *common.Client) {
	fmt.Println("\n== P2P ==")

	if info, err := cm.P2P().GetMerchantInfo(ctx); err != nil {
		logCallError("Common.P2P.GetMerchantInfo", err)
	} else {
		fmt.Printf("merchant: %s (id=%s) trades=%d\n", info.NickName, info.MerchantID, info.TotalTrades)
	}

	if ms, err := cm.P2P().GetMerchants(ctx, "yes"); err != nil {
		logCallError("Common.P2P.GetMerchants", err)
	} else {
		fmt.Printf("online merchants: %d\n", len(ms))
	}
}

func usersSection(ctx context.Context, cm *common.Client) {
	fmt.Println("\n== VIRTUAL SUB-ACCOUNTS (read-only) ==")

	if subs, err := cm.Users().GetSubAccounts(ctx, ""); err != nil {
		logCallError("Common.Users.GetSubAccounts", err)
	} else {
		fmt.Printf("virtual sub-accounts: %d\n", len(subs))
		for i := 0; i < len(subs) && i < 3; i++ {
			fmt.Printf("  %s name=%s status=%s\n", subs[i].SubAccountUID, subs[i].SubAccountName, subs[i].Status)
		}
	}
}

// logCallError classifies a read failure with the usual SDK predicates.
func logCallError(op string, err error) {
	switch {
	case bitget.IsAuth(err):
		log.Printf("%s: auth failed (check key/secret/passphrase + IP whitelist + account features): %v", op, err)
	case bitget.IsRateLimit(err):
		log.Printf("%s: rate-limited — back off and retry: %v", op, err)
	default:
		log.Printf("%s: %v", op, err)
	}
}
