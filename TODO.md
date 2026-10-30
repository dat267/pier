# Reproduced audit findings

- CR-230: `ai/providers.OpenAIProvider` omits the pinned OpenAI Decisions
  classifier model (`openai-decisions`, `gpt-6-luna`) and classifier API surface;
  upstream `pi/packages/ai/src/providers/openai.ts` and `api/openai-decisions.ts`
  expose both, while Pier has no classifier implementation contract.
