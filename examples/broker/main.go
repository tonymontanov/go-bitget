/*
FILE: examples/broker/main.go

DESCRIPTION:
Read-only demo of the BROKER / AGENT profile. It prints the broker
sub-account quota and list, the institutional broker reporting (total
commission, rebate info, subaccounts), the agent / affiliate reporting
(customer commissions, sub-customer list, KYC), and the copy-trading
broker trader list.

The example is deliberately READ-ONLY: it never creates sub-accounts,
withdraws, mints API keys, or changes any account state. Write endpoints
(Create / Withdraw / SetAutoTransfer / APIKeys.Create|Modify / ...) move
funds or credentials and are out of scope for a demo.

These endpoints require the account to be an approved Bitget broker /
agent — a non-eligible key will get a 4xx, which the demo logs as an
expected note rather than failing.

The blank import of the broker package registers its lazy factory so
bitget.Client.Broker() resolves.

USAGE (env-vars are mandatory):

	export BITGET_API_KEY=...
	export BITGET_SECRET_KEY=...
	export BITGET_PASSPHRASE=...
	go run ./examples/broker

Section-specific BITGET_BROKER_* variables take precedence over the
generic BITGET_* triple when set.
*/

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/broker"
)

func main() {
	var apiKey, secretKey, passphrase string = resolveCreds()
	if apiKey == "" || secretKey == "" || passphrase == "" {
		log.Fatal("BITGET_BROKER_API_KEY / BITGET_BROKER_SECRET_KEY / BITGET_BROKER_PASSPHRASE " +
			"(or the generic BITGET_* triple) env-vars are required")
	}

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = apiKey
	cfg.SecretKey = secretKey
	cfg.Passphrase = passphrase

	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		log.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	var bk *broker.Client = c.Broker().(*broker.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	subAccountSection(ctx, bk)
	statsSection(ctx, bk)
	agentSection(ctx, bk)
	copyBrokerSection(ctx, bk)
}

func subAccountSection(ctx context.Context, bk *broker.Client) {
	fmt.Println("== BROKER / SUB-ACCOUNTS ==")

	if info, err := bk.SubAccounts().GetInfo(ctx); err != nil {
		logCallError("Broker.SubAccounts.GetInfo", err)
	} else {
		fmt.Printf("sub-account quota: %d / %d\n", info.SubAccountSize, info.MaxSubAccountSize)
	}

	if subs, err := bk.SubAccounts().List(ctx, broker.SubAccountsQuery{}); err != nil {
		logCallError("Broker.SubAccounts.List", err)
	} else {
		fmt.Printf("sub-accounts: %d\n", len(subs))
		for i := 0; i < len(subs) && i < 3; i++ {
			fmt.Printf("  %s name=%s status=%s\n", subs[i].SubUID, subs[i].SubaccountName, subs[i].Status)
		}
	}
}

func statsSection(ctx context.Context, bk *broker.Client) {
	fmt.Println("== BROKER / REPORTING ==")

	if tc, err := bk.Stats().GetTotalCommission(ctx, broker.BrokerReportQuery{}); err != nil {
		logCallError("Broker.Stats.GetTotalCommission", err)
	} else {
		fmt.Printf("total-commission: %d daily row(s)\n", len(tc))
		if len(tc) > 0 {
			fmt.Printf("  %s totalCommission=%s activeTraders=%d\n",
				tc[0].Date, tc[0].TotalCommission, tc[0].TotalActiveTraders)
		}
	}

	if subs, err := bk.Stats().GetSubaccounts(ctx, broker.BrokerReportQuery{}); err != nil {
		logCallError("Broker.Stats.GetSubaccounts", err)
	} else {
		fmt.Printf("broker subaccounts (reporting): %d\n", len(subs))
	}
}

func agentSection(ctx context.Context, bk *broker.Client) {
	fmt.Println("== AGENT / AFFILIATE ==")

	if rows, err := bk.Agent().GetSubCustomerList(ctx, broker.AgentQuery{}); err != nil {
		logCallError("Broker.Agent.GetSubCustomerList", err)
	} else {
		fmt.Printf("sub-customers: %d\n", len(rows))
	}

	if rows, err := bk.Agent().GetCustomerCommissions(ctx, broker.AgentCustomerCommissionsQuery{}); err != nil {
		logCallError("Broker.Agent.GetCustomerCommissions", err)
	} else {
		fmt.Printf("customer commissions: %d row(s)\n", len(rows))
	}

	if rows, err := bk.Agent().GetCustomerKycResult(ctx, broker.AgentQuery{}); err != nil {
		logCallError("Broker.Agent.GetCustomerKycResult", err)
	} else {
		fmt.Printf("customer KYC: %d row(s)\n", len(rows))
	}
}

func copyBrokerSection(ctx context.Context, bk *broker.Client) {
	fmt.Println("== COPY-TRADING BROKER ==")

	if traders, err := bk.CopyBroker().GetTraders(ctx, broker.BrokerReportQuery{}); err != nil {
		logCallError("Broker.CopyBroker.GetTraders", err)
	} else {
		fmt.Printf("copy traders: %d\n", len(traders))
		for i := 0; i < len(traders) && i < 3; i++ {
			fmt.Printf("  %s (%s) followers=%d\n",
				traders[i].TraderID, traders[i].TraderName, traders[i].FollowCount)
		}
	}

	if traces, err := bk.CopyBroker().GetPendingOrders(ctx, broker.BrokerReportQuery{}); err != nil {
		logCallError("Broker.CopyBroker.GetPendingOrders", err)
	} else {
		fmt.Printf("copy pending order traces: %d\n", len(traces))
	}
}

// logCallError classifies a read failure with the usual SDK predicates.
func logCallError(op string, err error) {
	switch {
	case bitget.IsAuth(err):
		log.Printf("%s: auth failed (check key/secret/passphrase + IP whitelist + broker/agent permissions): %v", op, err)
	case bitget.IsRateLimit(err):
		log.Printf("%s: rate-limited — back off and retry: %v", op, err)
	default:
		log.Printf("%s: %v", op, err)
	}
}

// resolveCreds reads the section-specific BITGET_BROKER_* credentials,
// falling back to the generic BITGET_* triple when they are unset.
func resolveCreds() (apiKey, secretKey, passphrase string) {
	apiKey = firstNonEmpty(os.Getenv("BITGET_BROKER_API_KEY"), os.Getenv("BITGET_API_KEY"))
	secretKey = firstNonEmpty(os.Getenv("BITGET_BROKER_SECRET_KEY"), os.Getenv("BITGET_SECRET_KEY"))
	passphrase = firstNonEmpty(os.Getenv("BITGET_BROKER_PASSPHRASE"), os.Getenv("BITGET_PASSPHRASE"))
	return apiKey, secretKey, passphrase
}

// firstNonEmpty returns the first non-empty string of its arguments, or
// "" when all are empty.
func firstNonEmpty(values ...string) string {
	var i int
	for i = 0; i < len(values); i++ {
		if values[i] != "" {
			return values[i]
		}
	}
	return ""
}
