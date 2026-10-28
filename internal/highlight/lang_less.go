package highlight

var (
	lessM0  *Mode
	lessM1  *Mode
	lessM2  *Mode
	lessM3  *Mode
	lessM4  *Mode
	lessM5  *Mode
	lessM6  *Mode
	lessM7  *Mode
	lessM8  *Mode
	lessM9  *Mode
	lessM10 *Mode
	lessM11 *Mode
	lessM12 *Mode
	lessM13 *Mode
	lessM14 *Mode
	lessM15 *Mode
	lessM16 *Mode
	lessM17 *Mode
	lessM18 *Mode
	lessM19 *Mode
	lessM20 *Mode
	lessM21 *Mode
	lessM22 *Mode
	lessM23 *Mode
	lessM24 *Mode
	lessM25 *Mode
	lessM26 *Mode
	lessM27 *Mode
	lessM28 *Mode
	lessM29 *Mode
	lessM30 *Mode
	lessM31 *Mode
	lessM32 *Mode
	lessM33 *Mode
	lessM34 *Mode
	lessM35 *Mode
	lessM36 *Mode
	lessM37 *Mode
	lessM38 *Mode
	lessM39 *Mode
	lessM40 *Mode
	lessM41 *Mode
	lessM42 *Mode
	lessM43 *Mode
	lessM44 *Mode
	lessM45 *Mode
	lessM46 *Mode
	lessM47 *Mode
	lessM48 *Mode
)

func init() {
	lessM0 = &Mode{ClassName: "comment", Begin: "//", End: "$"}
	lessM1 = &Mode{Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b"}
	lessM2 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	lessM3 = &Mode{ClassName: "comment", Begin: "/\\*", End: "\\*/"}
	lessM4 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	lessM5 = &Mode{ClassName: "keyword", Begin: "@(import|media|charset|font-face|(-[a-z]+-)?keyframes|supports|document|namespace|page|viewport|host)\\b"}
	lessM6 = &Mode{End: "[;{}]", ReturnEnd: true, HasRelevance: true, Relevance: 0, Keywords: Keywords{"$pattern": "[a-z-]+", "keyword": "and or not only", "attribute": "any-hover any-pointer aspect-ratio color color-gamut color-index device-aspect-ratio device-height device-width display-mode forced-colors grid height hover inverted-colors monochrome orientation overflow-block overflow-inline pointer prefers-color-scheme prefers-contrast prefers-reduced-motion prefers-reduced-transparency resolution scan scripting update width min-width max-width min-height max-height"}}
	lessM7 = &Mode{ClassName: "string", Begin: "~?'.*?'"}
	lessM8 = &Mode{ClassName: "string", Begin: "~?\".*?\""}
	lessM9 = &Mode{ClassName: "number", Begin: "\\b\\d+(\\.\\d+)?(%|em|ex|ch|rem|vw|vh|vmin|vmax|cm|mm|in|pt|pc|px|deg|grad|rad|turn|s|ms|Hz|kHz|dpi|dpcm|dppx)?", HasRelevance: true, Relevance: 0}
	lessM10 = &Mode{Begin: "(url|data-uri)\\("}
	lessM11 = &Mode{ClassName: "string", End: "[\\)\\n]", ExcludeEnd: true}
	lessM12 = &Mode{ClassName: "number", Begin: "#([a-fA-F0-9]{6}|[a-fA-F0-9]{3})"}
	lessM13 = &Mode{Begin: "\\(", End: "\\)", HasRelevance: true, Relevance: 0, Keywords: Keywords{"keyword": "and or not only", "attribute": "any-hover any-pointer aspect-ratio color color-gamut color-index device-aspect-ratio device-height device-width display-mode forced-colors grid height hover inverted-colors monochrome orientation overflow-block overflow-inline pointer prefers-color-scheme prefers-contrast prefers-reduced-motion prefers-reduced-transparency resolution scan scripting update width min-width max-width min-height max-height"}}
	lessM14 = &Mode{ClassName: "variable", Begin: "@@?[\\w-]+", HasRelevance: true, Relevance: 10}
	lessM15 = &Mode{ClassName: "variable", Begin: "@\\{[\\w-]+\\}"}
	lessM16 = &Mode{ClassName: "built_in", Begin: "~?`[^`]*?`"}
	lessM17 = &Mode{ClassName: "attribute", Begin: "[\\w-]+\\s*:", End: ":", ReturnBegin: true, ExcludeEnd: true}
	lessM18 = &Mode{ClassName: "meta", Begin: "!important"}
	lessM19 = &Mode{ClassName: "variable"}
	lessM20 = &Mode{ClassName: "variable", Begin: "@[\\w-]+\\s*:", HasRelevance: true, Relevance: 15}
	lessM21 = &Mode{End: "[;}]", ReturnEnd: true}
	lessM22 = &Mode{Begin: "\\{", End: "\\}"}
	lessM23 = &Mode{Begin: "[\\w-]+:(:)?(active|any-link|blank|checked|current|default|defined|dir|disabled|drop|empty|enabled|first|first-child|first-of-type|fullscreen|future|focus|focus-visible|focus-within|has|host|host-context|hover|indeterminate|in-range|invalid|is|lang|last-child|last-of-type|left|link|local-link|not|nth-child|nth-col|nth-last-child|nth-last-col|nth-last-of-type|nth-of-type|only-child|only-of-type|optional|out-of-range|past|placeholder-shown|read-only|read-write|required|right|root|scope|target|target-within|user-invalid|valid|visited|where|after|backdrop|before|cue|cue-region|first-letter|first-line|grammar-error|marker|part|placeholder|selection|slotted|spelling-error)", ReturnBegin: true}
	lessM24 = &Mode{Illegal: "[<='$\"]", ReturnEnd: true, ReturnBegin: true, HasRelevance: true, Relevance: 0}
	lessM25 = &Mode{Illegal: "[<='$\"]", Begin: "[\\.#:&\\[>]", End: "[;{}]", ReturnEnd: true, ReturnBegin: true}
	lessM26 = &Mode{BeginKeywords: "when", EndsWithParent: true}
	lessM27 = &Mode{BeginKeywords: "and not"}
	lessM28 = &Mode{ClassName: "keyword", Begin: "all\\b"}
	lessM29 = &Mode{ClassName: "variable", Begin: "@\\{[\\w-]+\\}"}
	lessM30 = &Mode{ClassName: "selector-tag", Begin: "\\b(a|abbr|address|article|aside|audio|b|blockquote|body|button|canvas|caption|cite|code|dd|del|details|dfn|div|dl|dt|em|fieldset|figcaption|figure|footer|form|h1|h2|h3|h4|h5|h6|header|hgroup|html|i|iframe|img|input|ins|kbd|label|legend|li|main|mark|menu|nav|object|ol|p|q|quote|samp|section|span|strong|summary|sup|table|tbody|td|textarea|tfoot|th|thead|time|tr|ul|var|video)\\b"}
	lessM31 = &Mode{ClassName: "selector-tag", Begin: "([\\w-]+|@\\{[\\w-]+\\})%?", HasRelevance: true, Relevance: 0}
	lessM32 = &Mode{ClassName: "selector-id", Begin: "#([\\w-]+|@\\{[\\w-]+\\})"}
	lessM33 = &Mode{ClassName: "selector-class", Begin: "\\.([\\w-]+|@\\{[\\w-]+\\})", HasRelevance: true, Relevance: 0}
	lessM34 = &Mode{ClassName: "selector-tag", Begin: "&", HasRelevance: true, Relevance: 0}
	lessM35 = &Mode{ClassName: "selector-attr", Illegal: "$", Begin: "\\[", End: "\\]"}
	lessM36 = &Mode{ClassName: "string", Illegal: "\\n", Begin: "'", End: "'"}
	lessM37 = &Mode{Begin: "\\\\[\\s\\S]", HasRelevance: true, Relevance: 0}
	lessM38 = &Mode{ClassName: "string", Illegal: "\\n", Begin: "\"", End: "\""}
	lessM39 = &Mode{ClassName: "selector-pseudo", Begin: ":(active|any-link|blank|checked|current|default|defined|dir|disabled|drop|empty|enabled|first|first-child|first-of-type|fullscreen|future|focus|focus-visible|focus-within|has|host|host-context|hover|indeterminate|in-range|invalid|is|lang|last-child|last-of-type|left|link|local-link|not|nth-child|nth-col|nth-last-child|nth-last-col|nth-last-of-type|nth-of-type|only-child|only-of-type|optional|out-of-range|past|placeholder-shown|read-only|read-write|required|right|root|scope|target|target-within|user-invalid|valid|visited|where)"}
	lessM40 = &Mode{ClassName: "selector-pseudo", Begin: "::(after|backdrop|before|cue|cue-region|first-letter|first-line|grammar-error|marker|part|placeholder|selection|slotted|spelling-error)"}
	lessM41 = &Mode{Begin: "\\(", End: "\\)"}
	lessM42 = &Mode{Begin: "!important"}
	lessM43 = &Mode{Illegal: "[<='$\"]", Begin: "([\\w-]+|@\\{[\\w-]+\\})", End: "\\{", ReturnEnd: true, ReturnBegin: true}
	lessM44 = &Mode{Begin: "([\\w-]+|@\\{[\\w-]+\\})\\s*:", End: "[;}]", ReturnBegin: true, HasRelevance: true, Relevance: 0}
	lessM45 = &Mode{Begin: "-(webkit|moz|ms|o)-"}
	lessM46 = &Mode{ClassName: "attribute", Begin: "\\b(z-index|word-wrap|word-spacing|word-break|width|widows|white-space|visibility|vertical-align|unicode-bidi|transition-timing-function|transition-property|transition-duration|transition-delay|transition|transform-style|transform-origin|transform|top|text-underline-position|text-transform|text-shadow|text-rendering|text-overflow|text-indent|text-decoration-style|text-decoration-line|text-decoration-color|text-decoration|text-align-last|text-align|table-layout|tab-size|src|right|resize|quotes|position|pointer-events|perspective-origin|perspective|page-break-inside|page-break-before|page-break-after|padding-top|padding-right|padding-left|padding-bottom|padding|overflow-y|overflow-x|overflow-wrap|overflow|outline-width|outline-style|outline-offset|outline-color|outline|orphans|order|opacity|object-position|object-fit|normal|none|nav-up|nav-right|nav-left|nav-index|nav-down|min-width|min-height|max-width|max-height|mask|marks|margin-top|margin-right|margin-left|margin-bottom|margin|list-style-type|list-style-position|list-style-image|list-style|line-height|letter-spacing|left|justify-content|initial|inherit|ime-mode|image-resolution|image-rendering|image-orientation|icon|hyphens|height|font-weight|font-variation-settings|font-variant-ligatures|font-variant|font-style|font-stretch|font-smoothing|font-size-adjust|font-size|font-language-override|font-kerning|font-feature-settings|font-family|font-display|font|float|flex-wrap|flex-shrink|flex-grow|flex-flow|flex-direction|flex-basis|flex|filter|empty-cells|display|direction|cursor|counter-reset|counter-increment|content|columns|column-width|column-span|column-rule-width|column-rule-style|column-rule-color|column-rule|column-gap|column-fill|column-count|color|clip-path|clip|clear|caption-side|break-inside|break-before|break-after|box-sizing|box-shadow|box-decoration-break|bottom|border-width|border-top-width|border-top-style|border-top-right-radius|border-top-left-radius|border-top-color|border-top|border-style|border-spacing|border-right-width|border-right-style|border-right-color|border-right|border-radius|border-left-width|border-left-style|border-left-color|border-left|border-image-width|border-image-source|border-image-slice|border-image-repeat|border-image-outset|border-image|border-color|border-collapse|border-bottom-width|border-bottom-style|border-bottom-right-radius|border-bottom-left-radius|border-bottom-color|border-bottom|border|background-size|background-repeat|background-position|background-origin|background-image|background-color|background-clip|background-attachment|background|backface-visibility|auto|animation-timing-function|animation-play-state|animation-name|animation-iteration-count|animation-fill-mode|animation-duration|animation-direction|animation-delay|animation|align-self|align-items|align-content)\\b", End: ":", ExcludeEnd: true}
	lessM47 = &Mode{Illegal: "[<=$]", EndsWithParent: true, HasRelevance: true, Relevance: 0}
	lessM48 = &Mode{ClassName: "variable", Begin: "@[\\w-]+"}
	lessM0.Contains = []*Mode{lessM1, lessM2}
	lessM3.Contains = []*Mode{lessM1, lessM4}
	lessM5.Starts = lessM6
	lessM6.Contains = []*Mode{lessM0, lessM3, lessM7, lessM8, lessM9, lessM10, lessM12, lessM13, lessM14, lessM15, lessM16, lessM17, lessM18}
	lessM10.Starts = lessM11
	lessM13.Contains = []*Mode{lessM0, lessM3, lessM7, lessM8, lessM9, lessM10, lessM12, lessM13, lessM14, lessM15, lessM16, lessM17, lessM18}
	lessM19.Variants = []*Mode{lessM20, lessM48}
	lessM19.Starts = lessM21
	lessM20.Starts = lessM21
	lessM21.Contains = []*Mode{lessM0, lessM3, lessM7, lessM8, lessM9, lessM10, lessM12, lessM13, lessM14, lessM15, lessM16, lessM17, lessM18, lessM22}
	lessM22.Contains = []*Mode{lessM0, lessM3, lessM5, lessM19, lessM23, lessM44, lessM24}
	lessM23.Contains = []*Mode{lessM24}
	lessM24.Variants = []*Mode{lessM25, lessM43}
	lessM24.Contains = []*Mode{lessM0, lessM3, lessM26, lessM28, lessM29, lessM30, lessM31, lessM32, lessM33, lessM34, lessM35, lessM39, lessM40, lessM41, lessM42}
	lessM25.Contains = []*Mode{lessM0, lessM3, lessM26, lessM28, lessM29, lessM30, lessM31, lessM32, lessM33, lessM34, lessM35, lessM39, lessM40, lessM41, lessM42}
	lessM26.Contains = []*Mode{lessM27, lessM0, lessM3, lessM7, lessM8, lessM9, lessM10, lessM12, lessM13, lessM14, lessM15, lessM16, lessM17, lessM18}
	lessM35.Contains = []*Mode{lessM36, lessM38}
	lessM36.Contains = []*Mode{lessM37}
	lessM38.Contains = []*Mode{lessM37}
	lessM41.Contains = []*Mode{lessM0, lessM3, lessM7, lessM8, lessM9, lessM10, lessM12, lessM13, lessM14, lessM15, lessM16, lessM17, lessM18, lessM22}
	lessM43.Contains = []*Mode{lessM0, lessM3, lessM26, lessM28, lessM29, lessM30, lessM31, lessM32, lessM33, lessM34, lessM35, lessM39, lessM40, lessM41, lessM42}
	lessM44.Contains = []*Mode{lessM45, lessM46}
	lessM46.Starts = lessM47
	lessM47.Contains = []*Mode{lessM0, lessM3, lessM7, lessM8, lessM9, lessM10, lessM12, lessM13, lessM14, lessM15, lessM16, lessM17, lessM18}
	lessM48.Starts = lessM21
	RegisterLanguage("less", &Language{
		Name:            "Less",
		Aliases:         []string{},
		CaseInsensitive: true,
		Illegal:         "[=>'/<($\"]", Contains: []*Mode{lessM0, lessM3, lessM5, lessM19, lessM23, lessM44, lessM24},
	})
}
