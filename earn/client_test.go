/*
FILE: earn/client_test.go

DESCRIPTION:
Smoke test for the EARN scaffold: the root factory resolves (the
package's init() registered it), the category sub-clients are wired, and
NewClient guards a nil parent.
*/

package earn

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

func TestEarn_FactoryResolves(t *testing.T) {
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

	var v any = client.Earn()
	var ec, ok = v.(*Client)
	if !ok || ec == nil {
		t.Fatalf("Earn(): want *earn.Client, got %T", v)
	}
	if ec.Parent() != client {
		t.Error("Parent() mismatch")
	}
	if ec.Account() == nil || ec.Savings() == nil || ec.SharkFin() == nil || ec.Elite() == nil || ec.Loan() == nil {
		t.Error("sub-clients must be wired")
	}
}

func TestEarn_NewClientNilParent(t *testing.T) {
	t.Parallel()
	if NewClient(nil) != nil {
		t.Error("NewClient(nil): want nil")
	}
}
