package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// TestImageResizeOptionsForModel covers upstream's spread of a model's resize
// block over the defaults: missing keys keep their default rather than becoming
// zero, which would resize to nothing.
func TestImageResizeOptionsForModel(t *testing.T) {
	cases := []struct {
		name  string
		model *ai.Model
		want  ImageResizeOptions
	}{
		{name: "no model", model: nil, want: DefaultImageResizeOptions},
		{name: "no limits", model: &ai.Model{Input: []string{"image"}}, want: DefaultImageResizeOptions},
		{
			name:  "limits without resize",
			model: &ai.Model{InputLimits: &ai.ModelInputLimits{Images: &ai.ModelImageLimits{MaxPerMessage: 4}}},
			want:  DefaultImageResizeOptions,
		},
		{
			name: "partial block keeps defaults",
			model: &ai.Model{InputLimits: &ai.ModelInputLimits{Images: &ai.ModelImageLimits{
				Resize: &ai.ModelImageResize{MaxBytes: 1_000_000},
			}}},
			want: ImageResizeOptions{MaxWidth: 2000, MaxHeight: 2000, MaxBytes: 1_000_000, JPEGQuality: 80},
		},
		{
			name: "full block overrides",
			model: &ai.Model{InputLimits: &ai.ModelInputLimits{Images: &ai.ModelImageLimits{
				Resize: &ai.ModelImageResize{MaxWidth: 1024, MaxHeight: 768, MaxBytes: 2_000_000, JPEGQuality: 70},
			}}},
			want: ImageResizeOptions{MaxWidth: 1024, MaxHeight: 768, MaxBytes: 2_000_000, JPEGQuality: 70},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := imageResizeOptionsFor(tc.model); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestImageResizeOptionsIgnoresExplicitZeros pins D474: a resize field set to 0
// keeps the default. Reference: pi v1.5.2 (b4afdddf)
// utils/image-resize-core.ts spreads `{ ...DEFAULT_OPTIONS, ...options }`, so an
// explicit 0 there wins and a 0 width or quality degrades the resize; the port's
// `ModelImageResize` fields are plain integers, where 0 reads as unset, and the
// default is the useful answer for both.
func TestImageResizeOptionsIgnoresExplicitZeros(t *testing.T) {
	model := &ai.Model{Input: []string{"image"}, InputLimits: &ai.ModelInputLimits{
		Images: &ai.ModelImageLimits{Resize: &ai.ModelImageResize{MaxWidth: 0, MaxHeight: 0, MaxBytes: 0, JPEGQuality: 0}},
	}}
	if got, want := imageResizeOptionsFor(model), DefaultImageResizeOptions; got != want {
		t.Errorf("all-zero block: got %+v, want %+v", got, want)
	}
	model.InputLimits.Images.Resize.JPEGQuality = 60
	want := DefaultImageResizeOptions
	want.JPEGQuality = 60
	if got := imageResizeOptionsFor(model); got != want {
		t.Errorf("explicit quality with zero sizes: got %+v, want %+v", got, want)
	}
}

// TestNonVisionImageNote pins upstream getNonVisionImageNote: the note appears
// only when the model's input modes exclude "image".
func TestNonVisionImageNote(t *testing.T) {
	if note := nonVisionImageNote(nil); note != "" {
		t.Errorf("nil model: %q", note)
	}
	if note := nonVisionImageNote(&ai.Model{Input: []string{"text", "image"}}); note != "" {
		t.Errorf("vision model: %q", note)
	}
	if note := nonVisionImageNote(&ai.Model{Input: []string{"text"}}); note != "[Current model does not support images. The image will be omitted from this request.]" {
		t.Errorf("text-only model: %q", note)
	}
	if note := nonVisionImageNote(&ai.Model{}); note != "[Current model does not support images. The image will be omitted from this request.]" {
		t.Errorf("empty input list: %q", note)
	}
}
