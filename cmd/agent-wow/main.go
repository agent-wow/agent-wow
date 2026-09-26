package main

import (
	"fmt"
	"log"

	"github.com/hazim-j/agent-wow/internal/config"
)

func main() {
	if err := config.Init(); err != nil {
		log.Fatalf("initialize config: %v", err)
	}

	fmt.Println("TODO: implement")
}
