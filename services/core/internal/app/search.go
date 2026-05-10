package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type searXNGResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
		Engine  string `json:"engine"`
	} `json:"results"`
}

type deepSearchCandidate struct {
	Title   string
	URL     string
	Snippet string
	Domain  string
	Engine  string
	Query   string
	Rank    int
	Score   float64
}

func (s *Server) collectDeepSearchSources(ctx context.Context, query string) []store.SearchSource {
	return s.collectDeepSearchSourcesWithConfig(ctx, query, s.enabledDeepSearchConfig(ctx))
}

func (s *Server) enabledDeepSearchConfig(ctx context.Context) *store.SearchProviderConfig {
	config, err := s.Store.GetSearchProviderConfig(ctx)
	if err != nil || config == nil || !config.Enabled {
		return nil
	}
	return config
}

func (s *Server) collectDeepSearchSourcesWithConfig(ctx context.Context, query string, config *store.SearchProviderConfig) []store.SearchSource {
	normalizedQuery := normalizeDeepSearchQuery(query)
	if normalizedQuery == "" {
		return nil
	}
	if config == nil {
		return nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		return nil
	}
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	limit := config.ResultCount
	if limit <= 0 {
		limit = 5
	}
	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	queries := planDeepSearchQueries(query, normalizedQuery)
	if len(queries) == 0 {
		queries = []string{normalizedQuery}
	}
	candidatesByURL := map[string]deepSearchCandidate{}
	for index, plannedQuery := range queries {
		if searchCtx.Err() != nil {
			break
		}
		for _, candidate := range s.querySearXNG(searchCtx, baseURL, plannedQuery) {
			candidate.Query = plannedQuery
			candidate.Score = scoreDeepSearchCandidate(normalizedQuery, plannedQuery, candidate)
			key := canonicalSearchURL(candidate.URL)
			if key == "" {
				continue
			}
			if previous, ok := candidatesByURL[key]; ok && previous.Score >= candidate.Score {
				continue
			}
			candidatesByURL[key] = candidate
		}
		ranked := rankDeepSearchCandidates(candidatesByURL)
		// GPT/Claude-style search does not trust the first query blindly. Give
		// the planner a second pass when early results are weak or too sparse.
		if index > 0 && hasEnoughRelevantSearchCandidates(ranked, limit) {
			break
		}
	}

	ranked := rankDeepSearchCandidates(candidatesByURL)
	if len(ranked) == 0 {
		return nil
	}
	fetchLimit := limit * 2
	if fetchLimit < 3 {
		fetchLimit = 3
	}
	if fetchLimit > len(ranked) {
		fetchLimit = len(ranked)
	}
	for index := 0; index < fetchLimit; index++ {
		if searchCtx.Err() != nil {
			break
		}
		if fetched := s.extractSourceSnippet(searchCtx, ranked[index].URL); fetched != "" {
			if ranked[index].Snippet == "" {
				ranked[index].Snippet = fetched
			} else {
				ranked[index].Snippet = normalizeSearchSnippet(ranked[index].Snippet + "\n" + fetched)
			}
			ranked[index].Score = scoreDeepSearchCandidate(normalizedQuery, ranked[index].Query, ranked[index])
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Rank < ranked[j].Rank
		}
		return ranked[i].Score > ranked[j].Score
	})

	sources := make([]store.SearchSource, 0, limit)
	for _, candidate := range ranked {
		if !isRelevantSearchCandidate(candidate, len(sources)) {
			continue
		}
		sources = append(sources, store.SearchSource{
			Title:   candidate.Title,
			URL:     candidate.URL,
			Snippet: candidate.Snippet,
			Domain:  candidate.Domain,
			Index:   len(sources) + 1,
		})
		if len(sources) >= limit {
			break
		}
	}
	return sources
}

func (s *Server) querySearXNG(ctx context.Context, baseURL string, query string) []deepSearchCandidate {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	endpoint := baseURL + "/search?q=" + url.QueryEscape(query) + "&format=json&language=zh-CN&safesearch=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil
	}
	var decoded searXNGResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 2*1024*1024)).Decode(&decoded); err != nil {
		return nil
	}
	out := make([]deepSearchCandidate, 0, len(decoded.Results))
	for index, result := range decoded.Results {
		sourceURL := strings.TrimSpace(result.URL)
		if sourceURL == "" {
			continue
		}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = sourceURL
		}
		parsed, _ := url.Parse(sourceURL)
		out = append(out, deepSearchCandidate{
			Title:   title,
			URL:     sourceURL,
			Snippet: normalizeSearchSnippet(result.Content),
			Domain:  parsed.Hostname(),
			Engine:  strings.TrimSpace(result.Engine),
			Query:   query,
			Rank:    index + 1,
		})
	}
	return out
}

func planDeepSearchQueries(prompt string, normalizedQuery string) []string {
	normalizedQuery = strings.TrimSpace(normalizedQuery)
	if normalizedQuery == "" {
		return nil
	}
	queries := []string{normalizedQuery}
	keywords := significantSearchTerms(normalizedQuery)
	if len(keywords) > 0 {
		queries = append(queries, strings.Join(keywords, " "))
	}
	if looksLikeFreshnessQuery(prompt) || looksLikeFreshnessQuery(normalizedQuery) {
		queries = append(queries, normalizedQuery+" 最新")
	}
	if len(keywords) >= 2 {
		queries = append(queries, strings.Join(keywords[:minInt(len(keywords), 4)], " ")+" 资料 来源")
	}
	return dedupeSearchQueries(queries, 3)
}

func significantSearchTerms(value string) []string {
	terms := make([]string, 0, 6)
	seen := map[string]bool{}
	for token := range searchTokenWeights(value) {
		if isSearchStopToken(token) || len([]rune(token)) < 2 {
			continue
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		terms = append(terms, token)
	}
	sort.SliceStable(terms, func(i, j int) bool {
		if isASCIIWord(terms[i]) != isASCIIWord(terms[j]) {
			return isASCIIWord(terms[i])
		}
		if len([]rune(terms[i])) != len([]rune(terms[j])) {
			return len([]rune(terms[i])) > len([]rune(terms[j]))
		}
		return terms[i] < terms[j]
	})
	if len(terms) > 6 {
		terms = terms[:6]
	}
	return terms
}

func dedupeSearchQueries(queries []string, limit int) []string {
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, query := range queries {
		query = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(query, " "))
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, query)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func looksLikeFreshnessQuery(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "今天") ||
		strings.Contains(value, "最新") ||
		strings.Contains(value, "现在") ||
		strings.Contains(value, "recent") ||
		strings.Contains(value, "latest") ||
		strings.Contains(value, "today") ||
		strings.Contains(value, "2026")
}

func rankDeepSearchCandidates(items map[string]deepSearchCandidate) []deepSearchCandidate {
	ranked := make([]deepSearchCandidate, 0, len(items))
	for _, item := range items {
		ranked = append(ranked, item)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Rank < ranked[j].Rank
		}
		return ranked[i].Score > ranked[j].Score
	})
	return ranked
}

func hasEnoughRelevantSearchCandidates(candidates []deepSearchCandidate, limit int) bool {
	if len(candidates) == 0 {
		return false
	}
	required := minInt(limit, 3)
	count := 0
	for _, candidate := range candidates {
		if candidate.Score >= 0.28 {
			count++
		}
	}
	return count >= required && candidates[0].Score >= 0.38
}

func isRelevantSearchCandidate(candidate deepSearchCandidate, selectedCount int) bool {
	if candidate.Score >= 0.18 {
		return true
	}
	// Keep one plausible source for sparse exact-name queries without letting a
	// noisy result page flood the model context.
	return selectedCount == 0 && candidate.Score >= 0.10
}

func scoreDeepSearchCandidate(primaryQuery string, plannedQuery string, candidate deepSearchCandidate) float64 {
	queryWeights := searchTokenWeights(primaryQuery + " " + plannedQuery)
	if len(queryWeights) == 0 {
		return 0
	}
	titleScore := weightedTokenOverlap(queryWeights, searchTokenWeights(candidate.Title))
	snippetScore := weightedTokenOverlap(queryWeights, searchTokenWeights(candidate.Snippet))
	urlScore := weightedTokenOverlap(queryWeights, searchTokenWeights(candidate.Domain+" "+candidate.URL))
	score := titleScore*0.52 + snippetScore*0.36 + urlScore*0.12
	lowerHaystack := strings.ToLower(candidate.Title + "\n" + candidate.Snippet + "\n" + candidate.URL)
	lowerPrimary := strings.ToLower(strings.TrimSpace(primaryQuery))
	if lowerPrimary != "" && strings.Contains(lowerHaystack, lowerPrimary) {
		score += 0.22
	}
	if candidate.Rank > 0 && candidate.Rank <= 5 {
		score += 0.04 / float64(candidate.Rank)
	}
	if strings.TrimSpace(candidate.Snippet) == "" {
		score -= 0.04
	}
	if score < 0 {
		return 0
	}
	return score
}

func weightedTokenOverlap(queryWeights map[string]float64, documentWeights map[string]float64) float64 {
	if len(queryWeights) == 0 || len(documentWeights) == 0 {
		return 0
	}
	var total float64
	var matched float64
	for token, weight := range queryWeights {
		total += weight
		if _, ok := documentWeights[token]; ok {
			matched += weight
		}
	}
	if total == 0 {
		return 0
	}
	return matched / total
}

func searchTokenWeights(value string) map[string]float64 {
	weights := map[string]float64{}
	for _, token := range regexp.MustCompile(`[a-zA-Z0-9][a-zA-Z0-9._+-]{1,}`).FindAllString(strings.ToLower(value), -1) {
		token = strings.Trim(token, "._+-")
		if token == "" || isSearchStopToken(token) {
			continue
		}
		weights[token] = maxFloat(weights[token], 1.2)
	}
	for _, segment := range hanSegments(value) {
		runes := []rune(segment)
		if len(runes) >= 2 && !isSearchStopToken(segment) {
			weights[segment] = maxFloat(weights[segment], 1.4)
		}
		for size := 2; size <= 3; size++ {
			if len(runes) < size {
				continue
			}
			for index := 0; index <= len(runes)-size; index++ {
				token := string(runes[index : index+size])
				if isSearchStopToken(token) {
					continue
				}
				weights[token] = maxFloat(weights[token], 0.7)
			}
		}
	}
	return weights
}

func hanSegments(value string) []string {
	segments := make([]string, 0)
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		segments = append(segments, builder.String())
		builder.Reset()
	}
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			builder.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return segments
}

func isSearchStopToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "", "the", "and", "for", "with", "from", "this", "that", "what", "when", "where", "how",
		"搜索", "检索", "联网", "深度", "引用", "编号", "请帮", "帮我", "告诉", "说明", "什么", "怎么", "如何",
		"今天", "现在", "最新", "资料", "来源", "一下", "一个", "这个", "那个":
		return true
	default:
		return false
	}
}

func isASCIIWord(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func canonicalSearchURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimRight(parsed.String(), "/")
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func normalizeDeepSearchQuery(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	quotedPatterns := []*regexp.Regexp{
		regexp.MustCompile(`[“"]([^”"]{2,80})[”"]`),
		regexp.MustCompile(`[‘']([^’']{2,80})[’']`),
	}
	for _, pattern := range quotedPatterns {
		if match := pattern.FindStringSubmatch(prompt); len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}
	if strings.Contains(prompt, "北京时间") && (strings.Contains(prompt, "几点") || strings.Contains(prompt, "时间")) {
		return "北京时间 现在几点"
	}
	tokenPattern := regexp.MustCompile(`[A-Za-z][A-Za-z0-9._+-]{2,}(?:-[A-Za-z0-9._+-]+)*`)
	tokens := tokenPattern.FindAllString(prompt, -1)
	seen := map[string]bool{}
	keywords := make([]string, 0, 6)
	for _, token := range tokens {
		token = strings.Trim(token, ".,;:!?，。；：！？()（）[]【】")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if seen[lower] || isSearchInstructionToken(lower) {
			continue
		}
		seen[lower] = true
		keywords = append(keywords, token)
		if len(keywords) >= 6 {
			break
		}
	}
	if len(keywords) > 0 {
		return strings.Join(keywords, " ")
	}
	cleaned := prompt
	replacer := strings.NewReplacer(
		"深度搜索", " ",
		"链路验收", " ",
		"联网检索", " ",
		"联网搜索", " ",
		"检索", " ",
		"搜索", " ",
		"请", " ",
		"帮我", " ",
		"给我", " ",
		"告诉我", " ",
		"查一下", " ",
		"搜一下", " ",
		"你知道", " ",
		"用一句中文", " ",
		"说明", " ",
		"是什么", " ",
		"并保留引用编号", " ",
		"保留引用编号", " ",
		"给出引用", " ",
	)
	cleaned = replacer.Replace(cleaned)
	cleaned = regexp.MustCompile(`[，。；：！？、\s]+`).ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	runes := []rune(cleaned)
	if len(runes) > 120 {
		cleaned = strings.TrimSpace(string(runes[:120]))
	}
	return cleaned
}

func isSearchInstructionToken(token string) bool {
	switch token {
	case "http", "https", "www", "api", "url", "html", "svg", "react", "vue":
		return true
	default:
		return false
	}
}

func (s *Server) extractSourceSnippet(ctx context.Context, sourceURL string) string {
	requestCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Infinite-AI-SearchBot/1.0")
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.5")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 512*1024))
	if err != nil {
		return ""
	}
	return normalizeSearchSnippet(stripHTMLForSearch(string(raw)))
}

func normalizeSearchSnippet(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = regexp.MustCompile(`\s+`).ReplaceAllString(value, " ")
	runes := []rune(value)
	if len(runes) > 520 {
		value = strings.TrimSpace(string(runes[:520])) + "..."
	}
	return value
}

func stripHTMLForSearch(value string) string {
	value = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(value, " ")
	value = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(value, " ")
	value = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(value, " ")
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return replacer.Replace(value)
}
