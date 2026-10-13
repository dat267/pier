package durable

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of tools/read.ts's image path at pi v1.2.0: a supported image within the
// model's limits becomes one image block instead of an unsupported_image error.

func (a *fakeToolApi) Agent(chord.Context) (Agent, error) { return a.agent, nil }
func (a *fakeToolApi) Models() *ai.Models                 { return a.models }

// imageModels builds a catalog with one model, for the image-limit cases.
func imageModels(t *testing.T, model *ai.Model) *ai.Models {
	t.Helper()
	models := ai.CreateModels(nil)
	models.SetProvider(ai.CreateProvider(ai.CreateProviderOptions{ID: "p", Models: []*ai.Model{model}}))
	return models
}

// sizedPNG is a PNG whose IHDR declares width x height.
func sizedPNG(width, height int) []byte {
	buffer := pngBuffer(false)
	binary.BigEndian.PutUint32(buffer[16:20], uint32(width))
	binary.BigEndian.PutUint32(buffer[20:24], uint32(height))
	return buffer
}

func TestReadToolReturnsImageBlock(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	png := pngBuffer(false)
	if err := env.WriteFile(filepath.Join(dir, "image.png"), png, ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "image.png"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != nil && *result.IsError {
		t.Fatalf("image was rejected: %+v", result)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v", result.Content)
	}
	image, ok := result.Content[0].(ai.ImageContent)
	if !ok {
		t.Fatalf("content type = %T, want ai.ImageContent", result.Content[0])
	}
	if image.MimeType != "image/png" || image.Data != base64.StdEncoding.EncodeToString(png) {
		t.Fatalf("image = %+v", image)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code == nil || *result.Diagnostics[0].Code != "image" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestReadToolNotesWhenModelHasNoImageInput(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "image.png"), sizedPNG(2, 2), ctx); err != nil {
		t.Fatal(err)
	}
	models := imageModels(t, &ai.Model{ID: "m", Provider: "p", Input: []string{"text"}})
	api := &fakeToolApi{env: env, models: models, agent: Agent{Model: &ModelRef{Provider: "p", ModelID: "m"}}}
	result, err := CreateReadTool().Execute(JsonObject{"path": "image.png"}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "does not support images") {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestReadToolRejectsImageExceedingModelLimits(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "image.png"), sizedPNG(20, 10), ctx); err != nil {
		t.Fatal(err)
	}
	models := imageModels(t, &ai.Model{ID: "m", Provider: "p", Input: []string{"text", "image"},
		InputLimits: &ai.ModelInputLimits{Images: &ai.ModelImageLimits{Resize: &ai.ModelImageResize{MaxWidth: 8, MaxHeight: 8}}}})
	api := &fakeToolApi{env: env, models: models, agent: Agent{Model: &ModelRef{Provider: "p", ModelID: "m"}}}
	result, err := CreateReadTool().Execute(JsonObject{"path": "image.png"}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError == nil || !*result.IsError || len(result.Diagnostics) != 1 ||
		!strings.Contains(result.Diagnostics[0].Message, "larger than 8x8") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReadToolRejectsImageOverByteLimit(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "image.png"), sizedPNG(2, 2), ctx); err != nil {
		t.Fatal(err)
	}
	models := imageModels(t, &ai.Model{ID: "m", Provider: "p", Input: []string{"text", "image"},
		InputLimits: &ai.ModelInputLimits{Images: &ai.ModelImageLimits{Resize: &ai.ModelImageResize{MaxBytes: 1}}}})
	api := &fakeToolApi{env: env, models: models, agent: Agent{Model: &ModelRef{Provider: "p", ModelID: "m"}}}
	result, err := CreateReadTool().Execute(JsonObject{"path": "image.png"}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError == nil || !*result.IsError || len(result.Diagnostics) != 1 ||
		!strings.Contains(result.Diagnostics[0].Message, "too large to send") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReadToolRejectsUnsupportedImageWithoutProcessor(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "image.bmp"), bmpBuffer(), ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "image.bmp"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError == nil || !*result.IsError || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code == nil || *result.Diagnostics[0].Code != "unsupported_image" {
		t.Fatalf("result = %+v", result)
	}
}
