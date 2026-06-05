/*
FILE: common/public.go

DESCRIPTION:
Public sub-client — UNSIGNED utility reads (/api/v2/public/...):
server time and platform announcements. These send no ACCESS-SIGN header
(Signed: false); they work without API credentials.

Request params verified against the Bitget V2 docs and the tiagosiebler
reference client.
*/

package common

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	commontypes "github.com/tonymontanov/go-bitget/v2/common/types"
)

// PublicClient — unsigned public-utility sub-client.
type PublicClient struct {
	c *Client
}

func newPublicClient(c *Client) *PublicClient {
	return &PublicClient{c: c}
}

// ---------------------------------------------------------------------
// GetServerTime — public/time (unsigned).
// ---------------------------------------------------------------------

type serverTimeRow struct {
	ServerTime string `json:"serverTime"`
}

// GetServerTime returns the Bitget server time in epoch milliseconds.
// Unsigned.
func (p *PublicClient) GetServerTime(ctx context.Context) (int64, error) {
	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/public/time",
		Signed: false,
		Meta:   queryMeta(),
	})
	if err != nil {
		return 0, err
	}
	var row serverTimeRow
	if err = resp.UnmarshalData(&row); err != nil {
		return 0, errParse("Public.GetServerTime", err)
	}
	var ms int64
	ms, _ = bgcommon.ParseInt64OrZero(row.ServerTime)
	return ms, nil
}

// ---------------------------------------------------------------------
// GetAnnouncements — public/annoucements (unsigned).
// ---------------------------------------------------------------------

// AnnouncementsQuery — filters for GetAnnouncements. Language is required
// by the venue; AnnType narrows the category (e.g. "latest_news",
// "coin_listings", "trading_competitions", ...).
type AnnouncementsQuery struct {
	Language    string
	AnnType     string
	StartTimeMs int64
	EndTimeMs   int64
}

type announcementRow struct {
	AnnID    string `json:"annId"`
	AnnTitle string `json:"annTitle"`
	AnnDesc  string `json:"annDesc"`
	CTime    string `json:"cTime"`
	Language string `json:"language"`
	AnnURL   string `json:"annUrl"`
}

// GetAnnouncements lists platform announcements. Language is required.
// Unsigned. (Note the venue's misspelled path "annoucements".)
func (p *PublicClient) GetAnnouncements(ctx context.Context, q AnnouncementsQuery) ([]commontypes.Announcement, error) {
	if q.Language == "" {
		return nil, errInvalid("Public.GetAnnouncements", "language is required")
	}

	var query url.Values = url.Values{}
	query.Set("language", q.Language)
	if q.AnnType != "" {
		query.Set("annType", q.AnnType)
	}
	if q.StartTimeMs > 0 {
		query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	}
	if q.EndTimeMs > 0 {
		query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/public/annoucements",
		Query:  query,
		Signed: false,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []announcementRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Public.GetAnnouncements", err)
	}
	var out []commontypes.Announcement = make([]commontypes.Announcement, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var a commontypes.Announcement = commontypes.Announcement{
			AnnID:    rows[i].AnnID,
			AnnTitle: rows[i].AnnTitle,
			AnnDesc:  rows[i].AnnDesc,
			AnnURL:   rows[i].AnnURL,
			Language: rows[i].Language,
		}
		a.CTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		out = append(out, a)
	}
	return out, nil
}
