/*
FILE: copytrading/client_test.go

DESCRIPTION:
M1 scaffold contract: the root CopyTrading() factory resolves once the
package is imported, the default product type is USDT-FUTURES, an
explicit product type is honoured, and all four product×role sub-clients
are constructed (non-nil) and reachable.
*/

package copytrading

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func newParent(t *testing.T) *bitget.Client {
	t.Helper()
	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(bitget.DefaultConfig())
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestM1_RootFactory_ResolvesAndDefaults(t *testing.T) {
	var parent *bitget.Client = newParent(t)

	var v any = parent.CopyTrading()
	if v == nil {
		t.Fatal("CopyTrading() returned nil — factory not registered by init()")
	}
	var c *Client = v.(*Client)
	if c.ProductType() != roottypes.ProductTypeUSDTFutures {
		t.Errorf("default ProductType: want USDT-FUTURES, got %q", c.ProductType())
	}
}

func TestM1_SubClients_NonNil(t *testing.T) {
	var c *Client = NewClient(newParent(t))
	if c.FuturesTrader() == nil {
		t.Error("FuturesTrader() is nil")
	}
	if c.FuturesFollower() == nil {
		t.Error("FuturesFollower() is nil")
	}
	if c.SpotTrader() == nil {
		t.Error("SpotTrader() is nil")
	}
	if c.SpotFollower() == nil {
		t.Error("SpotFollower() is nil")
	}
}

func TestM1_ProductTypePinned(t *testing.T) {
	var c *Client = NewClientWithProductType(newParent(t), roottypes.ProductTypeCoinFutures)
	if c.ProductType() != roottypes.ProductTypeCoinFutures {
		t.Errorf("pinned ProductType: want COIN-FUTURES, got %q", c.ProductType())
	}
}

func TestM1_NilParent(t *testing.T) {
	if NewClient(nil) != nil {
		t.Error("NewClient(nil) must return nil")
	}
}
