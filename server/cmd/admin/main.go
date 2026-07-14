package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/adminapi"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func main() {
	cfg := mailcfg.Load()
	st, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	h := &adminapi.Handler{
		Cfg:      cfg,
		Store:    st,
		Settings: settings.NewManager(st),
		Queue:    st,
	}
	api := adminapi.NewRouter(h)
	uiDir := strings.TrimSpace(os.Getenv("ADMIN_UI_DIR"))
	var handler http.Handler = api
	if uiDir != "" {
		fs := http.FileServer(http.Dir(uiDir))
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
				api.ServeHTTP(w, r)
				return
			}
			clean := filepath.Clean(r.URL.Path)
			path := filepath.Join(uiDir, clean)
			if r.URL.Path == "/" || !fileExists(path) {
				http.ServeFile(w, r, filepath.Join(uiDir, "index.html"))
				return
			}
			fs.ServeHTTP(w, r)
		})
	}
	log.Printf("admin api listening on %s", cfg.AdminAddr)
	log.Fatal(http.ListenAndServe(cfg.AdminAddr, handler))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
