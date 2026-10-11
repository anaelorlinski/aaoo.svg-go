package svgrender

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/anaelorlinski/aaoo.svg-go/svg"
)

// DataFetcher fetches data: URLs and nothing else: a document then reads
// nothing but itself. It is the fetcher used when [Options.Fetcher] is nil.
func DataFetcher(target string) (svg.Resource, error) {
	if !isDataURL(target) {
		return svg.Resource{}, fmt.Errorf("not a data URL, refused: %s", shorten(target))
	}
	return fetchDataURL(target)
}

// FileFetcher fetches data: URLs and the files of the machine, given as
// file: URLs or as paths. It fetches nothing over the network.
//
// A document drawn with it can read any file the program can: do not use it
// with a document that is not trusted.
func FileFetcher(target string) (svg.Resource, error) {
	if isDataURL(target) {
		return fetchDataURL(target)
	}
	path := target
	if parsed, err := url.Parse(target); err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "file" {
			return svg.Resource{}, fmt.Errorf("not a local file, refused: %s", shorten(target))
		}
		path = parsed.Path
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return svg.Resource{}, err
	}
	return svg.Resource{
		Content:       bytes.NewReader(content),
		Filename:      filepath.Base(path),
		MimeType:      mime.TypeByExtension(filepath.Ext(path)),
		RedirectedUrl: target,
	}, nil
}

func isDataURL(target string) bool {
	return len(target) >= 5 && strings.EqualFold(target[:5], "data:")
}

// shorten cuts a URL for a message: a data URL may be megabytes long.
func shorten(target string) string {
	if len(target) > 60 {
		return target[:60] + "…"
	}
	return target
}

func fetchDataURL(target string) (svg.Resource, error) {
	// a data URL written on several lines keeps its tabs and line breaks
	target = strings.NewReplacer("\t", "", "\n", "", "\f", "", "\r", "").Replace(target)
	meta, data, found := strings.Cut(target[len("data:"):], ",")
	if !found {
		return svg.Resource{}, errors.New("data not found in data URL")
	}

	out := svg.Resource{MimeType: "text/plain", ProtocolEncoding: "US-ASCII"}
	isBase64 := false
	for i, part := range strings.Split(meta, ";") {
		part = strings.TrimSpace(part)
		switch {
		case i == 0 && strings.Contains(part, "/"):
			out.MimeType = strings.ToLower(part)
			out.ProtocolEncoding = ""
		case strings.EqualFold(part, "base64"):
			isBase64 = true
		case strings.HasPrefix(strings.ToLower(part), "charset="):
			out.ProtocolEncoding = part[len("charset="):]
		}
	}

	var content []byte
	if isBase64 {
		// some writers leave spaces, or the padding out
		cleaned := strings.TrimRight(strings.ReplaceAll(data, " ", ""), "=")
		var err error
		content, err = base64.RawStdEncoding.DecodeString(cleaned)
		if err != nil {
			content, err = base64.RawURLEncoding.DecodeString(cleaned)
		}
		if err != nil {
			return svg.Resource{}, fmt.Errorf("invalid base64 in data URL: %s", err)
		}
	} else {
		unescaped, err := url.PathUnescape(data)
		if err != nil {
			return svg.Resource{}, fmt.Errorf("invalid data URL: %s", err)
		}
		content = []byte(unescaped)
	}
	out.Content = bytes.NewReader(content)
	return out, nil
}
