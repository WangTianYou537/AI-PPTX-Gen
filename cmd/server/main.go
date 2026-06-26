package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"wty5.cn/ppt-gen/internal/httpapi"
	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/pptx"
	"wty5.cn/ppt-gen/internal/store"
)

func main() {
	addrFlag := flag.String("addr", envDefault("ADDR", ":8080"), "HTTP listen address")
	port := flag.String("port", envDefault("PORT", ""), "HTTP listen port, overrides -addr when set")
	debug := flag.Bool("debug", envBool("DEBUG"), "enable debug logs for upstream LLM requests and responses")
	storeKind := flag.String("store", "", "storage backend: json, sqlite, postgres")
	dsn := flag.String("dsn", "", "storage DSN or file path")
	dataPath := flag.String("data", "", "local data file path for json/sqlite")
	storageConfig := flag.String("storage-config", store.DefaultConfigPath(), "storage config file path")
	flag.Parse()

	addr := resolveAddr(*addrFlag, *port)
	llm.SetDebug(*debug)
	pptx.SetDebug(*debug)
	httpapi.SetDebug(*debug)

	manager, err := openStoreManager(*storeKind, *dsn, *dataPath, *storageConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer manager.Close()

	log.Printf("PPT generator listening on %s", addr)
	if *debug {
		log.Printf("debug logging enabled")
	}
	if err := http.ListenAndServe(addr, httpapi.NewServerWithStoreManager(manager)); err != nil {
		log.Fatal(err)
	}
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func resolveAddr(addr, port string) string {
	if port == "" {
		return addr
	}
	port = strings.TrimPrefix(strings.TrimSpace(port), ":")
	if port == "" {
		return addr
	}
	return ":" + port
}

func openStoreManager(kind, dsn, dataPath, configPath string) (*httpapi.StoreManager, error) {
	if kind != "" || dsn != "" || dataPath != "" {
		cfg := store.Config{Kind: kind, DSN: dsn, Path: dataPath}
		if cfg.Kind == "" {
			cfg.Kind = store.StorageJSON
		}
		appStore, err := store.OpenConfiguredStore(nilContext(), cfg)
		if err != nil {
			return nil, err
		}
		return httpapi.NewStoreManager(appStore, store.NormalizeConfig(cfg), configPath), nil
	}
	cfg, ok, err := store.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return httpapi.NewStoreManager(nil, store.Config{}, configPath), nil
	}
	appStore, err := store.OpenConfiguredStore(nilContext(), cfg)
	if err != nil {
		return nil, err
	}
	return httpapi.NewStoreManager(appStore, store.NormalizeConfig(cfg), configPath), nil
}

func nilContext() context.Context { return context.Background() }
