package ai

import (
	"regexp"
	"strings"
)

// Port of packages/ai/src/utils/overflow.ts: context-overflow detection.

// compileOverflowPattern translates the ASCII-only /i patterns used upstream.
// JS without /u never folds non-ASCII letters into ASCII (unlike RE2).
func compileOverflowPattern(pattern string) *regexp.Regexp {
	pattern = strings.TrimPrefix(pattern, "(?i)")
	var out strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' && i+1 < len(pattern) {
			i++
			if pattern[i] == 's' {
				out.WriteString(`[\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`)
			} else {
				out.WriteByte(c)
				out.WriteByte(pattern[i])
			}
		} else if c == '.' {
			out.WriteString(`[^\n\r\x{2028}\x{2029}]`)
		} else if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			out.WriteByte('[')
			out.WriteByte(c | 32)
			out.WriteByte(c &^ 32)
			out.WriteByte(']')
		} else {
			out.WriteByte(c)
		}
	}
	return regexp.MustCompile(out.String())
}

// overflowPatterns match error messages returned when the input exceeds the
// model's context window.
var overflowPatterns = []*regexp.Regexp{
	compileOverflowPattern(`(?i)prompt (?:is )?too long`),                                                                     // Anthropic and z.ai token overflow
	compileOverflowPattern(`(?i)prompt exceeds max length`),                                                                   // z.ai CN endpoint token overflow (3dd803d7e)
	compileOverflowPattern(`(?i)request_too_large`),                                                                           // Anthropic request byte-size overflow (HTTP 413)
	compileOverflowPattern(`(?i)input is too long for requested model`),                                                       // Amazon Bedrock
	compileOverflowPattern(`(?i)exceeds the context window`),                                                                  // OpenAI (Completions & Responses API)
	compileOverflowPattern(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length(?: of [0-9,]+ tokens?|\s*\([0-9,]+\))`), // OpenAI-compatible proxies (LiteLLM)
	compileOverflowPattern(`(?i)input token count.*exceeds the maximum`),                                                      // Google (Gemini)
	compileOverflowPattern(`(?i)maximum prompt length is [0-9]+`),                                                             // xAI (Grok)
	compileOverflowPattern(`(?i)reduce the length of the messages`),                                                           // Groq
	compileOverflowPattern(`(?i)maximum context length is [0-9]+ tokens`),                                                     // OpenRouter (most backends)
	compileOverflowPattern(`(?i)exceeds (?:the )?maximum allowed input length of [0-9,]+ tokens?`),                            // OpenRouter/Poolside
	compileOverflowPattern(`(?i)input \([0-9]+ tokens\) is longer than the model'?s context length \([0-9]+ tokens\)`),        // Together AI
	compileOverflowPattern(`(?i)exceeds the limit of [0-9]+`),                                                                 // GitHub Copilot
	compileOverflowPattern(`(?i)exceeds the available context size`),                                                          // llama.cpp server
	compileOverflowPattern(`(?i)greater than the context length`),                                                             // LM Studio
	compileOverflowPattern(`(?i)context window exceeds limit`),                                                                // MiniMax
	compileOverflowPattern(`(?i)exceeded model token limit`),                                                                  // Kimi For Coding
	compileOverflowPattern(`(?i)too large for model with [0-9]+ maximum context length`),                                      // Mistral
	compileOverflowPattern(`(?i)prompt has [0-9,]+ tokens?, but the configured context size is [0-9,]+ tokens?`),              // DS4 server
	compileOverflowPattern(`(?i)model_context_window_exceeded`),                                                               // z.ai non-standard finish_reason surfaced as error text
	compileOverflowPattern(`(?i)prompt too long; exceeded (?:max )?context length`),                                           // Ollama explicit overflow error
	compileOverflowPattern(`(?i)range of input length should be`),                                                             // DashScope / Qwen Token Plan
	compileOverflowPattern(`(?i)context[_ ]length[_ ]exceeded`),                                                               // Generic fallback
	compileOverflowPattern(`(?i)too many tokens`),                                                                             // Generic fallback
	compileOverflowPattern(`(?i)token limit exceeded`),                                                                        // Generic fallback
}

// cerebrasBodylessOverflowPattern matches Cerebras' bodyless 400/413.
var cerebrasBodylessOverflowPattern = compileOverflowPattern(`(?i)^4(?:00|13)\s*(?:status code)?\s*\(no body\)`)

// nonOverflowPatterns indicate non-overflow errors (rate limiting, server
// errors) that must not be treated as overflow even when they also match an
// overflow pattern.
var nonOverflowPatterns = []*regexp.Regexp{
	compileOverflowPattern(`(?i)^(Throttling error|Service unavailable):`), // AWS Bedrock non-overflow errors
	compileOverflowPattern(`(?i)rate limit`),                               // Generic rate limiting
	compileOverflowPattern(`(?i)too many requests`),                        // Generic HTTP 429 style
}

// IsContextOverflow reports whether an assistant message represents a context
// overflow. contextWindow enables the silent-overflow and length-stop cases;
// pass 0 to skip them.
func IsContextOverflow(message *AssistantMessage, contextWindow int64) bool {
	if message == nil {
		return false
	}

	// Case 1: an error message matching an overflow pattern.
	if message.StopReason == StopError && message.ErrorMessage != nil && *message.ErrorMessage != "" {
		text := *message.ErrorMessage
		isNonOverflow := false
		for _, pattern := range nonOverflowPatterns {
			if pattern.MatchString(text) {
				isNonOverflow = true
				break
			}
		}
		if !isNonOverflow {
			for _, pattern := range overflowPatterns {
				if pattern.MatchString(text) {
					return true
				}
			}
			if message.Provider == "cerebras" && cerebrasBodylessOverflowPattern.MatchString(text) {
				return true
			}
		}
	}

	// Case 2: silent overflow (z.ai style): success with usage beyond the window.
	if contextWindow > 0 && message.StopReason == StopStop {
		if message.Usage.Input+message.Usage.CacheRead > contextWindow {
			return true
		}
	}

	// Case 3: length-stop overflow (Xiaomi MiMo style): the server truncates
	// oversized input to fill the window, leaving no room for output.
	if contextWindow > 0 && message.StopReason == StopLength && message.Usage.Output == 0 {
		inputTokens := message.Usage.Input + message.Usage.CacheRead
		if float64(inputTokens) >= float64(contextWindow)*0.99 {
			return true
		}
	}

	return false
}

// IsRecoverableLength reports whether a length stop ended below the intended
// output limit, which permits one bounded compact-and-retry attempt.
// desiredMaxOutput must be the original limit before any context clamping.
func IsRecoverableLength(message *AssistantMessage, desiredMaxOutput int64) bool {
	if message == nil {
		return false
	}
	return message.StopReason == StopLength && desiredMaxOutput > 0 && message.Usage.Output < desiredMaxOutput
}

// GetOverflowPatterns exposes the overflow patterns (for tests).
func GetOverflowPatterns() []*regexp.Regexp {
	return append([]*regexp.Regexp{}, overflowPatterns...)
}
