package openai

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// handleCompanyResponsesImageWebsocket keeps the company image adapter aligned
// with Codex++ clients that use the Responses WebSocket transport. The regular
// WebSocket forwarding path remains unchanged for every non-image request.
func (h *OpenAIResponsesAPIHandler) handleCompanyResponsesImageWebsocket(
	c *gin.Context,
	writer *responsesWebsocketWriter,
	timeline websocketTimelineAppender,
	rawJSON []byte,
) bool {
	if strings.TrimSpace(gjson.GetBytes(rawJSON, "type").String()) != "response.create" ||
		!isCompanyResponsesImageRequest(c, rawJSON) {
		return false
	}

	request, imageJSON, err := buildCompanyResponsesImageRequest(rawJSON)
	if err != nil {
		errMsg := &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: err}
		writeCompanyImageWebsocketError(writer, timeline, errMsg)
		return true
	}

	sequenceNumber := int64(0)
	writeEvent := func(eventType string, payload []byte) error {
		payload, _ = sjson.SetBytes(payload, "sequence_number", sequenceNumber)
		sequenceNumber++
		return writeResponsesWebsocketPayload(writer, timeline, payload, time.Now())
	}

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	created := time.Now().Unix()
	createdResponse := []byte(`{"id":"","object":"response","created_at":0,"status":"in_progress","background":false,"error":null,"model":"","output":[],"parallel_tool_calls":true}`)
	createdResponse, _ = sjson.SetBytes(createdResponse, "id", responseID)
	createdResponse, _ = sjson.SetBytes(createdResponse, "created_at", created)
	createdResponse, _ = sjson.SetBytes(createdResponse, "model", request.Model)
	createdEvent := []byte(`{"type":"response.created","response":{}}`)
	createdEvent, _ = sjson.SetRawBytes(createdEvent, "response", createdResponse)
	if errWrite := writeEvent("response.created", createdEvent); errWrite != nil {
		return true
	}
	inProgressEvent := []byte(`{"type":"response.in_progress","response":{}}`)
	inProgressEvent, _ = sjson.SetRawBytes(inProgressEvent, "response", createdResponse)
	if errWrite := writeEvent("response.in_progress", inProgressEvent); errWrite != nil {
		return true
	}

	// Announce the output item before the upstream image request starts. This is
	// important for WebSocket clients that wait for the item lifecycle to begin
	// before accepting the eventual multi-megabyte image result.
	itemID := "ig_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	addedItem := []byte(`{"type":"image_generation_call","id":"","status":"in_progress"}`)
	addedItem, _ = sjson.SetBytes(addedItem, "id", itemID)
	addedEvent := []byte(`{"type":"response.output_item.added","output_index":0,"item":{}}`)
	addedEvent, _ = sjson.SetRawBytes(addedEvent, "item", addedItem)
	if errWrite := writeEvent("response.output_item.added", addedEvent); errWrite != nil {
		return true
	}

	ctx, cancel := h.imageExecutionContext(c, request.Path)
	defer cancel(nil)
	stopKeepAlive := startCompanyImageWebsocketKeepAlive(h, writer)
	imagePayload, upstreamHeaders, streamErr := h.ExecuteImageWithAuthManager(ctx, "openai-image", request.Model, imageJSON, "")
	stopKeepAlive()
	if streamErr != nil {
		status := streamErr.StatusCode
		if status <= 0 {
			status = http.StatusBadGateway
		}
		writeCompanyImageWebsocketError(writer, timeline, &interfaces.ErrorMessage{
			StatusCode: status,
			Error:      fmt.Errorf("%s", responsesStreamErrorText(streamErr, status)),
		})
		cancel(streamErr.Error)
		return true
	}

	responsePayload, err := buildCompanyResponsesImageResponse(imagePayload, request.Model)
	if err != nil {
		writeCompanyImageWebsocketError(writer, timeline, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadGateway,
			Error:      err,
		})
		cancel(err)
		return true
	}
	_ = upstreamHeaders
	responsePayload, _ = sjson.SetBytes(responsePayload, "id", responseID)
	responsePayload, _ = sjson.SetBytes(responsePayload, "created_at", created)
	if gjson.GetBytes(responsePayload, "output.0").Exists() {
		responsePayload, _ = sjson.SetBytes(responsePayload, "output.0.id", itemID)
	}

	imageEvents, err := buildCompanyResponsesImageStreamEvents(responsePayload, 0)
	if err != nil {
		writeCompanyImageWebsocketError(writer, timeline, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadGateway,
			Error:      err,
		})
		cancel(err)
		return true
	}
	for index, event := range imageEvents {
		if index == 0 {
			// The item was announced before the upstream call above.
			continue
		}
		if errWrite := writeEvent(event.Type, event.Payload); errWrite != nil {
			cancel(errWrite)
			return true
		}
	}
	cancel(nil)
	return true
}

func writeCompanyImageWebsocketError(writer *responsesWebsocketWriter, timeline websocketTimelineAppender, errMsg *interfaces.ErrorMessage) {
	if writer == nil || errMsg == nil {
		return
	}
	payload, err := buildResponsesWebsocketErrorPayload(errMsg)
	if err != nil {
		_, _ = writer.closeWithoutError()
		return
	}
	if errWrite := writeResponsesWebsocketPayload(writer, timeline, payload, time.Now()); errWrite != nil {
		return
	}
	_, _ = writer.closeWithoutError()
}

func startCompanyImageWebsocketKeepAlive(h *OpenAIResponsesAPIHandler, writer *responsesWebsocketWriter) func() {
	interval := handlers.StreamingKeepAliveInterval(h.Cfg)
	if interval <= 0 {
		return func() {}
	}
	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := writer.writePing(); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
	}
}
