package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
)

type Handler struct {
	db *DB
}

func NewHandler(networkName string) *Handler {
	return &Handler{
		NewDB(networkName),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		switch r.URL.Path {
		case "/config":
			h.serveGetConfig(w, r)
		case "/health":
			h.serveGetHealth(w, r)
		default:
			h.serveGetAsset(w, r)
		}
	case "POST":
		switch r.URL.Path {
		case "/config":
			h.servePostConfig(w, r)
		default:
			http.Error(w, fmt.Sprintf("unhandled POST path %s", r.URL.Path), http.StatusNotFound)
		}
	}
}

func (h *Handler) serveGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	_, err := w.Write([]byte("{}"))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) serveGetHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	_, err := w.Write([]byte("{}"))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) serveGetAsset(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path

	if encodedContent, ok := embeddedFiles[p]; ok {
		mimeType := deriveMimeTypeFromExt(p)

		w.Header().Set("Content-Type", mimeType)

		content, err := base64.RawStdEncoding.DecodeString(encodedContent)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if _, err = w.Write(content); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	} else {
		h.serveGetDefaultAsset(w, r)
	}
}

func (h *Handler) serveGetDefaultAsset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	content, err := base64.RawStdEncoding.DecodeString(embeddedFiles["/index.html"])
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(content); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) servePostConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	_, err := w.Write([]byte("{}"))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func deriveMimeTypeFromExt(p string) string {
	ext := filepath.Ext(p)

	switch ext {
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".otf":
		return "font/otf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".txt":
		return "text/plain"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".wasm":
		return "application/wasm"
	default:
		return "application/octet-stream"
	}	
}