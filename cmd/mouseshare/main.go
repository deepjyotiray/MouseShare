package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"mouseshare/internal/app"
	"mouseshare/internal/ui"
)

func main() {
	baseDir, err := resolveBaseDir()
	if err != nil {
		log.Fatal(err)
	}
	lock, err := acquireInstanceLock()
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()

	logger, err := newLogger(baseDir)
	if err != nil {
		log.Fatal(err)
	}
	service, err := app.New(baseDir, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer service.Close()

	if err := service.Start(); err != nil {
		logger.Fatal(err)
	}

	uiServer := ui.New(service)
	handler, err := uiServer.Handler()
	if err != nil {
		logger.Fatal(err)
	}
	httpServer := &http.Server{Handler: handler}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Fatal(err)
	}
	url := fmt.Sprintf("http://%s", ln.Addr().String())
	service.SetHTTPBaseURL(url)
	if err := publishUIURL(baseDir, url); err != nil {
		logger.Printf("publish ui url failed: %v", err)
	}

	go func() {
		logger.Printf("ui available at %s", url)
		if shouldOpenBrowser() {
			if err := openBrowser(url); err != nil {
				logger.Printf("open browser manually: %s", url)
			}
		}
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Printf("ui server stopped: %v", err)
		}
	}()

	waitForShutdown(logger, httpServer)
}

func acquireInstanceLock() (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:41092")
	if err != nil {
		return nil, fmt.Errorf("another MouseShare instance is already running on this machine; quit it before starting a new one")
	}
	return ln, nil
}

func newLogger(baseDir string) (*log.Logger, error) {
	logPath := filepath.Join(baseDir, "mouseshare.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	writer := io.MultiWriter(os.Stdout, file)
	logger := log.New(writer, "[mouseshare] ", log.LstdFlags|log.Lmsgprefix)
	logger.Printf("logging to %s", logPath)
	return logger, nil
}

func resolveBaseDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(cfg, "MouseShare")
	return path, os.MkdirAll(path, 0o755)
}

func publishUIURL(baseDir, url string) error {
	target := os.Getenv("MOUSESHARE_UI_URL_FILE")
	if strings.TrimSpace(target) == "" {
		target = filepath.Join(baseDir, "ui-url.txt")
	}
	return os.WriteFile(target, []byte(url+"\n"), 0o644)
}

func shouldOpenBrowser() bool {
	return os.Getenv("MOUSESHARE_NO_AUTO_OPEN") == ""
}

func waitForShutdown(logger *log.Logger, server *http.Server) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	logger.Println("shutdown complete")
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}
