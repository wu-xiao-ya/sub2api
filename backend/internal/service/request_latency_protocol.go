package service

import (
	"github.com/tidwall/gjson"
	"strings"
)

// Classifies protocol content, not provider names. Compatible providers use the
// same wire format; a transport heartbeat never counts as semantic output.
func classifyLatencyPayload(data, event string) (valid, output, visible bool) {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" || !gjson.Valid(data) {
		return
	}
	root := gjson.Parse(data)
	if root.Get("response.candidates").Exists() {
		root = root.Get("response")
	}
	typ := strings.TrimSpace(event)
	if typ == "" {
		typ = root.Get("type").String()
	}
	if typ == "ping" || typ == "heartbeat" {
		return
	}
	valid = root.Get("choices").IsArray() || root.Get("candidates").IsArray() ||
		strings.HasPrefix(typ, "response.") || strings.HasPrefix(typ, "message_") ||
		strings.HasPrefix(typ, "content_block_") || typ == "message" || root.Get("output").IsArray()
	if !valid {
		return
	}
	visible = openAIStreamDataContainsVisibleCharacter(data, typ)
	nonblank := func(v gjson.Result) bool { return gjsonStringHasVisibleCharacter(v) }
	for _, choice := range root.Get("choices").Array() {
		for _, part := range []gjson.Result{choice.Get("delta"), choice.Get("message")} {
			visible = visible || nonblank(part.Get("refusal"))
			for _, content := range part.Get("content").Array() {
				visible = visible || nonblank(content.Get("text"))
			}
			output = output || nonblank(part.Get("reasoning_content")) || nonblank(part.Get("reasoning"))
			for _, tool := range part.Get("tool_calls").Array() {
				output = output || nonblank(tool.Get("function.name")) || nonblank(tool.Get("function.arguments"))
			}
			output = output || nonblank(part.Get("function_call.name")) || nonblank(part.Get("function_call.arguments"))
		}
	}
	switch typ {
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta":
		output = output || nonblank(root.Get("delta"))
	case "response.function_call_arguments.done":
		output = output || nonblank(root.Get("arguments"))
	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		output = output || nonblank(root.Get("part.text"))
	case "response.output_item.added", "response.output_item.done":
		item := root.Get("item")
		for _, part := range item.Get("summary").Array() {
			output = output || nonblank(part.Get("text"))
		}
		output = output || (item.Get("type").String() == "function_call" && (nonblank(item.Get("name")) || nonblank(item.Get("arguments"))))
	case "content_block_delta":
		delta := root.Get("delta")
		visible = visible || nonblank(delta.Get("text"))
		output = output || nonblank(delta.Get("thinking")) || nonblank(delta.Get("partial_json"))
	case "content_block_start":
		block := root.Get("content_block")
		visible = visible || nonblank(block.Get("text"))
		output = output || nonblank(block.Get("thinking")) || (block.Get("type").String() == "tool_use" && nonblank(block.Get("name")))
	}
	// Native Anthropic non-stream responses.
	if typ == "message" {
		for _, part := range root.Get("content").Array() {
			visible = visible || nonblank(part.Get("text"))
			output = output || nonblank(part.Get("thinking")) || (part.Get("type").String() == "tool_use" && nonblank(part.Get("name")))
		}
	}
	// Native Gemini and Antigravity's response envelope.
	for _, candidate := range root.Get("candidates").Array() {
		for _, part := range candidate.Get("content.parts").Array() {
			text := nonblank(part.Get("text"))
			output = output || text || nonblank(part.Get("functionCall.name")) || part.Get("inlineData.data").Exists()
			visible = visible || (text && !part.Get("thought").Bool())
		}
	}
	// Responses non-stream body; images are output but never visible text.
	items := root.Get("output")
	if !items.Exists() {
		items = root.Get("response.output")
	}
	for _, item := range items.Array() {
		visible = visible || openAIResponseItemContainsVisibleCharacter(item)
		output = output || (item.Get("type").String() == "function_call" && nonblank(item.Get("name"))) ||
			(item.Get("type").String() == "image_generation_call" && nonblank(item.Get("result")))
		for _, part := range item.Get("summary").Array() {
			output = output || nonblank(part.Get("text"))
		}
	}
	return valid, output || visible, visible
}
