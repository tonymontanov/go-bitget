/*
FILE: common/client_test.go

DESCRIPTION:
Smoke test for the COMMON scaffold: the root factory resolves (the
package's init() registered it), the category sub-clients are wired, and
NewClient guards a nil parent.
*/

package common

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

func TestCommon_FactoryResolves(t *testing.T) {
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

	var v any = client.Common()
	var cc, ok = v.(*Client)
	if !ok || cc == nil {
		t.Fatalf("Common(): want *common.Client, got %T", v)
	}
	if cc.Parent() != client {
		t.Error("Parent() mismatch")
	}
	if cc.Public() == nil || cc.Account() == nil || cc.Tax() == nil || cc.P2P() == nil || cc.Users() == nil {
		t.Error("sub-clients must be wired")
	}
}

func TestCommon_NewClientNilParent(t *testing.T) {
	t.Parallel()
	if NewClient(nil) != nil {
		t.Error("NewClient(nil): want nil")
	}
}
