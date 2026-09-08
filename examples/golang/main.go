package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

func helloWorkflow(ctx dbos.Context, name string) (string, error) {
	return fmt.Sprintf("Hello, %s!", name), nil
}

func main() {
	appName := os.Getenv("DBOS_APP_NAME")
	if appName == "" {
		appName = "go-sample-app"
	}
	dbURL := os.Getenv("DBOS_SYSTEM_DATABASE_URL")

	dbosCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:         appName,
		DatabaseURL:     dbURL,
		ConductorURL:    os.Getenv("RELAY_URL"),
		ConductorAPIKey: os.Getenv("RELAY_API_KEY"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize DBOS context: %v\n", err)
		os.Exit(1)
	}

	dbos.RegisterWorkflow(dbosCtx, helloWorkflow, dbos.WithWorkflowName("helloWorkflow"))

	if err := dbos.Launch(dbosCtx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to launch DBOS: %v\n", err)
		os.Exit(1)
	}

	select {}
}
