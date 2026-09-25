package pixiv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/txperl/pixivgo"
)

// defaultAppAPIBase mirrors pixivgo's defaultHosts: the base its typed
// methods use when no bypass-SNI base is configured.
const defaultAppAPIBase = "https://app-api.pixiv.net"

// Wire models for the App API trending-tags feed, decoded by AppJSONGet
// instead of pixivgo.TrendingTagsResponse: upstream returns
// `trend_tags[].tag` as a plain string while pixivgo.TrendTag models it as an
// object, so the typed method fails unmarshaling a perfectly good response.
// The generated OpenAPI Go types alias these.
type TrendingTagsResponse struct {
	TrendTags []TrendingTag `json:"trend_tags"`
}

type TrendingTag struct {
	Tag            string                   `json:"tag"`
	TranslatedName *string                  `json:"translated_name"`
	Illust         pixivgo.IllustrationInfo `json:"illust"`
}

// AppJSONGet issues an authenticated GET against the App API and decodes the
// JSON body into out. It resolves the per-request identity exactly like
// CallRequest (user token → operator session; public mode → pool; local mode
// → operator session; none → ErrNotAuthenticated) and mirrors CallRefresh's
// bounded single re-exchange retry and invalid_grant eviction, but performs
// the request and decoding locally — for endpoints pixivgo's typed methods
// cannot decode (trending-tags, above).
func (s *Service) AppJSONGet(ctx context.Context, r *http.Request, path string, out any) error {
	refresh, ok := s.ReadRefreshToken(r)
	if !ok {
		return ErrNotAuthenticated
	}
	access, err := s.resolveAccess(ctx, refresh, "")
	if err != nil {
		return err
	}
	err = s.rawGetJSON(ctx, access, path, out)
	if err == nil || !isAuthError(err) {
		return err
	}
	// The cached/re-exchanged access token was rejected — force one fresh
	// exchange and replay, mirroring CallRefresh's bounded retry.
	access, err = s.resolveAccess(ctx, refresh, access)
	if err != nil {
		return err
	}
	return s.rawGetJSON(ctx, access, path, out)
}

// resolveAccess returns the access token for the resolved identity: the
// operator session's own token, or a pooled session's token through the
// shared singleflight/cache lifecycle (rejectAccess is the bounded post-401
// re-exchange key — see poolSessionClient).
func (s *Service) resolveAccess(ctx context.Context, refresh, rejectAccess string) (string, error) {
	if s.Authenticated() && refresh == s.refreshToken() {
		return s.Snapshot().AccessToken, nil
	}
	_, access, err := s.poolSessionClient(ctx, refresh, rejectAccess)
	if err != nil {
		return "", s.poolSessionFailure(refresh, err)
	}
	return access, nil
}

// rawGetJSON performs the authenticated GET with the same iOS app headers
// pixivgo sets, and decodes a 2xx JSON body into out. Non-2xx responses
// surface as *pixivgo.PixivError so isAuthError / isInvalidGrant / classify
// treat the raw path exactly like the typed one.
func (s *Service) rawGetJSON(ctx context.Context, access, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase+path, nil)
	if err != nil {
		return &pixivgo.PixivError{Message: fmt.Sprintf("create request error: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("User-Agent", "PixivIOSApp/7.13.3 (iOS 14.6; iPhone13,2)")
	req.Header.Set("App-OS", "ios")
	req.Header.Set("App-OS-Version", "14.6")

	resp, err := s.httpc.Do(req)
	if err != nil {
		return &pixivgo.PixivError{Message: fmt.Sprintf("auth request error: %v", err), Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return &pixivgo.PixivError{Message: fmt.Sprintf("read response error: %v", readErr), Err: readErr}
	}
	if resp.StatusCode != http.StatusOK {
		return &pixivgo.PixivError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &pixivgo.PixivError{Message: fmt.Sprintf("json unmarshal error: %v (HTTP %d)", err, resp.StatusCode)}
	}
	return nil
}
