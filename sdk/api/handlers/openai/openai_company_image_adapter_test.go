package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestBuildCompanyResponsesImageRequestGeneration(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-image-2.5-sunburst",
		"instructions":"Create a clean icon",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"for an internal AI gateway"}]}],
		"stream":true,
		"size":"1024x1024",
		"tools":[{"type":"image_generation","quality":"high"}]
	}`)

	request, imageJSON, err := buildCompanyResponsesImageRequest(raw)
	if err != nil {
		t.Fatalf("buildCompanyResponsesImageRequest() error = %v", err)
	}
	if request.Path != companyImageGenerationsPath || !request.Stream {
		t.Fatalf("request routing = (%q, %v), want (%q, true)", request.Path, request.Stream, companyImageGenerationsPath)
	}
	if request.Prompt != "Create a clean icon\n\nfor an internal AI gateway" {
		t.Fatalf("prompt = %q", request.Prompt)
	}
	if got := gjson.GetBytes(imageJSON, "model").String(); got != request.Model {
		t.Fatalf("image model = %q, want %q", got, request.Model)
	}
	if got := gjson.GetBytes(imageJSON, "prompt").String(); got != request.Prompt {
		t.Fatalf("image prompt = %q, want %q", got, request.Prompt)
	}
	if got := gjson.GetBytes(imageJSON, "size").String(); got != "1024x1024" {
		t.Fatalf("size = %q, want 1024x1024", got)
	}
	if got := gjson.GetBytes(imageJSON, "quality").String(); got != "high" {
		t.Fatalf("tool quality = %q, want high", got)
	}
	if gjson.GetBytes(imageJSON, "input").Exists() || gjson.GetBytes(imageJSON, "tools").Exists() {
		t.Fatal("Responses-only fields leaked into image request")
	}
}

func TestBuildCompanyResponsesImageRequestEdit(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-image-2.5-sunburst",
		"input":[{"type":"message","content":[
			{"type":"input_text","text":"replace the background"},
			{"type":"input_image","image_url":"data:image/png;base64,AA=="}
		]}]
	}`)

	request, imageJSON, err := buildCompanyResponsesImageRequest(raw)
	if err != nil {
		t.Fatalf("buildCompanyResponsesImageRequest() error = %v", err)
	}
	if request.Path != companyImageEditsPath {
		t.Fatalf("path = %q, want %q", request.Path, companyImageEditsPath)
	}
	if got := gjson.GetBytes(imageJSON, "images.0.image_url").String(); got != "data:image/png;base64,AA==" {
		t.Fatalf("image URL = %q", got)
	}
}

func TestBuildCompanyResponsesImageRequestUsesLatestUserPrompt(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-image-2",
		"instructions":"long system instructions should not become the image prompt",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"old developer context"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"old assistant response"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"draw only this latest request"}]}
		]
	}`)

	request, imageJSON, err := buildCompanyResponsesImageRequest(raw)
	if err != nil {
		t.Fatalf("buildCompanyResponsesImageRequest() error = %v", err)
	}
	if request.Prompt != "draw only this latest request" {
		t.Fatalf("prompt = %q", request.Prompt)
	}
	if got := gjson.GetBytes(imageJSON, "prompt").String(); got != request.Prompt {
		t.Fatalf("image prompt = %q, want %q", got, request.Prompt)
	}
}

func TestCompanyResponsesImageAdapterRequiresCompanyVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-image-2.5-sunburst","input":"draw"}`))

	if isCompanyResponsesImageRequest(c, []byte(`{"model":"gpt-image-2.5-sunburst","input":"draw"}`)) {
		t.Fatal("unverified request should not use the company adapter")
	}
	c.Set("company.gateway.verified", true)
	if !isCompanyResponsesImageRequest(c, []byte(`{"model":"gpt-image-2.5-sunburst","input":"draw"}`)) {
		t.Fatal("verified image request should use the company adapter")
	}
	if isCompanyResponsesImageRequest(c, []byte(`{"model":"gpt-5.4-mini","input":"draw"}`)) {
		t.Fatal("text model should not use the company adapter")
	}
}

func TestCompanyResponsesImageAdapterReadsImageModelFromTool(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.4-mini",
		"input":"draw a small orange tree",
		"tools":[{"type":"image_generation","model":"gpt-image-2"}]
	}`)

	model, ok := companyResponsesImageModel(raw)
	if !ok || model != "gpt-image-2" {
		t.Fatalf("image model = %q, ok=%t; want gpt-image-2", model, ok)
	}
	_, imageJSON, err := buildCompanyResponsesImageRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(imageJSON, "model").String(); got != "gpt-image-2" {
		t.Fatalf("upstream image model = %q, want gpt-image-2", got)
	}
}

func TestCompanyResponsesImageAdapterDefaultsToolImageModel(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.4-mini",
		"input":"draw a small orange tree",
		"tools":[{"type":"image_generation"}]
	}`)
	model, ok := companyResponsesImageModel(raw)
	if !ok || model != defaultImagesToolModel {
		t.Fatalf("image model = %q, ok=%t; want %q", model, ok, defaultImagesToolModel)
	}
}

func TestBuildCompanyResponsesImageResponse(t *testing.T) {
	payload, err := buildCompanyResponsesImageResponse([]byte(`{
		"created":1720000000,
		"data":[{"b64_json":"aW1hZ2U=","revised_prompt":"a square icon"}],
		"usage":{"input_tokens":3,"output_tokens":7,"total_tokens":10}
	}`), "gpt-image-2.5-sunburst")
	if err != nil {
		t.Fatalf("buildCompanyResponsesImageResponse() error = %v", err)
	}
	if got := gjson.GetBytes(payload, "object").String(); got != "response" {
		t.Fatalf("object = %q, want response", got)
	}
	if got := gjson.GetBytes(payload, "status").String(); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}
	if got := gjson.GetBytes(payload, "output.0.type").String(); got != "image_generation_call" {
		t.Fatalf("output type = %q", got)
	}
	if got := gjson.GetBytes(payload, "output.0.result").String(); got != "aW1hZ2U=" {
		t.Fatalf("result = %q", got)
	}
	if got := gjson.GetBytes(payload, "usage.total_tokens").Int(); got != 10 {
		t.Fatalf("usage.total_tokens = %d, want 10", got)
	}
}

func TestCompanyResponsesImageResponseSupportsDataURL(t *testing.T) {
	payload, err := buildCompanyResponsesImageResponse([]byte(`{
		"data":[{"url":"data:image/png;base64,AA=="}]
	}`), "gpt-image-2.5-sunburst")
	if err != nil {
		t.Fatalf("buildCompanyResponsesImageResponse() error = %v", err)
	}
	if got := gjson.GetBytes(payload, "output.0.result").String(); got != "AA==" {
		t.Fatalf("data URL result = %q, want AA==", got)
	}
}

func TestBuildCompanyResponsesImageStreamEvents(t *testing.T) {
	response := []byte(`{
  "id":"resp-1",
  "status":"completed",
  "output":[{"type":"image_generation_call","status":"completed","result":"aW1hZ2U="}],
  "usage": {
    "total_tokens": 1
  }
}`)
	events, err := buildCompanyResponsesImageStreamEvents(response, 4)
	if err != nil {
		t.Fatalf("buildCompanyResponsesImageStreamEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[0].Type != "response.output_item.added" || events[1].Type != "response.output_item.done" || events[2].Type != "response.completed" {
		t.Fatalf("event types = %q, %q, %q", events[0].Type, events[1].Type, events[2].Type)
	}
	if got := gjson.GetBytes(events[0].Payload, "sequence_number").Int(); got != 4 {
		t.Fatalf("added sequence_number = %d, want 4", got)
	}
	if got := gjson.GetBytes(events[0].Payload, "item.status").String(); got != "in_progress" {
		t.Fatalf("added status = %q, want in_progress", got)
	}
	if gjson.GetBytes(events[0].Payload, "item.result").Exists() {
		t.Fatal("added image item should not contain the completed result")
	}
	if got := gjson.GetBytes(events[1].Payload, "item.result").String(); got != "aW1hZ2U=" {
		t.Fatalf("done result = %q", got)
	}
	if got := gjson.GetBytes(events[2].Payload, "response.output.0.id").String(); got == "" {
		t.Fatal("completed response output item has no id")
	}
	if got := gjson.GetBytes(events[2].Payload, "sequence_number").Int(); got != 6 {
		t.Fatalf("completed sequence_number = %d, want 6", got)
	}
	if strings.Contains(string(events[2].Payload), "\n") {
		t.Fatal("completed image event must contain one compact JSON line")
	}
}
