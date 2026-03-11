package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/amin/mesh-tenant-limiter/internal/sim"
)

func main() {
	report, err := sim.Run()
	if err != nil {
		log.Fatalf("run demo: %v", err)
	}

	fmt.Println("Mesh Tenant Limiter Demo")
	fmt.Println(strings.Repeat("=", 25))
	fmt.Printf("Generated: %s\n\n", report.GeneratedAt.Format("2006-01-02 15:04:05 MST"))

	for _, scenario := range report.Scenarios {
		fmt.Printf("%s\n", scenario.Name)
		fmt.Printf("Goal: %s\n", scenario.Goal)
		for _, strategy := range scenario.Strategies {
			fmt.Printf("- %s: allowed=%d denied=%d\n", strategy.Name, strategy.Allowed, strategy.Denied)
			for _, note := range strategy.Notes {
				fmt.Printf("  note: %s\n", note)
			}
		}
		for _, observation := range scenario.Observations {
			fmt.Printf("  observation: %s\n", observation)
		}
		fmt.Println()
	}
}
