// Package web вшивает собранный SPA (frontend) и отдаёт его с SPA-fallback.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler отдаёт статику из dist/. Любой путь, для которого нет файла,
// отдаёт index.html (клиентский роутинг SPA).
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		// есть ли такой файл? (без ведущего слэша для fs.Stat)
		if _, err := fs.Stat(sub, p[1:]); err != nil {
			// нет файла → отдаём index.html
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
