//go:build integration

/*
FILE: integration/common_test.go

DESCRIPTION:
Live, UNSIGNED checks for the COMMON / PUBLIC profile (server time +
announcements). Runs without credentials.
*/

package integration

import (
	"testing"

	"github.com/tonymontanov/go-bitget/v2/common"
)

func commonClient(t *testing.T) *common.Client {
	t.Helper()
	var v any = newClient(t).Common()
	var cm, ok = v.(*common.Client)
	if !ok || cm == nil {
		t.Fatalf("Common(): want *common.Client, got %T", v)
	}
	return cm
}

func TestLive_Common_ServerTime(t *testing.T) {
	var cm = commonClient(t)
	var ms, err = cm.Public().GetServerTime(testCtx(t))
	if err != nil {
		t.Fatalf("GetServerTime: %v", err)
	}
	if ms < 1_600_000_000_000 {
		t.Fatalf("implausible server time: %d", ms)
	}
	t.Logf("common server time: %d", ms)
}

func TestLive_Common_Announcements(t *testing.T) {
	var cm = commonClient(t)
	var anns, err = cm.Public().GetAnnouncements(testCtx(t), common.AnnouncementsQuery{Language: "en-US"})
	if err != nil {
		t.Fatalf("GetAnnouncements: %v", err)
	}
	t.Logf("announcements: %d", len(anns))
	if len(anns) > 0 {
		t.Logf("latest: %q (%s)", anns[0].AnnTitle, anns[0].AnnURL)
	}
}
