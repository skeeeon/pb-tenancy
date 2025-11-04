package main

import (
	"log"
	
	"github.com/pocketbase/pocketbase"
	"github.com/skeeeon/pb-tenancy"
)

func main() {
	app := pocketbase.New()
	
	// Setup multi-tenancy with default options
	if err := pbtenancy.Setup(app, pbtenancy.DefaultOptions()); err != nil {
		log.Fatalf("Failed to setup tenancy: %v", err)
	}
	
	// Start PocketBase as usual
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
