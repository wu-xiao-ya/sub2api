package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddyCreditsUsesActualRemainingAndPreservesFractions(t *testing.T) {
	for _, tc := range []struct {
		name, enterprise, path, response string
		remaining                        float64
		invalid                          bool
	}{
		{"personal", "", "/v2/billing/meter/get-user-resource", `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CycleCapacityRemain":96,"CycleCapacityRemainPrecise":"96.61"},{"CapacityRemain":2}]}}}}`, 98.61, false},
		{"enterprise", "tenant", "/billing/meter/get-enterprise-user-usage", `{"code":0,"data":{"Data":{"limitNum":500,"credit":48.5}}}`, 451.5, false},
		{"missing", "", "/v2/billing/meter/get-user-resource", `{"code":0,"data":{}}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, tc.path, r.URL.Path)
				require.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
				require.Equal(t, tc.enterprise, r.Header.Get("X-Tenant-Id"))
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			s := newServer()
			s.billing = upstream.URL
			result, err := s.fetchCredits(context.Background(), credential{AccessToken: "fixture", EnterpriseID: tc.enterprise})
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tc.remaining, result["remaining"], 0.000001)
		})
	}
}
