package interactive

import (
	"regexp"
	"sort"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Port of src/modes/interactive/components/{session-selector-search,
// oauth-selector,login-dialog}.ts.

// SortMode is a session sort mode.
type SortMode string

const (
	SortThreaded  SortMode = "threaded"
	SortRecent    SortMode = "recent"
	SortRelevance SortMode = "relevance"
)

// NameFilter filters sessions by whether they have a name.
type NameFilter string

const (
	NameFilterAll   NameFilter = "all"
	NameFilterNamed NameFilter = "named"
)

// SearchToken is a parsed query token. The JSON field names match upstream.
type SearchToken struct {
	Kind  string `json:"kind"` // "fuzzy" | "phrase"
	Value string `json:"value"`
}

// ParsedSearchQuery is a parsed session search query.
type ParsedSearchQuery struct {
	Mode   string // "tokens" | "regex"
	Tokens []SearchToken
	Regex  *regexp.Regexp
	// Pattern is the raw regex source (without the case-insensitive flag,
	// matching JS RegExp.source).
	Pattern string
	Error   string
}

// MatchResult is a session match outcome.
type MatchResult struct {
	Matches bool
	Score   float64
}

// normalizeWhitespaceLower lowercases and collapses runs of JS whitespace for
// fuzzy matching (upstream `text.toLowerCase().replace(/\s+/g, " ").trim()`;
// Go's `\s` class is ASCII-only).
func normalizeWhitespaceLower(text string) string {
	var builder strings.Builder
	pendingSpace := false
	for _, r := range ai.JSLower(text) {
		if isSpaceRune(r) {
			pendingSpace = true
			continue
		}
		if pendingSpace && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		pendingSpace = false
		builder.WriteRune(r)
	}
	return builder.String()
}

// Session search tiers, best first (pi v1.5.0 8c03fef4d): each token is scored
// by the best field it matches, and a tier dominates any fuzzy score so an exact
// hit anywhere beats a scattered subsequence in a short field.
const (
	tierName  = 0 // the query appears in the session name
	tierText  = 1 // the query appears in the id, messages, or cwd
	tierFuzzy = 2 // the query characters appear scattered in the name or cwd
)

const tierWeight = 1_000_000.0

func tierScore(tier int, score float64) float64 {
	return float64(tier)*tierWeight + score
}

// sessionSearchFields lowercases each field on demand, once per MatchSession.
type sessionSearchFields struct {
	session        coding.SessionInfo
	nameLower      *string
	idLower        *string
	cwdLower       *string
	textLower      *string
	nameNormalized *string
	cwdNormalized  *string
	textNormalized *string
}

func (f *sessionSearchFields) name() string {
	if f.nameLower == nil {
		lower := ai.JSLower(f.session.Name)
		f.nameLower = &lower
	}
	return *f.nameLower
}

func (f *sessionSearchFields) id() string {
	if f.idLower == nil {
		lower := ai.JSLower(f.session.ID)
		f.idLower = &lower
	}
	return *f.idLower
}

func (f *sessionSearchFields) cwd() string {
	if f.cwdLower == nil {
		lower := ai.JSLower(f.session.Cwd)
		f.cwdLower = &lower
	}
	return *f.cwdLower
}

func (f *sessionSearchFields) messages() string {
	if f.textLower == nil {
		lower := ai.JSLower(f.session.AllMessagesText)
		f.textLower = &lower
	}
	return *f.textLower
}

func (f *sessionSearchFields) normalizedName() string {
	if f.nameNormalized == nil {
		normalized := normalizeWhitespaceLower(f.session.Name)
		f.nameNormalized = &normalized
	}
	return *f.nameNormalized
}

func (f *sessionSearchFields) normalizedCwd() string {
	if f.cwdNormalized == nil {
		normalized := normalizeWhitespaceLower(f.session.Cwd)
		f.cwdNormalized = &normalized
	}
	return *f.cwdNormalized
}

func (f *sessionSearchFields) normalizedMessages() string {
	if f.textNormalized == nil {
		normalized := normalizeWhitespaceLower(f.session.AllMessagesText)
		f.textNormalized = &normalized
	}
	return *f.textNormalized
}

// matchLiteralSearchToken matches the query text exactly. Messages and ids are
// only matched exactly: scattered matches in them are noise.
func matchLiteralSearchToken(fields *sessionSearchFields, value string) (float64, bool) {
	name := fields.name()
	if strings.Contains(name, value) {
		return tierScore(tierName, tui.MatchFuzzy(value, name).Score), true
	}
	if strings.Contains(fields.id(), value) || strings.Contains(fields.messages(), value) || strings.Contains(fields.cwd(), value) {
		return tierScore(tierText, 0), true
	}
	return 0, false
}

func matchFuzzySearchToken(fields *sessionSearchFields, value string) (float64, bool) {
	if score, ok := matchLiteralSearchToken(fields, ai.JSLower(value)); ok {
		return score, true
	}
	best := 0.0
	found := false
	for _, field := range []string{fields.session.Name, fields.session.Cwd} {
		match := tui.MatchFuzzy(value, field)
		if match.Matches && (!found || match.Score < best) {
			best = match.Score
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return tierScore(tierFuzzy, best), true
}

func matchPhraseSearchToken(fields *sessionSearchFields, phrase string) (float64, bool) {
	name := fields.normalizedName()
	if strings.Contains(name, phrase) {
		return tierScore(tierName, tui.MatchFuzzy(phrase, name).Score), true
	}
	if strings.Contains(fields.id(), phrase) || strings.Contains(fields.normalizedMessages(), phrase) ||
		strings.Contains(fields.normalizedCwd(), phrase) {
		return tierScore(tierText, 0), true
	}
	return 0, false
}

func matchRegexSearchToken(fields *sessionSearchFields, regex *regexp.Regexp) (float64, bool) {
	if fields.session.Name != "" && regex.MatchString(fields.session.Name) {
		return tierScore(tierName, 0), true
	}
	if regex.MatchString(fields.session.ID) || regex.MatchString(fields.session.AllMessagesText) || regex.MatchString(fields.session.Cwd) {
		return tierScore(tierText, 0), true
	}
	return 0, false
}

// HasSessionName reports whether the session has a non-empty name.
func HasSessionName(session coding.SessionInfo) bool {
	return ai.JSTrim(session.Name) != ""
}

func matchesNameFilter(session coding.SessionInfo, filter NameFilter) bool {
	if filter == NameFilterAll {
		return true
	}
	return HasSessionName(session)
}

// ParseSearchQuery parses a session search query.
func ParseSearchQuery(query string) ParsedSearchQuery {
	trimmed := ai.JSTrim(query)
	if trimmed == "" {
		return ParsedSearchQuery{Mode: "tokens"}
	}

	if strings.HasPrefix(trimmed, "re:") {
		pattern := ai.JSTrim(trimmed[3:])
		if pattern == "" {
			return ParsedSearchQuery{Mode: "regex", Error: "Empty regex"}
		}
		compiled, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return ParsedSearchQuery{Mode: "regex", Error: err.Error()}
		}
		return ParsedSearchQuery{Mode: "regex", Regex: compiled, Pattern: pattern}
	}

	var tokens []SearchToken
	var builder strings.Builder
	inQuote := false
	hadUnclosedQuote := false

	flush := func(kind string) {
		value := ai.JSTrim(builder.String())
		builder.Reset()
		if value == "" {
			return
		}
		tokens = append(tokens, SearchToken{Kind: kind, Value: value})
	}

	for _, character := range trimmed {
		switch {
		case character == '"':
			if inQuote {
				flush("phrase")
				inQuote = false
			} else {
				flush("fuzzy")
				inQuote = true
			}
		case !inQuote && isSpaceRune(character):
			flush("fuzzy")
		default:
			builder.WriteRune(character)
		}
	}
	if inQuote {
		hadUnclosedQuote = true
	}

	if hadUnclosedQuote {
		var fallback []SearchToken
		for _, token := range ai.JSSplitWhitespace(trimmed) {
			value := ai.JSTrim(token)
			if value != "" {
				fallback = append(fallback, SearchToken{Kind: "fuzzy", Value: value})
			}
		}
		return ParsedSearchQuery{Mode: "tokens", Tokens: fallback}
	}

	flush("fuzzy")
	return ParsedSearchQuery{Mode: "tokens", Tokens: tokens}
}

// MatchSession matches a session against a parsed query. Each field is matched
// separately, so a token never matches across field boundaries; different tokens
// may match different fields (pi v1.5.0 8c03fef4d).
func MatchSession(session coding.SessionInfo, parsed ParsedSearchQuery) MatchResult {
	return matchSessionFields(&sessionSearchFields{session: session}, parsed)
}

// sessionSearchCache memoizes the lowercased fields the search matches against,
// keyed by session path and invalidated when a matched field changes. Upstream
// keeps the same cache per SessionInfo object (session-selector-search.ts), so a
// keystroke stops re-lowercasing megabytes of message text.
//
// The cache is owned by the selector and used only on its loop.
type sessionSearchCache struct {
	entries map[string]*cachedSessionFields
}

type cachedSessionFields struct {
	name, cwd, messages string // sources when the fields were computed
	fields              sessionSearchFields
}

func newSessionSearchCache() *sessionSearchCache {
	return &sessionSearchCache{entries: map[string]*cachedSessionFields{}}
}

// fields returns the cached fields for session, recomputing them when one of
// the matched sources changed.
func (c *sessionSearchCache) fields(session coding.SessionInfo) *sessionSearchFields {
	if c == nil {
		return &sessionSearchFields{session: session}
	}
	key := session.Path
	if key == "" {
		key = session.ID
	}
	cached := c.entries[key]
	if cached == nil || cached.name != session.Name || cached.cwd != session.Cwd || cached.messages != session.AllMessagesText {
		cached = &cachedSessionFields{name: session.Name, cwd: session.Cwd, messages: session.AllMessagesText}
		if c.entries == nil {
			c.entries = map[string]*cachedSessionFields{}
		}
		c.entries[key] = cached
	}
	cached.fields.session = session
	return &cached.fields
}

// retain drops entries whose session was not part of the last list, like the
// weak cache upstream dropping objects with their sessions.
func (c *sessionSearchCache) retain(used map[string]bool) {
	if c == nil {
		return
	}
	for key := range c.entries {
		if !used[key] {
			delete(c.entries, key)
		}
	}
}

func matchSessionFields(fields *sessionSearchFields, parsed ParsedSearchQuery) MatchResult {
	if parsed.Mode == "regex" {
		if parsed.Regex == nil {
			return MatchResult{}
		}
		score, ok := matchRegexSearchToken(fields, parsed.Regex)
		if !ok {
			return MatchResult{}
		}
		return MatchResult{Matches: true, Score: score}
	}

	totalScore := 0.0
	for _, token := range parsed.Tokens {
		var score float64
		var ok bool
		if token.Kind == "phrase" {
			phrase := normalizeWhitespaceLower(token.Value)
			if phrase == "" {
				continue
			}
			score, ok = matchPhraseSearchToken(fields, phrase)
		} else {
			score, ok = matchFuzzySearchToken(fields, token.Value)
		}
		if !ok {
			return MatchResult{}
		}
		totalScore += score
	}
	return MatchResult{Matches: true, Score: totalScore}
}

// FilterAndSortSessions filters and sorts sessions for the selector.
func FilterAndSortSessions(sessions []coding.SessionInfo, query string, sortMode SortMode, nameFilter NameFilter) []coding.SessionInfo {
	return filterAndSortSessions(sessions, query, sortMode, nameFilter, nil)
}

// FilterAndSortSessionsCached is FilterAndSortSessions with the selector's
// per-session field cache.
func FilterAndSortSessionsCached(sessions []coding.SessionInfo, query string, sortMode SortMode, nameFilter NameFilter, cache *sessionSearchCache) []coding.SessionInfo {
	return filterAndSortSessions(sessions, query, sortMode, nameFilter, cache)
}

func filterAndSortSessions(sessions []coding.SessionInfo, query string, sortMode SortMode, nameFilter NameFilter, cache *sessionSearchCache) []coding.SessionInfo {
	filtered := sessions
	if nameFilter != NameFilterAll {
		filtered = nil
		for _, session := range sessions {
			if matchesNameFilter(session, nameFilter) {
				filtered = append(filtered, session)
			}
		}
	}
	if ai.JSTrim(query) == "" {
		return filtered
	}

	parsed := ParseSearchQuery(query)
	if parsed.Error != "" {
		return nil
	}

	used := map[string]bool{}
	match := func(session coding.SessionInfo) MatchResult {
		if cache != nil {
			key := session.Path
			if key == "" {
				key = session.ID
			}
			used[key] = true
		}
		return matchSessionFields(cache.fields(session), parsed)
	}

	if sortMode == SortRecent {
		var result []coding.SessionInfo
		for _, session := range filtered {
			if match(session).Matches {
				result = append(result, session)
			}
		}
		cache.retain(used)
		return result
	}

	type scoredSession struct {
		session coding.SessionInfo
		score   float64
	}
	var scored []scoredSession
	for _, session := range filtered {
		result := match(session)
		if result.Matches {
			scored = append(scored, scoredSession{session: session, score: result.Score})
		}
	}
	cache.retain(used)
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].session.Modified.After(scored[j].session.Modified)
		}
		return scored[i].score < scored[j].score
	})
	result := make([]coding.SessionInfo, 0, len(scored))
	for _, entry := range scored {
		result = append(result, entry.session)
	}
	return result
}

func isSpaceRune(r rune) bool { return tui.IsWhitespaceRune(r) }

// ---- OAuth selector ----

// AuthSelectorProvider is a provider entry for the auth selector.
type AuthSelectorProvider struct {
	ID       string
	Name     string
	AuthType string // "oauth" | "api_key"
	Method   any
	Status   *ai.AuthCheck
	// Subscription is nil for the default subscription label, false for an
	// account without a subscription (pi v1.4.0 oauth-selector.ts).
	Subscription *bool
}

// FormatAuthSelectorProviderType formats the auth type label.
func FormatAuthSelectorProviderType(authType string) string {
	return formatAuthSelectorProviderType(authType, nil)
}

func formatAuthSelectorProviderType(authType string, subscription *bool) string {
	if authType == "oauth" {
		if subscription != nil && !*subscription {
			return "account"
		}
		return "subscription"
	}
	return "API key"
}

// OAuthSelectorComponent renders the auth provider selector.
type OAuthSelectorComponent struct {
	*tui.Container

	searchInput        *tui.Input
	focused            bool
	listContainer      *tui.Container
	allProviders       []AuthSelectorProvider
	filteredProviders  []AuthSelectorProvider
	selectedIndex      int
	mode               string
	onSelect           func(providerID string, authType string)
	onCancel           func()
	showAuthTypeLabels bool
}

// NewOAuthSelectorComponent creates the selector.
func NewOAuthSelectorComponent(mode string, providers []AuthSelectorProvider, onSelect func(string, string), onCancel func(), initialSearchInput string) *OAuthSelectorComponent {
	theme := ActiveTheme()
	component := &OAuthSelectorComponent{
		Container:         &tui.Container{},
		mode:              mode,
		allProviders:      providers,
		filteredProviders: providers,
		onSelect:          onSelect,
		onCancel:          onCancel,
	}
	authTypes := map[string]bool{}
	for _, provider := range providers {
		authTypes[provider.AuthType] = true
	}
	component.showAuthTypeLabels = len(authTypes) > 1

	component.AddChild(NewDynamicBorder(nil))
	component.AddChild(tui.NewSpacer(1))
	title := "Select provider to configure:"
	if mode == "logout" {
		title = "Select provider to logout:"
	}
	component.AddChild(tui.NewTruncatedText(theme.Fg("accent", theme.Bold(title)), 1, 0))

	component.AddChild(tui.NewSpacer(1))
	component.searchInput = tui.NewInput(tui.InputOptions{})
	if initialSearchInput != "" {
		component.searchInput.SetValue(initialSearchInput)
	}
	component.searchInput.OnSubmit = func(string) {
		if component.selectedIndex >= 0 && component.selectedIndex < len(component.filteredProviders) {
			selected := component.filteredProviders[component.selectedIndex]
			if component.onSelect != nil {
				component.onSelect(selected.ID, selected.AuthType)
			}
		}
	}
	component.AddChild(component.searchInput)
	component.AddChild(tui.NewSpacer(1))

	component.listContainer = &tui.Container{}
	component.AddChild(component.listContainer)
	component.AddChild(tui.NewSpacer(1))
	component.AddChild(NewDynamicBorder(nil))

	component.FilterProviders(initialSearchInput)
	return component
}

// FilterProviders filters the provider list.
func (c *OAuthSelectorComponent) FilterProviders(query string) {
	if query != "" {
		c.filteredProviders = tui.FuzzyFilter(c.allProviders, query, func(provider AuthSelectorProvider) string {
			methodName := ""
			switch method := provider.Method.(type) {
			case *ai.OAuthAuth:
				if method != nil {
					methodName = method.Name
				}
			case *ai.ApiKeyAuth:
				if method != nil {
					methodName = method.Name
				}
			case interface{ GetName() string }:
				methodName = method.GetName()
			}
			return provider.Name + " " + provider.ID + " " + provider.AuthType + " " + methodName
		})
	} else {
		c.filteredProviders = c.allProviders
	}
	c.selectedIndex = max(0, min(c.selectedIndex, max(0, len(c.filteredProviders)-1)))
	c.updateList()
}

func (c *OAuthSelectorComponent) updateList() {
	theme := ActiveTheme()
	c.listContainer.Clear()

	const maxVisible = 8
	startIndex := max(0, min(c.selectedIndex-maxVisible/2, len(c.filteredProviders)-maxVisible))
	endIndex := min(startIndex+maxVisible, len(c.filteredProviders))
	for i := startIndex; i < endIndex; i++ {
		provider := c.filteredProviders[i]
		isSelected := i == c.selectedIndex
		statusIndicator := formatAuthSelectorProviderStatus(provider)
		authTypeLabel := ""
		if c.showAuthTypeLabels {
			authTypeLabel = theme.Fg("muted", " ["+formatAuthSelectorProviderType(provider.AuthType, provider.Subscription)+"]")
		}
		line := "  " + theme.Fg("text", provider.Name) + authTypeLabel + statusIndicator
		if isSelected {
			line = theme.Fg("accent", "→ ") + theme.Fg("accent", provider.Name) + authTypeLabel + statusIndicator
		}
		c.listContainer.AddChild(tui.NewTruncatedText(line, 1, 0))
	}

	if startIndex > 0 || endIndex < len(c.filteredProviders) {
		c.listContainer.AddChild(tui.NewTruncatedText(theme.Fg("muted",
			"  ("+itoa(c.selectedIndex+1)+"/"+itoa(len(c.filteredProviders))+")"), 1, 0))
	}

	if len(c.filteredProviders) == 0 {
		message := "No matching providers"
		if len(c.allProviders) == 0 {
			if c.mode == "login" {
				message = "No providers available"
			} else {
				message = "No providers logged in. Use /login first."
			}
		}
		c.listContainer.AddChild(tui.NewTruncatedText(theme.Fg("muted", "  "+message), 1, 0))
	}
}

var envVarRegex = regexp.MustCompile(`^[A-Z][A-Z0-9_]*(?:, [A-Z][A-Z0-9_]*)*$`)

func formatAuthSelectorProviderStatus(provider AuthSelectorProvider) string {
	theme := ActiveTheme()
	if provider.Status == nil {
		return theme.Fg("muted", " • not configured")
	}
	if provider.Status.Type != provider.AuthType {
		label := formatAuthSelectorProviderType(provider.Status.Type, provider.Subscription) + " configured"
		return theme.Fg("muted", " • ") + theme.Fg("warning", label)
	}
	if provider.Status.Source == "" || provider.Status.Source == "OAuth" || provider.Status.Source == "stored credential" {
		return theme.Fg("success", " ✓ configured")
	}
	source := provider.Status.Source
	if envVarRegex.MatchString(source) {
		source = "env: " + source
	}
	return theme.Fg("success", " ✓ "+source)
}

// HandleInput processes input.
func (c *OAuthSelectorComponent) HandleInput(data string) {
	kb := tui.GetKeybindings()
	switch {
	case kb.Matches(data, "tui.select.up"):
		if len(c.filteredProviders) == 0 {
			return
		}
		c.selectedIndex = max(0, c.selectedIndex-1)
		c.updateList()
	case kb.Matches(data, "tui.select.down"):
		if len(c.filteredProviders) == 0 {
			return
		}
		c.selectedIndex = min(len(c.filteredProviders)-1, c.selectedIndex+1)
		c.updateList()
	case kb.Matches(data, "tui.select.confirm"):
		if c.selectedIndex >= 0 && c.selectedIndex < len(c.filteredProviders) {
			selected := c.filteredProviders[c.selectedIndex]
			if c.onSelect != nil {
				c.onSelect(selected.ID, selected.AuthType)
			}
		}
	case kb.Matches(data, "tui.select.cancel"):
		if c.onCancel != nil {
			c.onCancel()
		}
	default:
		c.searchInput.HandleInput(data)
		c.FilterProviders(c.searchInput.Value())
	}
}

// SetFocused implements Focusable.
func (c *OAuthSelectorComponent) SetFocused(focused bool) {
	c.focused = focused
	c.searchInput.SetFocused(focused)
}

// IsFocused implements Focusable.
func (c *OAuthSelectorComponent) IsFocused() bool { return c.focused }

// SelectedProvider returns the highlighted provider.
func (c *OAuthSelectorComponent) SelectedProvider() (AuthSelectorProvider, bool) {
	if c.selectedIndex < 0 || c.selectedIndex >= len(c.filteredProviders) {
		return AuthSelectorProvider{}, false
	}
	return c.filteredProviders[c.selectedIndex], true
}

var (
	_ tui.Component = (*OAuthSelectorComponent)(nil)
	_ tui.Focusable = (*OAuthSelectorComponent)(nil)
)
