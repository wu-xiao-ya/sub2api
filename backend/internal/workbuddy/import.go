package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MaxImportBytes = 256 << 10

type importRecord struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Domain       string `json:"domain"`
	ExpiresAt    int64  `json:"expiresAt"`
}

func decodeImport(raw string) ([]importRecord, error) {
	b := bytes.TrimSpace([]byte(raw))
	invalid := status.Error(codes.InvalidArgument, "expected a WorkBuddy JSON account or an array of up to 16 accounts")
	if len(b) == 0 || len(b) > MaxImportBytes {
		return nil, invalid
	}
	var rows []importRecord
	switch b[0] {
	case '[':
		if json.Unmarshal(b, &rows) != nil {
			return nil, invalid
		}
	case '{':
		var row importRecord
		if json.Unmarshal(b, &row) != nil {
			return nil, invalid
		}
		rows = []importRecord{row}
	}
	if len(rows) == 0 || len(rows) > 16 {
		return nil, invalid
	}
	return rows, nil
}

func (s *server) importAccounts(ctx context.Context, c configuration, raw string) (map[string]any, bool, error) {
	rows, err := decodeImport(raw)
	if err != nil {
		return nil, false, err
	}
	type result struct {
		Index       int    `json:"index"`
		IdentityRef string `json:"identity_ref,omitempty"`
		Reason      string `json:"reason,omitempty"`
	}
	results := make([]result, 0, len(rows))
	changed := false
	for index, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		ref, reason := s.importAccount(ctx, c, row)
		results = append(results, result{Index: index + 1, IdentityRef: ref, Reason: reason})
		changed = changed || ref != ""
	}
	return map[string]any{"results": results}, changed, nil
}

func (s *server) importAccount(ctx context.Context, c configuration, row importRecord) (string, string) {
	a := credential{
		AccessToken: strings.TrimSpace(row.AccessToken), RefreshToken: strings.TrimSpace(row.RefreshToken),
		Domain: strings.TrimSpace(row.Domain), ExpiresAt: row.ExpiresAt,
	}
	if len(a.AccessToken) == 0 || len(a.AccessToken) > 16384 || len(a.RefreshToken) > 16384 ||
		strings.ContainsAny(a.AccessToken+a.RefreshToken, "\r\n") || !domesticDomain(a.Domain) {
		return "", "invalid_credentials_or_region"
	}
	// DSH exports milliseconds. Accept Unix seconds from hand-written JSON too.
	if a.ExpiresAt > 100000000000 {
		a.ExpiresAt /= 1000
	}
	now := s.now().Unix()
	if a.ExpiresAt <= now+60 {
		if a.RefreshToken == "" {
			return "", "expiry_or_refresh_required"
		}
		data, err := s.refresh(ctx, a)
		if err != nil {
			return "", "refresh_failed"
		}
		a.AccessToken = str(data, "accessToken", "access_token")
		a.ExpiresAt = expiry(data, now)
		if token := str(data, "refreshToken", "refresh_token"); token != "" {
			a.RefreshToken = token
		}
		if a.AccessToken == "" || a.ExpiresAt <= now+60 {
			return "", "refresh_failed"
		}
	}
	profile, err := s.document(ctx, http.MethodGet, s.billing+"/v2/plugin/login/account", &a, nil)
	if err != nil {
		return "", "profile_unavailable"
	}
	a.UID = str(profile, "uid", "Uid", "userId", "UserID")
	a.EnterpriseID = str(profile, "enterpriseId", "enterprise_id", "EnterpriseId")
	if domain := str(profile, "domain", "Domain"); domain != "" && !domesticDomain(domain) {
		return "", "invalid_credentials_or_region"
	}
	if a.UID == "" {
		return "", "identity_unavailable"
	}
	a.Models, err = s.models(ctx, a)
	if err != nil || len(a.Models) == 0 {
		return "", "deepseek_catalog_unavailable"
	}
	ref := identityRef(a.UID)
	c.Accounts[ref] = a
	return ref, ""
}
