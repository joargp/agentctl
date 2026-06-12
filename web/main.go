package main

import (
	"flag"
	"log"

	"github.com/joargp/agentctl/internal/dashboard"
)

func main() {
	port := flag.Int("port", 8080, "port to run the server on")
	flag.Parse()

	if err := dashboard.Run(*port); err != nil {
		log.Fatal(err)
	}
}
