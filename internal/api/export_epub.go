package api

import (
	"archive/zip"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type exportChapter struct {
	ChapterNo  int
	Title      string
	Paragraphs []string
}

func writeZipEntry(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create EPUB entry %s: %w", name, err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		return fmt.Errorf("write EPUB entry %s: %w", name, err)
	}
	return nil
}
func buildEPUB(dst io.Writer, slug, novelTitle, author string, chapters []exportChapter) error {
	return buildEPUBStream(dst, slug, novelTitle, author, func(yield func(exportChapter) error) error {
		for _, chapter := range chapters {
			if err := yield(chapter); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildEPUBStream(dst io.Writer, slug, novelTitle, author string, stream exportStream) error {
	zw := zip.NewWriter(dst)
	closed := false
	defer func() {
		if !closed {
			_ = zw.Close()
		}
	}()

	header := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mw, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create EPUB mimetype: %w", err)
	}
	if _, err := io.WriteString(mw, "application/epub+zip"); err != nil {
		return fmt.Errorf("write EPUB mimetype: %w", err)
	}

	containerXML := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`
	if err := writeZipEntry(zw, "META-INF/container.xml", containerXML); err != nil {
		return err
	}
	styleCSS := `body { font-family: 'Sarabun', sans-serif; line-height: 1.8; margin: 5%; color: #333; }
h1, h2 { text-align: center; color: #111; margin-bottom: 2em; }
p { text-indent: 1.5em; margin: 0.8em 0; }`
	if err := writeZipEntry(zw, "OEBPS/style.css", styleCSS); err != nil {
		return err
	}

	var chapters []exportChapter // Navigation retains titles only, never paragraph bodies.
	err = stream(func(ch exportChapter) error {
		idx := len(chapters)
		var paras strings.Builder
		for _, p := range ch.Paragraphs {
			fmt.Fprintf(&paras, "    <p>%s</p>\n", html.EscapeString(p))
		}
		content := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="th">
<head><title>%s</title><link rel="stylesheet" type="text/css" href="style.css"/></head>
<body><h2>%s</h2>
%s</body></html>`, html.EscapeString(ch.Title), html.EscapeString(ch.Title), paras.String())
		if err := writeZipEntry(zw, fmt.Sprintf("OEBPS/chapter_%d.xhtml", idx+1), content); err != nil {
			return err
		}
		chapters = append(chapters, exportChapter{ChapterNo: ch.ChapterNo, Title: ch.Title})
		return nil
	})
	if err != nil {
		return err
	}

	var manifest, spine strings.Builder
	for idx := range chapters {
		fmt.Fprintf(&manifest, "    <item id=\"ch_%d\" href=\"chapter_%d.xhtml\" media-type=\"application/xhtml+xml\"/>\n", idx+1, idx+1)
		fmt.Fprintf(&spine, "    <itemref idref=\"ch_%d\"/>\n", idx+1)
	}
	opf := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="BookId" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="BookId">urn:uuid:novelclaw-%s</dc:identifier>
    <dc:title>%s</dc:title><dc:language>th</dc:language><dc:creator>%s</dc:creator>
    <dc:publisher>NovelClaw AI</dc:publisher><meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
    <item id="style" href="style.css" media-type="text/css"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
%s  </manifest>
  <spine toc="ncx">
%s  </spine>
</package>`, html.EscapeString(slug), html.EscapeString(novelTitle), html.EscapeString(author),
		time.Now().UTC().Format("2006-01-02T15:04:05Z"), manifest.String(), spine.String())
	if err := writeZipEntry(zw, "OEBPS/content.opf", opf); err != nil {
		return err
	}

	var nav strings.Builder
	for idx, ch := range chapters {
		fmt.Fprintf(&nav, "    <navPoint id=\"navPoint-%d\" playOrder=\"%d\"><navLabel><text>%s</text></navLabel><content src=\"chapter_%d.xhtml\"/></navPoint>\n",
			idx+1, idx+1, html.EscapeString(ch.Title), idx+1)
	}
	ncx := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head><meta name="dtb:uid" content="urn:uuid:novelclaw-%s"/><meta name="dtb:depth" content="1"/></head>
  <docTitle><text>%s</text></docTitle>
  <navMap>
%s  </navMap>
</ncx>`, html.EscapeString(slug), html.EscapeString(novelTitle), nav.String())
	if err := writeZipEntry(zw, "OEBPS/toc.ncx", ncx); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize EPUB: %w", err)
	}
	closed = true
	return nil
}

func serveEPUBStream(w http.ResponseWriter, r *http.Request, fileName, slug, novelTitle, author string, stream exportStream) error {
	tmp, err := os.CreateTemp("", "novelclaw-*.epub")
	if err != nil {
		return fmt.Errorf("create EPUB temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := buildEPUBStream(tmp, slug, novelTitle, author, stream); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind EPUB temp file: %w", err)
	}
	info, err := tmp.Stat()
	if err != nil {
		return fmt.Errorf("stat EPUB temp file: %w", err)
	}

	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	http.ServeContent(w, r, fileName, info.ModTime(), tmp)
	return nil
}
