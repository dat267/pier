package highlight

func init() { RegisterLanguage("objectivec", ObjectiveC()) }

// ObjectiveC returns the Objective-C language definition (a port of
// languages/objectivec.js).
func ObjectiveC() *Language {
	apiClass := &Mode{
		ClassName: "built_in",
		Begin:     `\b(AV|CA|CF|CG|CI|CL|CM|CN|CT|MK|MP|MTK|MTL|NS|SCN|SK|UI|WK|XC)\w+`,
	}

	classKeywords := Keywords{
		"$pattern": `[a-zA-Z@][a-zA-Z0-9_]*`,
		"keyword":  "@interface @class @protocol @implementation",
	}

	atString := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			{
				ClassName: "string",
				Begin:     `@"`,
				End:       `"`,
				Illegal:   `\n`,
				Contains:  []*Mode{BackslashEscape},
			},
		},
	}

	preprocessor := &Mode{
		ClassName: "meta",
		Begin:     `#\s*[a-z]+\b`,
		End:       `$`,
		Keywords: Keywords{
			"meta-keyword": "if else elif endif define undef warning error line pragma ifdef ifndef include",
		},
		Contains: []*Mode{
			{Begin: `\\\n`, HasRelevance: true, Relevance: 0},
			Variant(QuoteStringMode, func(m *Mode) { m.ClassName = "meta-string" }),
			{ClassName: "meta-string", Begin: `<.*?>`, End: `$`, Illegal: `\n`},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}

	classMode := &Mode{
		ClassName:  "class",
		Begin:      `(@interface|@class|@protocol|@implementation)\b`,
		End:        `(\{|$)`,
		ExcludeEnd: true,
		Keywords:   classKeywords,
		Contains:   []*Mode{UnderscoreTitleMode},
	}

	dotIdent := &Mode{Begin: `\.` + UnderscoreIdentRe, HasRelevance: true, Relevance: 0}

	return &Language{
		Name:    "Objective-C",
		Aliases: []string{"mm", "objc", "obj-c", "obj-c++", "objective-c++"},
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "[a-zA-Z@][a-zA-Z0-9_]*",
				"keyword": "int float while char export sizeof typedef const struct for union unsigned long volatile static bool " +
					"mutable if do return goto void enum else break extern asm case short default double register " +
					"explicit signed typename this switch continue wchar_t inline readonly assign readwrite self " +
					"@synchronized id typeof nonatomic super unichar IBOutlet IBAction strong weak copy in out inout " +
					"bycopy byref oneway __strong __weak __block __autoreleasing @private @protected @public @try " +
					"@property @end @throw @catch @finally @autoreleasepool @synthesize @dynamic @selector @optional " +
					"@required @encode @package @import @defs @compatibility_alias __bridge __bridge_transfer " +
					"__bridge_retained __bridge_retain __covariant __contravariant __kindof _Nonnull _Nullable " +
					"_Null_unspecified __FUNCTION__ __PRETTY_FUNCTION__ __attribute__ getter setter retain " +
					"unsafe_unretained nonnull nullable null_unspecified null_resettable class instancetype " +
					"NS_DESIGNATED_INITIALIZER NS_UNAVAILABLE NS_REQUIRES_SUPER NS_RETURNS_INNER_POINTER NS_INLINE " +
					"NS_AVAILABLE NS_DEPRECATED NS_ENUM NS_OPTIONS NS_SWIFT_UNAVAILABLE NS_ASSUME_NONNULL_BEGIN " +
					"NS_ASSUME_NONNULL_END NS_REFINED_FOR_SWIFT NS_SWIFT_NAME NS_SWIFT_NOTHROW NS_DURING NS_HANDLER " +
					"NS_ENDHANDLER NS_VALUERETURN NS_VOIDRETURN",
				"literal":  "false true FALSE TRUE nil YES NO NULL",
				"built_in": "BOOL dispatch_once_t dispatch_queue_t dispatch_sync dispatch_async dispatch_once",
			},
			Illegal: `</`,
			Contains: []*Mode{
				apiClass,
				CLineCommentMode,
				CBlockCommentMode,
				CNumberMode,
				QuoteStringMode,
				AposStringMode,
				atString,
				preprocessor,
				classMode,
				dotIdent,
			},
		},
	}
}
