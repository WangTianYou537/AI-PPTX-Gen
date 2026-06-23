package main

import (
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
	storeKind := flag.String("store", "json", "storage backend: json, sqlite, postgres, mysql")
	dsn := flag.String("dsn", "", "storage DSN or file path")
	dataPath := flag.String("data", "", "local data file path for json/sqlite")
	flag.Parse()

	addr := resolveAddr(*addrFlag, *port)
	llm.SetDebug(*debug)
	pptx.SetDebug(*debug)

	appStore, err := openStore(*storeKind, *dsn, *dataPath)
	if err != nil {
		log.Fatal(err)
	}
	defer appStore.Close()

	log.Printf("PPT generator listening on %s", addr)
	if *debug {
		log.Printf("debug logging enabled")
	}
	if err := http.ListenAndServe(addr, httpapi.NewServer(appStore)); err != nil {
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

func openStore(kind, dsn, dataPath string) (store.Store, error) {
	if dsn == "" {
		dsn = dataPath
	}
	switch kind {
	case "", "json":
		return store.NewJSONStore(dsn)
	case "sqlite":
		return store.NewSQLStore("sqlite", dsn, "sqlite")
	case "postgres":
		return store.NewSQLStore("postgres", dsn, "postgres")
	case "mysql":
		return store.NewSQLStore("mysql", dsn, "mysql")
	default:
		return nil, store.ErrInvalidStore
	}
}
