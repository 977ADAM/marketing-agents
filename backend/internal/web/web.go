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
		// директория не считается файлом — иначе отдался бы листинг вместо SPA.
		if info, err := fs.Stat(sub, p[1:]); err != nil || info.IsDir() {
			// нет файла → отдаём index.html
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
