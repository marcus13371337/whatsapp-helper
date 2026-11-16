package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

type call struct {
	server   *server
	whatsapp WhatsApp
	folder   string
	params   json.RawMessage
}

func (c *call) decode(into any) error {
	if len(c.params) == 0 {
		return nil
	}
	if err := json.Unmarshal(c.params, into); err != nil {
		return Errorf(CodeBadRequest, "The request's details were not in the shape the helper expects.")
	}
	return nil
}

type handler struct {
	fn         func(ctx context.Context, c *call) (any, error)
	serial     bool
	needsLogin bool
	lane       func(params json.RawMessage) string
}

var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"hello": {fn: hello, serial: true},
		"ping":  {fn: ping, serial: true},

		"login":  {fn: login},
		"logout": {fn: logout, needsLogin: true},

		"conversations": {fn: conversations, needsLogin: true},
		"messages":      {fn: messages, needsLogin: true},
		"send":          {fn: send, needsLogin: true, lane: conversationLane},
		"react":         {fn: react, needsLogin: true},
		"mark_read":     {fn: markRead, needsLogin: true},
		"attachment":    {fn: attachment, needsLogin: true},
	}
}

type helloParams struct {
	Protocol int    `json:"protocol"`
	Folder   string `json:"folder"`
}

type helloResult struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
}

func hello(_ context.Context, c *call) (any, error) {
	var p helloParams
	if err := c.decode(&p); err != nil {
		return nil, err
	}
	if p.Protocol != ProtocolVersion {
		return nil, Errorf(CodeUnsupportedProtocol, "This helper speaks protocol %d, not %d. Use a matching helper version.", ProtocolVersion, p.Protocol)
	}
	folder, err := resolveFolder(p.Folder)
	if err != nil {
		return nil, err
	}
	c.server.mu.Lock()
	c.server.folder = folder
	c.server.greeted = true
	c.server.mu.Unlock()
	return helloResult{Protocol: ProtocolVersion, Version: c.server.opts.Version}, nil
}

func resolveFolder(folder string) (string, error) {
	if folder == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		folder = cwd
	}
	if !filepath.IsAbs(folder) {
		return "", Errorf(CodeBadFolder, "The helper's folder must be given as a full path.")
	}
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return "", Errorf(CodeBadFolder, "The helper's folder does not exist.")
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return "", Errorf(CodeBadFolder, "The helper's folder is not a folder.")
	}
	return resolved, nil
}

func ping(context.Context, *call) (any, error) {
	return nil, nil
}
