package openai

import "bytes"

type sseFrameAccumulator struct {
	pending         []byte
	syntheticBreaks []int
}

func (a *sseFrameAccumulator) AddChunk(chunk []byte) [][]byte {
	if len(chunk) == 0 {
		return nil
	}

	var frames [][]byte
	if responsesSSEStartsNewDataFrame(a.pending, chunk) {
		frames = append(frames, bytes.Clone(responsesSSERecoverJSONFrame(a.pending, a.syntheticBreaks)))
		a.pending = a.pending[:0]
		a.syntheticBreaks = a.syntheticBreaks[:0]
	}
	a.pending = responsesSSEAppendChunk(a.pending, &a.syntheticBreaks, chunk)

	for {
		frameLen := responsesSSEFrameLen(a.pending)
		if frameLen == 0 {
			break
		}
		frames = append(frames, bytes.Clone(responsesSSERecoverJSONFrame(a.pending[:frameLen], a.syntheticBreaks)))
		copy(a.pending, a.pending[frameLen:])
		a.pending = a.pending[:len(a.pending)-frameLen]
		a.syntheticBreaks = responsesSSEConsumeBreaks(a.syntheticBreaks, frameLen)
	}

	if len(bytes.TrimSpace(a.pending)) == 0 {
		a.pending = a.pending[:0]
		a.syntheticBreaks = a.syntheticBreaks[:0]
		return frames
	}
	if len(a.pending) == 0 || !responsesSSECanEmitWithoutDelimiter(a.pending) {
		return frames
	}
	frames = append(frames, bytes.Clone(responsesSSERecoverJSONFrame(a.pending, a.syntheticBreaks)))
	a.pending = a.pending[:0]
	a.syntheticBreaks = a.syntheticBreaks[:0]
	return frames
}

func (a *sseFrameAccumulator) Flush() [][]byte {
	if len(a.pending) == 0 {
		return nil
	}

	var frames [][]byte
	for {
		frameLen := responsesSSEFrameLen(a.pending)
		if frameLen == 0 {
			break
		}
		frames = append(frames, bytes.Clone(responsesSSERecoverJSONFrame(a.pending[:frameLen], a.syntheticBreaks)))
		copy(a.pending, a.pending[frameLen:])
		a.pending = a.pending[:len(a.pending)-frameLen]
		a.syntheticBreaks = responsesSSEConsumeBreaks(a.syntheticBreaks, frameLen)
	}

	if len(bytes.TrimSpace(a.pending)) == 0 {
		a.pending = nil
		return frames
	}
	frame := responsesSSERecoverJSONFrame(a.pending, a.syntheticBreaks)
	if responsesSSECanFlushWithoutDelimiter(frame) {
		frames = append(frames, bytes.Clone(frame))
	}
	a.pending = nil
	a.syntheticBreaks = nil
	return frames
}
