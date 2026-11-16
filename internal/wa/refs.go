package wa

import (
	"encoding/base64"
	"encoding/json"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

type attachmentRef struct {
	Type          string `json:"type,omitempty"`
	Name          string `json:"name,omitempty"`
	DirectPath    string `json:"direct_path"`
	MediaKey      []byte `json:"media_key"`
	FileSHA256    []byte `json:"file_sha256,omitempty"`
	FileEncSHA256 []byte `json:"file_enc_sha256,omitempty"`
	MediaType     string `json:"media_type"`
}

func (r attachmentRef) encode() string {
	raw, _ := json.Marshal(r)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeRef(ref string) (attachmentRef, error) {
	var r attachmentRef
	raw, err := base64.RawURLEncoding.DecodeString(ref)
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	if err != nil || r.DirectPath == "" || len(r.MediaKey) == 0 || r.MediaType == "" {
		return r, helper.Errorf(helper.CodeBadRequest, "That attachment ref did not come from a message.")
	}
	return r, nil
}
