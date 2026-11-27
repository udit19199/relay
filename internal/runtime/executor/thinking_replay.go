package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/MoeForge/moego/ojson"
	internalcache "github.com/udit19199/relay/internal/cache"
	"github.com/udit19199/relay/internal/runtime/executor/helps"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/util"
)

type thinkingReplayScope struct {
	modelFamily   string
	sessionKey    string
	snapshot      internalcache.ClaudeThinkingReplaySnapshot
	cacheReady    bool
	replayApplied bool
}

func (s thinkingReplayScope) valid() bool {
	return strings.TrimSpace(s.modelFamily) != "" && strings.TrimSpace(s.sessionKey) != ""
}

func shouldClearThinkingReplayAfterError(err error) bool {
	if err == nil {
		return false
	}
	var upstreamStatus statusErr
	if !errors.As(err, &upstreamStatus) {
		return false
	}
	statusCode := upstreamStatus.StatusCode()
	return statusCode == 400 || statusCode == 422
}

func thinkingReplayContentIsReplayable(content []byte) bool {
	root := util.ParseGJSONBytesNoCopy(content)
	if !root.IsArray() {
		return false
	}
	hasSignedThinking := false
	hasToolUse := false
	for _, part := range root.Array() {
		switch strings.TrimSpace(part.Get("type").String()) {
		case "thinking":
			if strings.TrimSpace(part.Get("signature").String()) != "" {
				hasSignedThinking = true
			}
		case "tool_use":
			if strings.TrimSpace(part.Get("id").String()) != "" {
				hasToolUse = true
			}
		}
	}
	return hasSignedThinking && hasToolUse
}

func restoreThinkingReplayContentDoc(doc *ojson.Doc, cachedContent []byte) bool {
	if doc == nil {
		return false
	}
	cachedParts, cachedOK := thinkingReplayNonThinkingContentParts(util.ParseGJSONBytesNoCopy(cachedContent))
	if !cachedOK {
		return false
	}
	messages := doc.Get("messages")
	if !messages.IsArray() {
		return false
	}
	messageItems := messages.Array()
	for index := len(messageItems) - 1; index >= 0; index-- {
		message := messageItems[index]
		if !strings.EqualFold(strings.TrimSpace(message.Get("role").String()), "assistant") {
			continue
		}
		currentContent := message.Get("content")
		if thinkingReplayJSONEqual([]byte(currentContent.Raw), cachedContent) {
			return false
		}
		if thinkingReplayContentHasThinking(currentContent) {
			continue
		}
		currentParts, currentOK := thinkingReplayNonThinkingContentParts(currentContent)
		if !currentOK || !thinkingReplayCanonicalPartsEqual(currentParts, cachedParts) {
			continue
		}
		return doc.SetRawBytes(fmt.Sprintf("messages.%d.content", index), cachedContent) == nil
	}
	return false
}

func thinkingReplayContentHasThinking(content ojson.Result) bool {
	if !content.IsArray() {
		return false
	}
	for _, part := range content.Array() {
		switch strings.TrimSpace(part.Get("type").String()) {
		case "thinking", "redacted_thinking":
			return true
		}
	}
	return false
}

func thinkingReplayNonThinkingContentParts(content ojson.Result) ([][]byte, bool) {
	if !content.IsArray() {
		return nil, false
	}
	parts := make([][]byte, 0, len(content.Array()))
	hasToolUse := false
	for _, part := range content.Array() {
		switch strings.TrimSpace(part.Get("type").String()) {
		case "thinking", "redacted_thinking":
			continue
		case "tool_use":
			if strings.TrimSpace(part.Get("id").String()) == "" {
				return nil, false
			}
			hasToolUse = true
		}
		canonical, ok := thinkingReplayCanonicalJSON([]byte(part.Raw))
		if !ok {
			return nil, false
		}
		parts = append(parts, canonical)
	}
	return parts, hasToolUse
}

func thinkingReplayCanonicalPartsEqual(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !bytes.Equal(left[i], right[i]) {
			return false
		}
	}
	return true
}

func thinkingReplayJSONEqual(left, right []byte) bool {
	canonicalLeft, leftOK := thinkingReplayCanonicalJSON(left)
	canonicalRight, rightOK := thinkingReplayCanonicalJSON(right)
	return leftOK && rightOK && bytes.Equal(canonicalLeft, canonicalRight)
}

func thinkingReplayCanonicalJSON(raw []byte) ([]byte, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if errDecode := decoder.Decode(&value); errDecode != nil {
		return nil, false
	}
	canonical, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return nil, false
	}
	return canonical, true
}

type thinkingReplayStreamBlock struct {
	raw                  []byte
	text                 strings.Builder
	thinking             strings.Builder
	signature            strings.Builder
	input                strings.Builder
	textInitialized      bool
	thinkingInitialized  bool
	signatureInitialized bool
	hasInputDelta        bool
	finished             bool
}

type thinkingReplayStreamAccumulator struct {
	blocks        map[int]*thinkingReplayStreamBlock
	observed      bool
	complete      bool
	upstreamError bool
	abandoned     bool
	bytesUsed     int
}

func newThinkingReplayStreamAccumulator() *thinkingReplayStreamAccumulator {
	return &thinkingReplayStreamAccumulator{blocks: make(map[int]*thinkingReplayStreamBlock)}
}

func (a *thinkingReplayStreamAccumulator) observe(chunk []byte) {
	for _, line := range bytes.Split(chunk, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		if !ojson.ValidBytes(payload) {
			a.abandon()
			continue
		}
		root := util.ParseGJSONBytesNoCopy(payload)
		switch root.Get("type").String() {
		case "message_start":
			a.observed = true
		case "content_block_start":
			if !a.abandoned {
				a.observeBlockStart(root)
			}
		case "content_block_delta":
			if !a.abandoned {
				a.observeBlockDelta(root)
			}
		case "content_block_stop":
			if !a.abandoned {
				a.finishBlock(int(root.Get("index").Int()))
			}
		case "message_stop":
			a.complete = true
		case "error":
			a.upstreamError = true
			a.abandon()
		}
	}
}

func (a *thinkingReplayStreamAccumulator) observeBlockStart(root ojson.Result) {
	index := int(root.Get("index").Int())
	block := root.Get("content_block")
	if !block.IsObject() || len(a.blocks) >= internalcache.ClaudeThinkingReplayCacheMaxBlocksPerTurn {
		a.abandon()
		return
	}
	if _, exists := a.blocks[index]; exists {
		a.abandon()
		return
	}
	raw := []byte(block.Raw)
	if !a.reserveBytes(len(raw)) {
		return
	}
	a.blocks[index] = &thinkingReplayStreamBlock{raw: append([]byte(nil), raw...)}
}

func (a *thinkingReplayStreamAccumulator) observeBlockDelta(root ojson.Result) {
	index := int(root.Get("index").Int())
	block, ok := a.blocks[index]
	if !ok {
		a.abandon()
		return
	}
	delta := root.Get("delta")
	switch delta.Get("type").String() {
	case "text_delta":
		a.appendBlockText(block, &block.text, &block.textInitialized, "text", delta.Get("text").String())
	case "thinking_delta":
		a.appendBlockText(block, &block.thinking, &block.thinkingInitialized, "thinking", delta.Get("thinking").String())
	case "signature_delta":
		a.appendBlockText(block, &block.signature, &block.signatureInitialized, "signature", delta.Get("signature").String())
	case "input_json_delta":
		suffix := delta.Get("partial_json").String()
		if a.reserveBytes(len(suffix)) {
			block.input.WriteString(suffix)
			block.hasInputDelta = true
		}
	default:
		a.abandon()
	}
}

func (a *thinkingReplayStreamAccumulator) appendBlockText(block *thinkingReplayStreamBlock, builder *strings.Builder, initialized *bool, path, suffix string) {
	if !*initialized {
		initial := ojson.GetBytesNoCopy(block.raw, path).String()
		if !a.reserveBytes(len(initial)) {
			return
		}
		builder.WriteString(initial)
		*initialized = true
	}
	if a.reserveBytes(len(suffix)) {
		builder.WriteString(suffix)
	}
}

func (a *thinkingReplayStreamAccumulator) finishBlock(index int) {
	block, ok := a.blocks[index]
	if !ok {
		a.abandon()
		return
	}
	if block.hasInputDelta && !ojson.Valid(block.input.String()) {
		a.abandon()
		return
	}
	block.finished = true
}

func (a *thinkingReplayStreamAccumulator) reserveBytes(count int) bool {
	if count < 0 || a.bytesUsed > internalcache.ClaudeThinkingReplayCacheMaxBytesPerSession-count {
		a.abandon()
		return false
	}
	a.bytesUsed += count
	return true
}

func (a *thinkingReplayStreamAccumulator) abandon() {
	a.abandoned = true
	a.blocks = nil
	a.bytesUsed = 0
}

func (a *thinkingReplayStreamAccumulator) content() ([]byte, bool) {
	if !a.observed || !a.complete || a.upstreamError || a.abandoned {
		return nil, false
	}
	indexes := make([]int, 0, len(a.blocks))
	for index := range a.blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	parts := make([][]byte, 0, len(indexes))
	for _, index := range indexes {
		block := a.blocks[index]
		if !block.finished {
			a.abandon()
			return nil, false
		}
		if !block.textInitialized && !block.thinkingInitialized && !block.signatureInitialized && !block.hasInputDelta {
			parts = append(parts, append([]byte(nil), block.raw...))
			continue
		}
		blockDoc := ojson.NewDoc(block.raw)
		var errSet error
		if block.textInitialized {
			errSet = blockDoc.Set("text", block.text.String())
		}
		if errSet == nil && block.thinkingInitialized {
			errSet = blockDoc.Set("thinking", block.thinking.String())
		}
		if errSet == nil && block.signatureInitialized {
			errSet = blockDoc.Set("signature", block.signature.String())
		}
		if errSet == nil && block.hasInputDelta {
			errSet = blockDoc.SetRawBytes("input", []byte(block.input.String()))
		}
		if errSet != nil {
			a.abandon()
			return nil, false
		}
		parts = append(parts, blockDoc.Bytes())
	}
	content := helps.JoinRawJSONArray(parts)
	if len(content) > internalcache.ClaudeThinkingReplayCacheMaxBytesPerSession {
		a.abandon()
		return nil, false
	}
	return content, true
}

type thinkingReplayContentCacheFunc func(context.Context, thinkingReplayScope, []byte)
type thinkingReplayContentClearFunc func(context.Context, thinkingReplayScope)

func wrapThinkingReplayStream(ctx context.Context, result *coreexecutor.StreamResult, scope thinkingReplayScope, cacheContent thinkingReplayContentCacheFunc, clearContent thinkingReplayContentClearFunc) *coreexecutor.StreamResult {
	if result == nil || !scope.valid() {
		return result
	}
	out := make(chan coreexecutor.StreamChunk)
	go func() {
		defer close(out)
		accumulator := newThinkingReplayStreamAccumulator()
		hasError := false
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				hasError = true
			} else {
				accumulator.observe(chunk.Payload)
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
		if hasError {
			return
		}
		if content, completed := accumulator.content(); completed {
			cacheContent(ctx, scope, content)
			return
		}
		if accumulator.upstreamError && scope.replayApplied {
			clearContent(ctx, scope)
		}
	}()
	return &coreexecutor.StreamResult{Headers: result.Headers.Clone(), Chunks: out}
}
