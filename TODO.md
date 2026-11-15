# Reproduced, unresolved findings

## A WebP that already fits is omitted instead of embedded

- Severity: medium. A prompt attachment or tool-result image in WebP never reaches the model, while pi v1.5.2 `b4afdddf` embeds it.
- Failure: `coding.ProcessImage` treats `image/webp` as a supported inline type, but `resizeInlineImage` cannot decode it: Go's standard library has no WebP decoder. Auto-resize is on by default, so a valid small WebP takes the decode-failure path and the prompt carries `[Image omitted: could not be resized below the inline image size limit.]` (`coding/promptimage_test.go` `TestPromptAppendsImageOmissionHint` pins the same message for a broken image).
- Evidence: `ProcessImage([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "image/webp", nil)` returns `ok=false` with that message. Upstream `packages/coding-agent/src/utils/image-process.ts` `normalizeImage` keeps `image/webp` as a supported type and `resizeImageInProcess` decodes it with Photon, so a WebP within `maxWidth`/`maxHeight`/`maxBytes` is returned byte-identically (`wasResized: false`). Only the `@file` CLI path passes, because `cmd/main.go` sets `autoResizeImages: false` there.
- Repair plan: either add `golang.org/x/image/webp` (pure Go, so `CGO_ENABLED=0` still holds) and register it before `decodeInlineImage`, or pass a WebP through untouched when its base64 size is under `MaxBytes` and accept the unverified dimensions. The first keeps D408's canvas bound and the size limit; it needs a dependency decision and a red test with a real WebP fixture.
