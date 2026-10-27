package highlight

func init() { RegisterLanguage("matlab", Matlab()) }

// Matlab returns the Matlab language definition (a port of languages/matlab.js).
func Matlab() *Language {
	const transposeRe = `('|\.')+`
	transpose := &Mode{
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{{Begin: transposeRe}},
	}

	params := &Mode{ClassName: "params"}
	params.Variants = []*Mode{
		Variant(params, func(m *Mode) { m.Begin = `\(`; m.End = `\)` }),
		Variant(params, func(m *Mode) { m.Begin = `\[`; m.End = `\]` }),
	}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "function",
		End:           `$`,
		Contains:      []*Mode{UnderscoreTitleMode, params},
	}

	builtIn := &Mode{ClassName: "built_in", Begin: `true|false`, HasRelevance: true, Relevance: 0, Starts: transpose}

	transposeIdent := &Mode{Begin: `[a-zA-Z][a-zA-Z_0-9]*` + transposeRe, HasRelevance: true, Relevance: 0}

	number := &Mode{ClassName: "number", Begin: CNumberRe, HasRelevance: true, Relevance: 0, Starts: transpose}

	singleString := &Mode{
		ClassName: "string",
		Begin:     `'`,
		End:       `'`,
		Contains:  []*Mode{BackslashEscape, {Begin: `''`}},
	}

	closeBracket := &Mode{Begin: `\]|\}|\)`, HasRelevance: true, Relevance: 0, Starts: transpose}

	doubleString := &Mode{
		ClassName: "string",
		Begin:     `"`,
		End:       `"`,
		Contains:  []*Mode{BackslashEscape, {Begin: `""`}},
		Starts:    transpose,
	}

	return &Language{
		Name: "Matlab",
		Keywords: Keywords{
			"keyword": "arguments break case catch classdef continue else elseif end enumeration events for function global " +
				"if methods otherwise parfor persistent properties return spmd switch try while",
			"built_in": "sin sind sinh asin asind asinh cos cosd cosh acos acosd acosh tan tand tanh atan atand atan2 atanh " +
				"sec secd sech asec asecd asech csc cscd csch acsc acscd acsch cot cotd coth acot acotd acoth hypot " +
				"exp expm1 log log1p log10 log2 pow2 realpow reallog realsqrt sqrt nthroot nextpow2 abs angle complex " +
				"conj imag real unwrap isreal cplxpair fix floor ceil round mod rem sign airy besselj bessely besselh " +
				"besseli besselk beta betainc betaln ellipj ellipke erf erfc erfcx erfinv expint gamma gammainc " +
				"gammaln psi legendre cross dot factor isprime primes gcd lcm rat rats perms nchoosek factorial " +
				"cart2sph cart2pol pol2cart sph2cart hsv2rgb rgb2hsv zeros ones eye repmat rand randn linspace " +
				"logspace freqspace meshgrid accumarray size length ndims numel disp isempty isequal " +
				"isequalwithequalnans cat reshape diag blkdiag tril triu fliplr flipud flipdim rot90 find sub2ind " +
				"ind2sub bsxfun ndgrid permute ipermute shiftdim circshift squeeze isscalar isvector ans eps realmax " +
				"realmin pi i|0 inf nan isnan isinf isfinite j|0 why compan gallery hadamard hankel hilb invhilb " +
				"magic pascal rosser toeplitz vander wilkinson max min nanmax nanmin mean nanmean type table " +
				"readtable writetable sortrows sort figure plot plot3 scatter scatter3 cellfun legend intersect " +
				"ismember procrustes hold num2cell ",
		},
		Illegal: `(//|"|#|/\*|\s+/\w+)`,
		Contains: []*Mode{
			function,
			builtIn,
			transposeIdent,
			number,
			singleString,
			closeBracket,
			doubleString,
			CommentMode(`^\s*%\{\s*$`, `^\s*%\}\s*$`),
			CommentMode(`%`, `$`),
		},
	}
}
