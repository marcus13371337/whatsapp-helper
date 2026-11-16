package helper

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

const MaxLineBytes = 4 << 20

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type wireError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    wireErrorData `json:"data"`
}

type wireErrorData struct {
	Code string `json:"code"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

var jsonRPCCodes = map[string]int{
	CodeBadRequest:    -32600,
	CodeUnknownMethod: -32601,
	CodeInternal:      -32603,
}

func toWireError(e *Error) *wireError {
	code, ok := jsonRPCCodes[e.Code]
	if !ok {
		code = -32000
	}
	return &wireError{Code: code, Message: e.Message, Data: wireErrorData{Code: e.Code}}
}

var errLineTooLong = errors.New("line too long")

type lineWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *lineWriter) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw)+1 > MaxLineBytes {
		return errLineTooLong
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.out.Write(append(raw, '\n'))
	return err
}

type readLine struct {
	raw     []byte
	tooLong bool
}

func readLines(in io.Reader, lines chan<- readLine) {
	defer close(lines)
	reader := bufio.NewReaderSize(in, 64<<10)
	for {
		var buf []byte
		tooLong := false
		for {
			chunk, isPrefix, err := reader.ReadLine()
			if err != nil {
				return
			}
			if !tooLong {
				buf = append(buf, chunk...)
				if len(buf) > MaxLineBytes {
					tooLong, buf = true, nil
				}
			}
			if !isPrefix {
				break
			}
		}
		lines <- readLine{raw: buf, tooLong: tooLong}
	}
}
