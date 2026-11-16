package helper

import (
	"cmp"
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func resolveInFolder(folder, path string) (string, error) {
	if path == "" {
		return "", Errorf(CodeBadRequest, "A file needs a path.")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(folder, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", Errorf(CodeFileProblem, "The file %q could not be found in the helper's folder.", filepath.Base(path))
	}
	if !strings.HasPrefix(resolved, folder+string(filepath.Separator)) {
		return "", Errorf(CodeOutsideFolder, "The file %q is outside the helper's folder. Put it in the folder first.", filepath.Base(path))
	}
	if info, err := os.Stat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", Errorf(CodeFileProblem, "%q is not a file the helper can send.", filepath.Base(path))
	}
	return resolved, nil
}

type attachmentParams struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

type attachmentResult struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

func attachment(ctx context.Context, c *call) (any, error) {
	var p attachmentParams
	if err := c.decode(&p); err != nil {
		return nil, err
	}
	if p.Ref == "" {
		return nil, Errorf(CodeBadRequest, "The request must carry the attachment's ref from its message.")
	}
	file, err := os.CreateTemp(c.folder, "attachment-*")
	if err != nil {
		return nil, Errorf(CodeFileProblem, "The helper could not write into its folder.")
	}
	keep := false
	defer func() {
		file.Close()
		if !keep {
			os.Remove(file.Name())
		}
	}()
	downloaded, err := c.whatsapp.Download(ctx, p.Ref, file)
	if err != nil {
		return nil, err
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	contentType := downloaded.Type
	if contentType == "" {
		contentType = sniffType(file)
	}
	name := safeName(cmp.Or(p.Name, downloaded.Name), contentType)
	path := file.Name() + "-" + name
	if err := os.Rename(file.Name(), path); err != nil {
		return nil, Errorf(CodeFileProblem, "The helper could not write into its folder.")
	}
	keep = true
	return attachmentResult{Path: path, Name: name, Size: size, Type: contentType}, nil
}

func sniffType(file *os.File) string {
	head := make([]byte, 512)
	n, _ := file.ReadAt(head, 0)
	return http.DetectContentType(head[:n])
}

func safeName(name, contentType string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		name = ""
	}
	if name == "" {
		name = "file" + extensionFor(contentType)
	}
	return name
}

var preferredExtensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"video/mp4":       ".mp4",
	"video/3gpp":      ".3gp",
	"audio/ogg":       ".ogg",
	"audio/mpeg":      ".mp3",
	"audio/mp4":       ".m4a",
	"audio/aac":       ".aac",
	"application/pdf": ".pdf",
}

func extensionFor(contentType string) string {
	base, _, _ := strings.Cut(contentType, ";")
	base = strings.TrimSpace(base)
	if extension, ok := preferredExtensions[base]; ok {
		return extension
	}
	if extensions, _ := mime.ExtensionsByType(base); len(extensions) > 0 {
		return extensions[0]
	}
	return ""
}
