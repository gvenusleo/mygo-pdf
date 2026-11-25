package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const releaseOutput = "build/pdfium-release"

type releaseFile struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type releaseLibrary struct {
	Name  string                 `json:"name"`
	Files map[string]releaseFile `json:"files"`
}

type pluginManifest struct {
	Libraries []releaseLibrary `json:"libraries"`
}

type licenseFile struct {
	name   string
	digest [sha256.Size]byte
}

type licenseNotice struct {
	text      []byte
	platforms []string
}

func prepareRelease(tag string) error {
	if err := os.MkdirAll(releaseOutput, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(releaseOutput, "prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	base := "https://github.com/gvenusleo/mygo-pdf/releases/download/" + url.PathEscape(tag)
	libraries := releaseLibrary{Name: "pdfium", Files: make(map[string]releaseFile)}
	licenses := make(map[licenseFile]licenseNotice)
	for _, target := range slices.Sorted(maps.Keys(archives)) {
		platform := strings.Replace(target, "/", "-", 1)
		file, err := releasePlatform(stage, base, platform, archives[target], licenses)
		if err != nil {
			return fmt.Errorf("prepare release for %s: %w", target, err)
		}
		libraries.Files[platform] = file
	}
	license, err := writeNotices(base, licenses)
	if err != nil {
		return err
	}
	notices := releaseLibrary{Name: "pdfium-notices", Files: make(map[string]releaseFile)}
	for platform := range libraries.Files {
		notices.Files[platform] = license
	}
	manifest, err := json.Marshal(pluginManifest{Libraries: []releaseLibrary{libraries, notices}}, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(releaseOutput, "mygo-plugin.json"), append(manifest, '\n'), 0o644)
}

func releasePlatform(stage, base, platform string, spec archive, licenses map[licenseFile]licenseNotice) (releaseFile, error) {
	data, err := download(spec)
	if err != nil {
		return releaseFile{}, err
	}
	dir := filepath.Join(stage, platform)
	if err := extract(dir, spec.library, data); err != nil {
		return releaseFile{}, err
	}
	name := filepath.Base(spec.library)
	binary, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return releaseFile{}, err
	}
	if digest := fmt.Sprintf("%x", sha256.Sum256(binary)); digest != spec.libraryDigest {
		return releaseFile{}, fmt.Errorf("PDFium library SHA-256 mismatch")
	}
	if err := collectNotices(dir, name, platform, licenses); err != nil {
		return releaseFile{}, err
	}
	asset := platform + "-" + name
	if err := os.WriteFile(filepath.Join(releaseOutput, asset), binary, 0o644); err != nil {
		return releaseFile{}, err
	}
	return releaseFile{Name: name, URL: base + "/" + asset, SHA256: spec.libraryDigest}, nil
}

func collectNotices(dir, library, platform string, licenses map[licenseFile]licenseNotice) error {
	return fs.WalkDir(os.DirFS(dir), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || name == library {
			return err
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		key := licenseFile{name: name, digest: sha256.Sum256(data)}
		if !utf8.Valid(data) {
			if name != "licenses/freetype.txt" {
				return fmt.Errorf("unexpected encoding in %s", name)
			}
			// The upstream FreeType notice is distributed as Latin-1.
			var text strings.Builder
			for _, b := range data {
				text.WriteRune(rune(b))
			}
			data = []byte(text.String())
		}
		notice := licenses[key]
		notice.text = data
		notice.platforms = append(notice.platforms, platform)
		licenses[key] = notice
		return nil
	})
}

func writeNotices(base string, licenses map[licenseFile]licenseNotice) (releaseFile, error) {
	var report bytes.Buffer
	for _, path := range []string{"LICENSE", "resources/pdfium-mygo-LICENSE.txt"} {
		data, err := os.ReadFile(path)
		if err != nil {
			return releaseFile{}, err
		}
		fmt.Fprintf(&report, "===== MyGo PDF source notice: %s =====\n\n%s\n\n", path, data)
	}
	keys := slices.SortedFunc(maps.Keys(licenses), func(a, b licenseFile) int {
		if order := strings.Compare(a.name, b.name); order != 0 {
			return order
		}
		return bytes.Compare(a.digest[:], b.digest[:])
	})
	for _, key := range keys {
		notice := licenses[key]
		fmt.Fprintf(&report, "===== PDFium %s (%s) =====\n\n%s\n\n", key.name, strings.Join(notice.platforms, ", "), notice.text)
	}
	const name = "pdfium-notices.txt"
	data := report.Bytes()
	if err := os.WriteFile(filepath.Join(releaseOutput, name), data, 0o644); err != nil {
		return releaseFile{}, err
	}
	return releaseFile{Name: name, URL: base + "/" + name, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}
