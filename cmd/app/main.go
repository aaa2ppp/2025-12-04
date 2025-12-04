package main

import (
	"log"

	"link-checker/internal/app"
	"link-checker/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	app := app.App{}

	if err := app.Run(cfg); err != nil {
		log.Fatal(err)
	}
}
