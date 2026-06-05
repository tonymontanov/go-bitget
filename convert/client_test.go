/*
FILE: convert/client_test.go

DESCRIPTION:
Smoke test for the CONVERT scaffold: the root factory resolves (the
package's init() registered it) and NewClient guards a nil parent.
*/

package convert

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

func TestConvert_FactoryResolves(t *testing.T) {
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

	var v any = client.Convert()
	var cp, ok = v.(*Client)
	if !ok || cp == nil {
		t.Fatalf("Convert(): want *convert.Client, got %T", v)
	}
	if cp.Parent() != client {
		t.Error("Parent() mismatch")
	}
}

func TestConvert_NewClientNilParent(t *testing.T) {
	t.Parallel()
	if NewClient(nil) != nil {
		t.Error("NewClient(nil): want nil")
	}
}
