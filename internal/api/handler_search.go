package api

import (
	"cmp"
	"net/http"
	"slices"

	"github.com/txperl/pixivgo"
	"golang.org/x/sync/errgroup"

	"github.com/txperl/PixivBiu/internal/pixiv"
)

// upstreamSearchPageSize is Pixiv's fixed search page size: every search response
// carries at most this many works and its next_url advances the offset by exactly
// this amount. The ranked window relies on that determinism to fan its upstream
// pages out (offset = base + i*upstreamSearchPageSize) instead of walking next_url
// one page at a time; the client's fixed-stride pager assumes the same size.
const upstreamSearchPageSize = 30

func (h *APIHandler) SearchIllusts(w http.ResponseWriter, r *http.Request, params SearchIllustsParams) {
	if err := h.requirePublicRead(r); err != nil {
		WriteError(w, r, err)
		return
	}
	// bookmarks_desc / views_desc are synthetic illust-only sorts: Pixiv's
	// native popularity sort is Premium-only, so we sample a bounded set of
	// date_desc results and rank them locally on counts every account already
	// sees. Everything else is a straight Pixiv passthrough.
	if sort := derefEnum(params.Sort); isRankedSort(sort) {
		h.searchIllustsRanked(w, r, params, sort)
		return
	}
	resp, err := pixiv.CallRequest(r.Context(), h.svc, r, func(c *pixivgo.Client) (*pixivgo.SearchIllustrations, error) {
		return c.SearchIllust(r.Context(), searchIllustParams(params, pixivgo.Sort(derefEnum(params.Sort)), i64OptToIntOpt(params.Offset)))
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, IllustPage{
		Illusts:    resp.Illusts,
		NextOffset: pixiv.NextOffset(resp.NextURL),
	})
}

// searchIllustsRanked backs the synthetic bookmarks_desc / views_desc sorts. It
// treats one response page as a window of search.sample.pages upstream pages of
// date_desc results (honoring the same word/target/duration/date/AI filters)
// starting at params.Offset, dedupes by illust id, and ranks that window by the
// chosen count (descending, stable so ties keep date order). next_offset carries
// the upstream offset where the *next* window begins, so the client pages through
// disjoint ranked windows (page 2 = the next sample.pages*30 works, re-ranked);
// it is null only at the true end of results.
//
// The window is fetched in parallel, bounded by search.sample.concurrency. Pixiv
// search offsets are deterministic (next_url just advances by upstreamSearchPageSize),
// so each upstream page's offset is base + i*upstreamSearchPageSize — there's no
// need to walk next_url one page at a time. Results are merged in offset order, so
// dedupe order (and thus the stable ranking) matches the old sequential walk. Any
// page failing surfaces the error rather than returning a short window: the client
// paginates by a fixed offset stride (page * sample.pages * 30), not by following
// next_offset, so a partial window would silently skip offsets on the next page —
// errgroup cancels the siblings and returns the first error for the same reason.
func (h *APIHandler) searchIllustsRanked(w http.ResponseWriter, r *http.Request, params SearchIllustsParams, sort string) {
	pages := max(int(h.searchSamplePages.Load()), 1)
	// Both live via atomic (Manager.Config() is boot-pinned). Clamp concurrency to
	// [1, pages]: a wider limit can't help a window smaller than it.
	concurrency := min(max(int(h.searchSampleConcurrency.Load()), 1), pages)

	base := 0
	if params.Offset != nil {
		base = int(*params.Offset)
	}

	// Resolve the upstream identity ONCE for the whole window: round-robining
	// pool.Next() per page would burn one rotation slot per fan-out goroutine
	// and mix identities inside one ranked result set. The gate above already
	// passed, but the pool can drain between gate and now — fail closed then.
	refresh, ok := h.svc.ReadRefreshToken(r)
	if !ok {
		WriteError(w, r, pixiv.ErrNotAuthenticated)
		return
	}

	// Fan the window out into pre-sized slots so each goroutine writes its own index
	// without locking; the order is preserved for the in-order merge below.
	results := make([]*pixivgo.SearchIllustrations, pages)
	g, ctx := errgroup.WithContext(r.Context())
	g.SetLimit(concurrency)
	for i := range pages {
		off := base + i*upstreamSearchPageSize
		g.Go(func() error {
			p := searchIllustParams(params, pixivgo.SortDateDesc, &off)
			resp, err := pixiv.CallRefresh(ctx, h.svc, refresh, func(c *pixivgo.Client) (*pixivgo.SearchIllustrations, error) {
				return c.SearchIllust(ctx, p)
			})
			if err != nil {
				return err
			}
			results[i] = resp
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		WriteError(w, r, err)
		return
	}

	seen := make(map[int]struct{}, pages*upstreamSearchPageSize)
	all := make([]pixivgo.IllustrationInfo, 0, pages*upstreamSearchPageSize)
	// reachedEnd mirrors the old sequential break: a page that comes back empty or
	// without a next_url is the tail of results. Pages fanned out past that tail just
	// return empty (offsets are monotonic), so merging them adds nothing — but it
	// means next_offset must be nil so the client's "next page" closes.
	reachedEnd := false
	for _, resp := range results {
		if len(resp.Illusts) == 0 || pixiv.NextOffset(resp.NextURL) == nil {
			reachedEnd = true
		}
		for _, il := range resp.Illusts {
			id := il.ID.Int()
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			all = append(all, il)
		}
	}
	var nextWindow *int64 // upstream offset where the next window starts; nil = end of results
	if !reachedEnd {
		nw := int64(base + pages*upstreamSearchPageSize)
		nextWindow = &nw
	}

	field := func(il pixivgo.IllustrationInfo) int { return il.TotalBookmarks }
	if sort == string(ViewsDesc) {
		field = func(il pixivgo.IllustrationInfo) int { return il.TotalView }
	}
	slices.SortStableFunc(all, func(a, b pixivgo.IllustrationInfo) int {
		return cmp.Compare(field(b), field(a)) // descending
	})

	writeJSON(w, http.StatusOK, IllustPage{Illusts: all, NextOffset: nextWindow})
}

func (h *APIHandler) SearchUsers(w http.ResponseWriter, r *http.Request, params SearchUsersParams) {
	if err := h.requirePublicRead(r); err != nil {
		WriteError(w, r, err)
		return
	}
	resp, err := pixiv.CallRequest(r.Context(), h.svc, r, func(c *pixivgo.Client) (*pixivgo.UserListResponse, error) {
		return c.SearchUser(r.Context(), pixivgo.SearchUserParams{
			Word:     params.Word,
			Sort:     pixivgo.Sort(userSearchSort(params.Sort)),
			Duration: durationOpt(params.Duration),
			Filter:   pixivgo.Filter(derefEnum(params.ClientMode)),
			Offset:   i64OptToIntOpt(params.Offset),
		})
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UserPreviewPage{
		UserPreviews: resp.UserPreviews,
		NextOffset:   pixiv.NextOffset(resp.NextURL),
	})
}

// isRankedSort reports whether s is one of the synthetic local-ranking sorts
// (bookmarks_desc / views_desc) the server fulfills by sampling + ranking
// date_desc results, rather than forwarding to Pixiv.
func isRankedSort(s string) bool {
	return s == string(BookmarksDesc) || s == string(ViewsDesc)
}

// searchIllustParams maps our query params to pixivgo's. The caller supplies the
// sort and starting offset: SearchIllusts forwards the requested sort; the ranked
// path pins date_desc and walks the offset window by window.
func searchIllustParams(params SearchIllustsParams, sort pixivgo.Sort, offset *int) pixivgo.SearchIllustParams {
	return pixivgo.SearchIllustParams{
		Word:         params.Word,
		SearchTarget: pixivgo.SearchTarget(derefEnum(params.SearchTarget)),
		Sort:         sort,
		Duration:     durationOpt(params.Duration),
		StartDate:    params.StartDate,
		EndDate:      params.EndDate,
		Filter:       pixivgo.Filter(derefEnum(params.ClientMode)),
		SearchAIType: excludeAIOpt(params.ExcludeAi),
		Offset:       offset,
	}
}

// userSearchSort maps the shared SearchSort to a value valid for user search.
// bookmarks_desc/views_desc are illust-only synthetic sorts (the UI doesn't
// offer them for users); if one slips through, fall back to date_desc so we
// never hand Pixiv's user-search endpoint a token it would reject.
func userSearchSort(p *SearchSort) string {
	sort := derefEnum(p)
	if isRankedSort(sort) {
		return string(DateDesc)
	}
	return sort
}

func durationOpt(p *DurationQuery) *pixivgo.Duration {
	if p == nil {
		return nil
	}
	d := pixivgo.Duration(*p)
	return &d
}

func excludeAIOpt(p *ExcludeAiQuery) *int {
	if p == nil || !*p {
		return nil
	}
	v := 1
	return &v
}

// SearchTrendingTags serves Pixiv's trending-tags-illust feed: the tags du
// jour, each with one sample illustration for a thumbnail strip. A read —
// pool-gated like the other reads, so anonymous public callers authenticate
// upstream through the service token pool. It goes through the raw JSON call
// path because upstream returns `trend_tags[].tag` as a plain string while
// pixivgo.TrendTag models it as an object (the typed method fails unmarshaling
// a perfectly good response).
func (h *APIHandler) SearchTrendingTags(w http.ResponseWriter, r *http.Request) {
	if err := h.requirePublicRead(r); err != nil {
		WriteError(w, r, err)
		return
	}
	var resp pixiv.TrendingTagsResponse
	if err := h.svc.AppJSONGet(r.Context(), r, "/v1/trending-tags/illust", &resp); err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
