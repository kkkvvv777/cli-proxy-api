package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	companyImageGenerationsPath = "/v1/images/generations"
	companyImageEditsPath       = "/v1/images/edits"
)

type companyResponsesImageRequest struct {
	Model  string
	Prompt string
	Images []string
	Path   string
	Stream bool
}

type companyResponsesImageResult struct {
	Result        string
	URL           string
	RevisedPrompt string
}

// isCompanyResponsesImageRequest deliberately checks the company request
// marker. The adapter must not change the native Responses behaviour for
// ordinary CLIProxyAPI users.
func isCompanyResponsesImageRequest(c *gin.Context, rawJSON []byte) bool {
	if !handlers.CompanyGatewayVerified(c) {
		return false
	}
	_, ok := companyResponsesImageModel(rawJSON)
	return ok
}

// companyResponsesImageModel accepts both shapes used by Responses clients:
// some put the selected image model at request.model, while others keep the
// main text model there and put the image model on tools[].model.
func companyResponsesImageModel(rawJSON []byte) (string, bool) {
	requestModel := strings.TrimSpace(gjson.GetBytes(rawJSON, "model").String())
	if requestModel != "" && isSupportedImagesModel(thinking.ParseSuffix(requestModel).ModelName) {
		return requestModel, true
	}

	for _, tool := range gjson.GetBytes(rawJSON, "tools").Array() {
		if !strings.EqualFold(strings.TrimSpace(tool.Get("type").String()), "image_generation") {
			continue
		}
		toolModel := strings.TrimSpace(tool.Get("model").String())
		if toolModel == "" {
			toolModel = defaultImagesToolModel
		}
		if isSupportedImagesModel(thinking.ParseSuffix(toolModel).ModelName) {
			return toolModel, true
		}
	}
	return "", false
}

func buildCompanyResponsesImageRequest(rawJSON []byte) (companyResponsesImageRequest, []byte, error) {
	model, ok := companyResponsesImageModel(rawJSON)
	if !ok {
		return companyResponsesImageRequest{}, nil, fmt.Errorf("image model is required")
	}

	prompt := extractCompanyResponsesImagePrompt(rawJSON)
	if prompt == "" {
		return companyResponsesImageRequest{}, nil, fmt.Errorf("image prompt is required")
	}
	images := extractCompanyResponsesImageInputs(rawJSON)
	path := companyImageGenerationsPath
	if len(images) > 0 {
		path = companyImageEditsPath
	}

	imageRequest := []byte(`{"model":"","prompt":"","response_format":"b64_json"}`)
	imageRequest, _ = sjson.SetBytes(imageRequest, "model", model)
	imageRequest, _ = sjson.SetBytes(imageRequest, "prompt", prompt)
	for _, field := range []string{
		"size", "quality", "background", "output_format", "input_fidelity", "moderation",
		"output_compression", "partial_images", "n",
	} {
		imageRequest = copyCompanyImageOption(imageRequest, rawJSON, field)
	}

	// Some clients put image-generation options on the tool instead of the
	// request root. Root fields win, matching the usual API precedence.
	var imageTool gjson.Result
	for _, tool := range gjson.GetBytes(rawJSON, "tools").Array() {
		if strings.EqualFold(strings.TrimSpace(tool.Get("type").String()), "image_generation") {
			imageTool = tool
			if strings.EqualFold(strings.TrimSpace(tool.Get("action").String()), "edit") {
				path = companyImageEditsPath
			}
			break
		}
	}
	for _, field := range []string{
		"size", "quality", "background", "output_format", "input_fidelity", "moderation",
		"output_compression", "partial_images", "n",
	} {
		if !gjson.GetBytes(rawJSON, field).Exists() {
			imageRequest = copyCompanyImageOptionFrom(imageRequest, imageTool.Get(field), field)
		}
	}

	if len(images) > 0 {
		items := make([][]byte, 0, len(images))
		for _, image := range images {
			item := []byte(`{"image_url":""}`)
			item, _ = sjson.SetBytes(item, "image_url", image)
			items = append(items, item)
		}
		imageRequest, _ = sjson.SetRawBytes(imageRequest, "images", joinCompanyImageItems(items))
	}

	return companyResponsesImageRequest{
		Model:  model,
		Prompt: prompt,
		Images: images,
		Path:   path,
		Stream: gjson.GetBytes(rawJSON, "stream").Bool(),
	}, imageRequest, nil
}

func copyCompanyImageOption(dst, rawJSON []byte, field string) []byte {
	return copyCompanyImageOptionFrom(dst, gjson.GetBytes(rawJSON, field), field)
}

func copyCompanyImageOptionFrom(dst []byte, value gjson.Result, field string) []byte {
	if !value.Exists() || !json.Valid([]byte(value.Raw)) {
		return dst
	}
	updated, err := sjson.SetRawBytes(dst, field, []byte(value.Raw))
	if err != nil {
		return dst
	}
	return updated
}

func joinCompanyImageItems(items [][]byte) []byte {
	if len(items) == 0 {
		return []byte("[]")
	}
	joined := []byte("[")
	for index, item := range items {
		if index > 0 {
			joined = append(joined, ',')
		}
		joined = append(joined, item...)
	}
	return append(joined, ']')
}

func extractCompanyResponsesImagePrompt(rawJSON []byte) string {
	instructions := strings.TrimSpace(gjson.GetBytes(rawJSON, "instructions").String())
	input := gjson.GetBytes(rawJSON, "input")
	if prompt := latestCompanyResponsesUserPrompt(input); prompt != "" {
		if companyResponsesInputHasHistory(input) {
			return prompt
		}
		return joinCompanyResponsesPromptParts(instructions, prompt)
	}

	parts := make([]string, 0, 2)
	if instructions != "" {
		parts = append(parts, instructions)
	}
	appendCompanyResponseText(&parts, input)
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func latestCompanyResponsesUserPrompt(input gjson.Result) string {
	latest := ""
	var walk func(gjson.Result)
	walk = func(node gjson.Result) {
		if !node.Exists() {
			return
		}
		if node.IsArray() {
			for _, item := range node.Array() {
				walk(item)
			}
			return
		}
		if !strings.EqualFold(strings.TrimSpace(node.Get("role").String()), "user") {
			if content := node.Get("content"); content.Exists() {
				walk(content)
			}
			return
		}
		parts := make([]string, 0, 2)
		appendCompanyResponseText(&parts, node.Get("content"))
		if len(parts) == 0 {
			appendCompanyResponseText(&parts, node.Get("text"))
		}
		latest = strings.TrimSpace(strings.Join(parts, "\n\n"))
	}
	walk(input)
	return latest
}

func companyResponsesInputHasHistory(input gjson.Result) bool {
	messageCount := 0
	hasNonUserMessage := false
	var walk func(gjson.Result)
	walk = func(node gjson.Result) {
		if !node.Exists() {
			return
		}
		if node.IsArray() {
			for _, item := range node.Array() {
				walk(item)
			}
			return
		}
		if role := strings.TrimSpace(node.Get("role").String()); role != "" {
			messageCount++
			if !strings.EqualFold(role, "user") {
				hasNonUserMessage = true
			}
			return
		}
		if content := node.Get("content"); content.Exists() {
			walk(content)
		}
	}
	walk(input)
	return hasNonUserMessage || messageCount > 1
}

func joinCompanyResponsesPromptParts(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			filtered = append(filtered, value)
		}
	}
	return strings.TrimSpace(strings.Join(filtered, "\n\n"))
}

func appendCompanyResponseText(parts *[]string, node gjson.Result) {
	if !node.Exists() || parts == nil {
		return
	}
	if node.Type == gjson.String {
		if value := strings.TrimSpace(node.String()); value != "" {
			*parts = append(*parts, value)
		}
		return
	}
	if node.IsArray() {
		for _, item := range node.Array() {
			appendCompanyResponseText(parts, item)
		}
		return
	}
	if text := strings.TrimSpace(node.Get("text").String()); text != "" {
		*parts = append(*parts, text)
	}
	if content := node.Get("content"); content.Exists() {
		appendCompanyResponseText(parts, content)
	}
}

func extractCompanyResponsesImageInputs(rawJSON []byte) []string {
	images := make([]string, 0)
	var walk func(gjson.Result)
	walk = func(node gjson.Result) {
		if !node.Exists() {
			return
		}
		if node.IsArray() {
			for _, item := range node.Array() {
				walk(item)
			}
			return
		}
		if strings.EqualFold(strings.TrimSpace(node.Get("type").String()), "input_image") {
			imageURL := strings.TrimSpace(node.Get("image_url").String())
			if imageURL == "" {
				imageURL = strings.TrimSpace(node.Get("image_url.url").String())
			}
			if imageURL != "" {
				images = append(images, imageURL)
			}
		}
		if content := node.Get("content"); content.Exists() {
			walk(content)
		}
	}
	walk(gjson.GetBytes(rawJSON, "input"))
	return images
}

func (h *OpenAIResponsesAPIHandler) handleCompanyResponsesImage(c *gin.Context, rawJSON []byte) bool {
	if !isCompanyResponsesImageRequest(c, rawJSON) {
		return false
	}
	request, imageJSON, err := buildCompanyResponsesImageRequest(rawJSON)
	if err != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{Error: handlers.ErrorDetail{
			Message: err.Error(), Type: "invalid_request_error",
		}})
		return true
	}
	if request.Stream {
		h.handleCompanyResponsesImageStream(c, request, imageJSON)
		return true
	}
	h.handleCompanyResponsesImageResponse(c, request, imageJSON)
	return true
}

func (h *OpenAIResponsesAPIHandler) imageExecutionContext(c *gin.Context, path string) (context.Context, handlers.APIHandlerCancelFunc) {
	ctx, cancel := h.GetContextWithCancel(h, c, context.Background())
	ctx = handlers.WithDisallowFreeAuth(ctx)
	return handlers.WithRequestPath(ctx, path), cancel
}

func (h *OpenAIResponsesAPIHandler) handleCompanyResponsesImageResponse(c *gin.Context, request companyResponsesImageRequest, imageJSON []byte) {
	c.Header("Content-Type", "application/json")
	ctx, cancel := h.imageExecutionContext(c, request.Path)
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, ctx)
	imagePayload, upstreamHeaders, errMsg := h.ExecuteImageWithAuthManager(ctx, "openai-image", request.Model, imageJSON, "")
	stopKeepAlive()
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cancel(errMsg.Error)
		return
	}
	responsePayload, err := buildCompanyResponsesImageResponse(imagePayload, request.Model)
	if err != nil {
		h.WriteErrorResponse(c, &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: err})
		cancel(err)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	_, _ = c.Writer.Write(responsePayload)
	cancel(nil)
}

func (h *OpenAIResponsesAPIHandler) handleCompanyResponsesImageStream(c *gin.Context, request companyResponsesImageRequest, imageJSON []byte) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{Error: handlers.ErrorDetail{
			Message: "Streaming not supported", Type: "server_error",
		}})
		return
	}

	ctx, cancel := h.imageExecutionContext(c, request.Path)
	setImagesSSEHeaders(c)

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	created := time.Now().Unix()
	createdResponse := []byte(`{"id":"","object":"response","created_at":0,"status":"in_progress","background":false,"error":null,"model":"","output":[],"parallel_tool_calls":true}`)
	createdResponse, _ = sjson.SetBytes(createdResponse, "id", responseID)
	createdResponse, _ = sjson.SetBytes(createdResponse, "created_at", created)
	createdResponse, _ = sjson.SetBytes(createdResponse, "model", request.Model)
	sequenceNumber := int64(0)
	writeResponseEvent := func(eventType string, payload []byte) {
		payload, _ = sjson.SetBytes(payload, "sequence_number", sequenceNumber)
		sequenceNumber++
		_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventType, payload)
		flusher.Flush()
	}
	createdEvent := []byte(`{"type":"response.created","sequence_number":0,"response":{}}`)
	createdEvent, _ = sjson.SetRawBytes(createdEvent, "response", createdResponse)
	writeResponseEvent("response.created", createdEvent)
	inProgressEvent := []byte(`{"type":"response.in_progress","sequence_number":0,"response":{}}`)
	inProgressEvent, _ = sjson.SetRawBytes(inProgressEvent, "response", createdResponse)
	writeResponseEvent("response.in_progress", inProgressEvent)

	// Image generation is completion-oriented. The native image executor still
	// uses its own upstream streaming path, while this compatibility surface
	// buffers the final image and emits a small, valid Responses event sequence.
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, ctx)
	imagePayload, upstreamHeaders, streamErr := h.ExecuteImageWithAuthManager(ctx, "openai-image", request.Model, imageJSON, "")
	stopKeepAlive()
	if streamErr != nil {
		status := streamErr.StatusCode
		if status <= 0 {
			status = http.StatusBadGateway
		}
		errText := responsesStreamErrorText(streamErr, status)
		writeResponseEvent("error", handlers.BuildOpenAIResponsesStreamErrorChunk(status, errText, 1))
		cancel(streamErr.Error)
		return
	}
	responsePayload, err := buildCompanyResponsesImageResponse(imagePayload, request.Model)
	if err != nil {
		errMsg := &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: err}
		writeResponseEvent("error", handlers.BuildOpenAIResponsesStreamErrorChunk(errMsg.StatusCode, errMsg.Error.Error(), 1))
		cancel(err)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	responsePayload, _ = sjson.SetBytes(responsePayload, "id", responseID)
	responsePayload, _ = sjson.SetBytes(responsePayload, "created_at", created)
	imageEvents, err := buildCompanyResponsesImageStreamEvents(responsePayload, sequenceNumber)
	if err != nil {
		errMsg := &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: err}
		writeResponseEvent("error", handlers.BuildOpenAIResponsesStreamErrorChunk(errMsg.StatusCode, errMsg.Error.Error(), int(sequenceNumber)))
		cancel(err)
		return
	}
	for _, event := range imageEvents {
		writeResponseEvent(event.Type, event.Payload)
	}
	cancel(nil)
}

type companyResponsesImageStreamEvent struct {
	Type    string
	Payload []byte
}

func buildCompanyResponsesImageStreamEvents(responsePayload []byte, sequenceNumber int64) ([]companyResponsesImageStreamEvent, error) {
	if !json.Valid(responsePayload) {
		return nil, fmt.Errorf("invalid image response envelope")
	}
	output := gjson.GetBytes(responsePayload, "output")
	if !output.IsArray() || len(output.Array()) == 0 {
		return nil, fmt.Errorf("image response envelope has no output items")
	}

	events := make([]companyResponsesImageStreamEvent, 0, len(output.Array())*2+1)
	for index, item := range output.Array() {
		itemPayload := []byte(item.Raw)
		if strings.TrimSpace(item.Get("id").String()) == "" {
			itemPayload, _ = sjson.SetBytes(itemPayload, "id", "ig_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
			responsePayload, _ = sjson.SetRawBytes(responsePayload, fmt.Sprintf("output.%d", index), itemPayload)
		}

		addedItem := itemPayload
		addedItem, _ = sjson.SetBytes(addedItem, "status", "in_progress")
		addedItem, _ = sjson.DeleteBytes(addedItem, "result")
		addedEvent := []byte(`{"type":"response.output_item.added","sequence_number":0,"output_index":0,"item":{}}`)
		addedEvent, _ = sjson.SetBytes(addedEvent, "sequence_number", sequenceNumber)
		sequenceNumber++
		addedEvent, _ = sjson.SetBytes(addedEvent, "output_index", index)
		addedEvent, _ = sjson.SetRawBytes(addedEvent, "item", addedItem)
		events = append(events, companyResponsesImageStreamEvent{Type: "response.output_item.added", Payload: addedEvent})

		doneEvent := []byte(`{"type":"response.output_item.done","sequence_number":0,"output_index":0,"item":{}}`)
		doneEvent, _ = sjson.SetBytes(doneEvent, "sequence_number", sequenceNumber)
		sequenceNumber++
		doneEvent, _ = sjson.SetBytes(doneEvent, "output_index", index)
		doneEvent, _ = sjson.SetRawBytes(doneEvent, "item", itemPayload)
		events = append(events, companyResponsesImageStreamEvent{Type: "response.output_item.done", Payload: doneEvent})
	}

	completedEvent := []byte(`{"type":"response.completed","sequence_number":0,"response":{}}`)
	completedEvent, _ = sjson.SetBytes(completedEvent, "sequence_number", sequenceNumber)
	completedEvent, _ = sjson.SetRawBytes(completedEvent, "response", responsePayload)
	events = append(events, companyResponsesImageStreamEvent{Type: "response.completed", Payload: completedEvent})
	return events, nil
}

func buildCompanyResponsesImageResponse(imagePayload []byte, model string) ([]byte, error) {
	if isXAIImagesModel(thinking.ParseSuffix(model).ModelName) {
		converted, err := buildImagesAPIResponseFromXAI(imagePayload, "b64_json")
		if err != nil {
			return nil, err
		}
		imagePayload = converted
	}
	results, err := extractCompanyImagesAPIResults(imagePayload)
	if err != nil {
		return nil, err
	}
	return buildCompanyResponsesImageEnvelope(model, results, gjson.GetBytes(imagePayload, "created").Int(), gjson.GetBytes(imagePayload, "usage")), nil
}

func extractCompanyImagesAPIResults(payload []byte) ([]companyResponsesImageResult, error) {
	if !json.Valid(payload) {
		return nil, fmt.Errorf("upstream returned invalid image response JSON")
	}
	data := gjson.GetBytes(payload, "data")
	if !data.IsArray() {
		return nil, fmt.Errorf("upstream image response has no data array")
	}
	results := make([]companyResponsesImageResult, 0, len(data.Array()))
	for _, item := range data.Array() {
		result := strings.TrimSpace(item.Get("b64_json").String())
		url := strings.TrimSpace(item.Get("url").String())
		if result == "" && strings.HasPrefix(url, "data:") {
			if comma := strings.IndexByte(url, ','); comma >= 0 {
				result = strings.TrimSpace(url[comma+1:])
			}
		}
		if result == "" {
			result = url
		}
		if result == "" && url == "" {
			continue
		}
		results = append(results, companyResponsesImageResult{
			Result:        result,
			URL:           url,
			RevisedPrompt: strings.TrimSpace(item.Get("revised_prompt").String()),
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("upstream did not return image output")
	}
	return results, nil
}

func buildCompanyResponsesImageEnvelope(model string, results []companyResponsesImageResult, created int64, usage gjson.Result) []byte {
	if created <= 0 {
		created = time.Now().Unix()
	}
	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	response := []byte(`{"id":"","object":"response","created_at":0,"status":"completed","model":"","output":[],"output_text":""}`)
	response, _ = sjson.SetBytes(response, "id", responseID)
	response, _ = sjson.SetBytes(response, "created_at", created)
	response, _ = sjson.SetBytes(response, "model", model)
	for _, result := range results {
		item := []byte(`{"type":"image_generation_call","id":"","status":"completed","result":""}`)
		item, _ = sjson.SetBytes(item, "id", "ig_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
		item, _ = sjson.SetBytes(item, "result", result.Result)
		if result.URL != "" {
			item, _ = sjson.SetBytes(item, "url", result.URL)
		}
		if result.RevisedPrompt != "" {
			item, _ = sjson.SetBytes(item, "revised_prompt", result.RevisedPrompt)
		}
		response, _ = sjson.SetRawBytes(response, "output.-1", item)
	}
	if usage.Exists() && json.Valid([]byte(usage.Raw)) {
		response, _ = sjson.SetRawBytes(response, "usage", []byte(usage.Raw))
	}
	return response
}
