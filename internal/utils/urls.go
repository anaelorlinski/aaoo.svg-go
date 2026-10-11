package utils

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html/charset"
)

type Resource struct {
	Content *bytes.Reader

	// Optionnals values

	// MIME type extracted e.g. from a *Content-Type* header. If not provided, the type is guessed from the
	// 	file extension in the URL.
	MimeType string

	// actual URL of the resource
	// 	if there were e.g. HTTP redirects.
	RedirectedUrl string

	// filename of the resource. Usually
	// 	derived from the *filename* parameter in a *Content-Disposition*
	// 	header
	Filename string

	ProtocolEncoding string
}

// Fetcher fetches an external resource such as a stylesheet or another
// SVG document.
type Fetcher = func(url string) (Resource, error)

// micmic python urllib.urljoin behavior
func basicUrlJoin(baseUrl string, urls *url.URL) (string, error) {
	parsedBase, err := url.Parse(baseUrl)
	if err != nil {
		return "", fmt.Errorf("invalid base url : %s", baseUrl)
	}
	if urls.Host != "" { // copy the scheme from base
		urls.Scheme = parsedBase.Scheme
		*parsedBase = *urls
	} else if path.IsAbs(urls.Path) { // join from the root
		parsedBase.Path = urls.Path
	} else { // join from the directory
		if path.Ext(parsedBase.Path) != "" {
			parsedBase.Path = path.Join(path.Dir(parsedBase.Path), urls.Path)
		} else {
			parsedBase.Path = path.Join(parsedBase.Path, urls.Path)
		}
	}
	parsedBase.RawQuery = urls.RawQuery
	parsedBase.RawFragment = urls.RawFragment
	parsedBase.Fragment = urls.Fragment
	return parsedBase.String(), nil
}

// dataURLSpaces removes what a data URL written on several lines holds
// besides its data.
var dataURLSpaces = strings.NewReplacer("\t", "", "\n", "", "\f", "", "\r", "")

// defaut: allowRelative = false
func SafeUrljoin(baseUrl, urls string, allowRelative bool) (string, error) {
	// A data URL is not relative to anything, and is not a URL the standard
	// library parses: it may be megabytes long and written on several
	// lines, whose breaks and indentation are not part of it.
	if trimmed := strings.TrimSpace(urls); len(trimmed) >= 5 && strings.EqualFold(trimmed[:5], "data:") {
		return dataURLSpaces.Replace(trimmed), nil
	}
	parsed, err := url.Parse(urls)
	if err != nil {
		return "", fmt.Errorf("invalid url : %s (%s)", urls, err)
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	} else if baseUrl != "" {
		return basicUrlJoin(baseUrl, parsed)
	} else if allowRelative {
		return parsed.String(), nil
	} else {
		return "", errors.New("Relative URI reference without a base URI: " + urls)
	}
}

// Return a file URL for the given `file` path.
func PathToURL(file string) (out string, err error) {
	file, err = filepath.Abs(file)
	if err != nil {
		return "", err
	}
	fileinfo, err := os.Lstat(file)
	if err != nil {
		return "", err
	}
	if fileinfo.IsDir() {
		// Make sure directory names have a trailing slash.
		// Otherwise relative URIs are resolved from the parent directory.
		file += string(filepath.Separator)
	}
	file = filepath.ToSlash(file)
	return "file://" + file, nil
}

// FetchText fetches the text at url, decoded to UTF-8.
func FetchText(url string, urlFetcher Fetcher) ([]byte, error) {
	result, err := urlFetcher(url)
	if err != nil {
		return nil, err
	}
	return decodeToUtf8(result.Content, result.ProtocolEncoding)
}

func decodeToUtf8(data io.Reader, encod string) ([]byte, error) {
	if encod == "" { // assume UTF8
		return io.ReadAll(data)
	}
	enc, _ := charset.Lookup(encod)
	if enc == nil {
		return nil, fmt.Errorf("unsupported encoding %s", encod)
	}
	return io.ReadAll(enc.NewDecoder().Reader(data))
}
