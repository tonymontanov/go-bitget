/*
FILE: broker/client_test.go

DESCRIPTION:
Smoke test for the BROKER scaffold: the root factory resolves (the
package's init() registered it), the category sub-clients are wired, and
NewClient guards a nil parent.
*/

package broker

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

func TestBroker_FactoryResolves(t *testing.T) {
	t.Parallel()

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"

	var client *bitget.Client
	var err error
	client, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var v any = client.Broker()
	var bc, ok = v.(*Client)
	if !ok || bc == nil {
		t.Fatalf("Broker(): want *broker.Client, got %T", v)
	}
	if bc.Parent() != client {
		t.Error("Parent() mismatch")
	}
	if bc.SubAccounts() == nil || bc.APIKeys() == nil || bc.Stats() == nil || bc.Agent() == nil || bc.CopyBroker() == nil {
		t.Error("sub-clients must be wired")
	}
}

func TestBroker_NewClientNilParent(t *testing.T) {
	t.Parallel()
	if NewClient(nil) != nil {
		t.Error("NewClient(nil): want nil")
	}
}
