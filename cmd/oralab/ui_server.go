package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var webAssets embed.FS

type managerOperations interface {
	state(context.Context) managerState
	apply(context.Context, applyRequest) operationResult
	lifecycle(context.Context, lifecycleRequest) operationResult
}

type uiHandler struct {
	manager managerOperations
	csrf    string
	port    int
	static  http.Handler
}

func (a app) ui(root string, args []string) int {
	port, openBrowser, ok := parseUIOptions(args)
	if !ok {
		fmt.Fprintln(a.stderr, "usage: oralab ui [--port PORT] [--no-open]")
		return 2
	}
	csrf, err := randomToken()
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: create UI security token: %v\n", err)
		return 1
	}
	staticFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: load UI assets: %v\n", err)
		return 1
	}
	manager := newResourceManager(root, systemDockerRunner{})
	handler := &uiHandler{
		manager: manager,
		csrf:    csrf,
		port:    port,
		static:  http.FileServer(http.FS(staticFS)),
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: start local UI: %v\n", err)
		return 1
	}
	defer listener.Close()

	address := "http://127.0.0.1:" + strconv.Itoa(port)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      operationTimeout + time.Minute,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	fmt.Fprintf(a.stdout, "OraLab Resource Manager: %s\n", address)
	fmt.Fprintln(a.stdout, "The service is bound to this computer only. Press Ctrl+C to stop it.")
	if openBrowser {
		if err := launchBrowser(address); err != nil {
			fmt.Fprintf(a.stderr, "oralab: browser did not open automatically: %v\nOpen %s manually.\n", err, address)
		}
	}

	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveError := make(chan error, 1)
	go func() {
		serveError <- server.Serve(listener)
	}()
	select {
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(a.stderr, "oralab: UI server failed: %v\n", err)
			return 1
		}
	case <-stopContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			fmt.Fprintf(a.stderr, "oralab: UI shutdown failed: %v\n", err)
			return 1
		}
	}
	return 0
}

func (h *uiHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.setSecurityHeaders(writer)
	if !h.validHost(request.Host) {
		http.Error(writer, "invalid host", http.StatusForbidden)
		return
	}

	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/":
		h.serveIndex(writer)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/assets/"):
		request.URL.Path = strings.TrimPrefix(request.URL.Path, "/assets")
		h.static.ServeHTTP(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/api/state":
		stateContext, cancel := context.WithTimeout(request.Context(), stateTimeout)
		defer cancel()
		h.writeJSON(writer, http.StatusOK, h.manager.state(stateContext))
	case request.Method == http.MethodPost && request.URL.Path == "/api/apply":
		if !h.authorizeMutation(writer, request) {
			return
		}
		var payload applyRequest
		if !decodeJSON(writer, request, &payload) {
			return
		}
		result := h.manager.apply(request.Context(), payload)
		status := http.StatusOK
		if result.RequiresRestart {
			status = http.StatusConflict
		} else if !result.OK {
			status = http.StatusBadRequest
		}
		h.writeJSON(writer, status, result)
	case request.Method == http.MethodPost && request.URL.Path == "/api/lifecycle":
		if !h.authorizeMutation(writer, request) {
			return
		}
		var payload lifecycleRequest
		if !decodeJSON(writer, request, &payload) {
			return
		}
		result := h.manager.lifecycle(request.Context(), payload)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusBadRequest
		}
		h.writeJSON(writer, status, result)
	default:
		http.NotFound(writer, request)
	}
}

func (h *uiHandler) serveIndex(writer http.ResponseWriter) {
	contents, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		http.Error(writer, "UI unavailable", http.StatusInternalServerError)
		return
	}
	page := strings.ReplaceAll(string(contents), "__ORALAB_CSRF_TOKEN__", h.csrf)
	http.SetCookie(writer, &http.Cookie{
		Name:     "oralab_session",
		Value:    h.csrf,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   0,
	})
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write([]byte(page))
}

func (h *uiHandler) authorizeMutation(writer http.ResponseWriter, request *http.Request) bool {
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		http.Error(writer, "application/json required", http.StatusUnsupportedMediaType)
		return false
	}
	if origin := request.Header.Get("Origin"); origin != "" && !h.validOrigin(origin) {
		http.Error(writer, "invalid origin", http.StatusForbidden)
		return false
	}
	cookie, err := request.Cookie("oralab_session")
	if err != nil || cookie.Value != h.csrf || request.Header.Get("X-OraLab-CSRF") != h.csrf {
		http.Error(writer, "invalid request token", http.StatusForbidden)
		return false
	}
	return true
}

func (h *uiHandler) validHost(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		host = value
		port = ""
	}
	if port != "" && port != strconv.Itoa(h.port) {
		return false
	}
	return host == "127.0.0.1" || strings.EqualFold(host, "localhost")
}

func (h *uiHandler) validOrigin(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || !h.validHost(parsed.Host) {
		return false
	}
	return parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func (h *uiHandler) setSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

func (h *uiHandler) writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, 8*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		http.Error(writer, "invalid JSON request", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(writer, "request must contain one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

func parseUIOptions(args []string) (int, bool, bool) {
	port := 8765
	openBrowser := true
	for len(args) > 0 {
		switch args[0] {
		case "--no-open":
			openBrowser = false
			args = args[1:]
		case "--port":
			if len(args) < 2 {
				return 0, false, false
			}
			parsed, err := strconv.Atoi(args[1])
			if err != nil || parsed < 1024 || parsed > 65535 {
				return 0, false, false
			}
			port = parsed
			args = args[2:]
		default:
			return 0, false, false
		}
	}
	return port, openBrowser, true
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func launchBrowser(address string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	case "darwin":
		command = exec.Command("open", address)
	default:
		command = exec.Command("xdg-open", address)
	}
	return command.Start()
}
