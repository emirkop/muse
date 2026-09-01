package main

import (
	"fmt"
	"os"

	"muse-backend/internal/platform/observability"
)

func main() {
	if _, err := fmt.Fprint(os.Stdout, observability.PrometheusRules()); err != nil {
		fmt.Fprintln(os.Stderr, "alertrules:", err)
		os.Exit(1)
	}
}
