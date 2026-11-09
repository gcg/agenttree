package main

import (
	"fmt"
	"os"

	"github.com/gcg/agenttree/internal/config"
)

func main() {
	cfg, _, err := config.Load(config.GlobalConfigPath(), "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("agenttree initialized (default layout: %s, default agent: %s)\n", cfg.Layout, cfg.DefaultAgent)
}
