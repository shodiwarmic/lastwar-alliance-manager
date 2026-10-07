package fixtures

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

//go:embed img/*.png
var images embed.FS

//go:embed templates/cool.html
var coolHTML string

var coolTmpl = template.Must(template.New("cool").Parse(coolHTML))

// imageFor names the picture shown for a file: its kind and the theme, or the generic
// picture when the id is not a seeded file (a visitor's upload) or there is no picture
// of that kind.
func (s *server) imageFor(fileID int, theme string) string {
	if theme != "dark" {
		theme = "light"
	}
	for _, f := range s.manifest.Files {
		if f.ID == fileID {
			name := f.Kind + "-" + theme + ".png"
			if _, err := fs.Stat(images, "img/"+name); err == nil {
				return name
			}
		}
	}
	return "generic-" + theme + ".png"
}

// fileIDFrom reads the file id from WOPISrc, ".../wopi/files/<id>".
func fileIDFrom(wopiSrc string) int {
	u, err := url.Parse(wopiSrc)
	if err != nil {
		return 0
	}
	id, _ := strconv.Atoi(path.Base(strings.TrimSuffix(u.Path, "/")))
	return id
}

// collabora answers the app's form POST (and a GET, for a direct look) with an HTML page
// rather than a bare image, so the watermark, the caption and panning at phone width are
// plain CSS. It never calls back to the app's WOPI endpoints.
func (s *server) collabora(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	theme := r.URL.Query().Get("theme")
	img := s.imageFor(fileIDFrom(r.URL.Query().Get("WOPISrc")), theme)
	fa := s.cfg.FrameAncestors
	if fa == "" {
		fa = "'none'"
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors "+fa)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	coolTmpl.Execute(w, map[string]any{"Image": "/img/" + img, "Dark": theme == "dark"})
}

func (s *server) image(w http.ResponseWriter, r *http.Request) {
	b, err := images.ReadFile("img/" + path.Base(r.URL.Path))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(b)
}
