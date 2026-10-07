package forward

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

// ErrConversionSSEFrameTooLarge 表示累计的 SSE 帧超过转换器上限。
var ErrConversionSSEFrameTooLarge = errors.New("the upstream SSE frame exceeds the size limit")

const defaultConversionSSEFrameBytes = 500 * 1024 * 1024

// conversionSSEScanner 按空行划分上游事件，合并多行 data，并接受省略 event 的帧。
type conversionSSEScanner struct {
	lines      Lines
	parser     openai.OpenAICompatSSEFrameParser
	frame      openai.OpenAICompatSSEFrame
	finished   bool
	maxBytes   int
	frameBytes int
	err        error
}

func newConversionSSEScanner(in Response) *conversionSSEScanner {
	limit := in.MaxSSEFrameBytes
	if limit <= 0 {
		limit = defaultConversionSSEFrameBytes
	}
	return &conversionSSEScanner{lines: in.Lines, maxBytes: limit}
}

func (s *conversionSSEScanner) Scan() bool {
	if s.finished {
		return false
	}
	for s.lines.Scan() {
		line := strings.TrimSuffix(s.lines.Text(), "\r")
		if line != "" {
			// 在缓存前累计整行和换行符，短 data 行和注释也计入单帧上限。
			if len(line) >= s.maxBytes-s.frameBytes {
				s.err = ErrConversionSSEFrameTooLarge
				s.finished = true
				s.parser = openai.OpenAICompatSSEFrameParser{}
				s.frame = openai.OpenAICompatSSEFrame{}
				return false
			}
			s.frameBytes += len(line) + 1
		} else {
			s.frameBytes = 0
		}
		if frame, ok := s.parser.AddLine(line); ok {
			s.frame = frame
			return true
		}
	}
	s.finished = true
	s.err = s.lines.Err()
	frame, ok := s.parser.Finish()
	// 连接异常可能发生在 JSON 已完整到达之后，交付该帧以读取用量，Err 继续报告读取错误。
	if s.err != nil && !json.Valid([]byte(frame.Data)) {
		return false
	}
	s.frame = frame
	return ok
}

func (s *conversionSSEScanner) Err() error {
	return s.err
}
