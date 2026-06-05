/*
FILE: uta/client_test.go

DESCRIPTION:
Smoke test for the UTA scaffold: the root factory resolves (the package's
init() registered it), the category sub-clients are wired, and NewClient
guards a nil parent.
*/

package uta

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

func TestUTA_FactoryResolves(t *testing.T) {
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

	var v any = client.UTA()
	var uc, ok = v.(*Client)
	if !ok || uc == nil {
		t.Fatalf("UTA(): want *uta.Client, got %T", v)
	}
	if uc.Parent() != client {
		t.Error("Parent() mismatch")
	}
	if uc.Public() == nil || uc.Account() == nil || uc.Trade() == nil || uc.Position() == nil || uc.Strategy() == nil {
		t.Error("sub-clients must be wired")
	}
}

func TestUTA_NewClientNilParent(t *testing.T) {
	t.Parallel()
	if NewClient(nil) != nil {
		t.Error("NewClient(nil): want nil")
	}
}
