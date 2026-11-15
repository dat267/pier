# Reproduced, unresolved findings

No open findings. The WebP omission was fixed by registering the
`golang.org/x/image/webp` decoder in `coding/fileprocessor.go`, which
`TestProcessImageEmbedsASmallWebP`, `TestProcessImageResizesALargeWebP`, and
`TestEncodeImageAsPNGTranscodesWebP` pin against pi v1.5.2 `b4afdddf`
`utils/image-process.ts` and `EnsurePngTranscoder`.
