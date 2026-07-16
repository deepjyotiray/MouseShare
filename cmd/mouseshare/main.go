package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"mouseshare/internal/app"
	"mouseshare/internal/ui"
)

func main() {
	logger := log.New(os.Stdout, "[mouseshare] ", log.LstdFlags|log.Lmsgprefix)

	baseDir, err := resolveBaseDir()
	if err != nil {
		logger.Fatal(err)
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

	go func() {
		logger.Printf("ui available at %s", url)
		if err := openBrowser(url); err != nil {
			logger.Printf("open browser manually: %s", url)
		}
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Printf("ui server stopped: %v", err)
		}
	}()

	waitForShutdown(logger, httpServer)
}

func resolveBaseDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(cfg, "MouseShare")
	return path, os.MkdirAll(path, 0o755)
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
