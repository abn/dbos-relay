package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

func recordStepExecution(ctx context.Context, dbURL, workflowID, stepName string) error {
	if dbURL == "" {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(timeoutCtx, dbURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(timeoutCtx) }()

	_, err = conn.Exec(timeoutCtx, `
		CREATE TABLE IF NOT EXISTS test_step_executions (
			workflow_id TEXT NOT NULL,
			step_name TEXT NOT NULL,
			executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		INSERT INTO test_step_executions (workflow_id, step_name, executed_at)
		VALUES ($1, $2, NOW());
	`, workflowID, stepName)
	return err
}

func helloWorkflow(ctx dbos.Context, name string) (string, error) {
	return fmt.Sprintf("Hello, %s!", name), nil
}

func orderWorkflow(ctx dbos.Context, orderID string) (string, error) {
	dbURL := os.Getenv("DBOS_SYSTEM_DATABASE_URL")
	wfID, err := ctx.GetWorkflowID()
	if err != nil {
		return "", err
	}

	// Step 1: record step 1 execution
	_, err = dbos.RunAsStep(ctx, func(stepCtx context.Context) (string, error) {
		_ = recordStepExecution(stepCtx, dbURL, wfID, "step1")
		return "step1-completed", nil
	})
	if err != nil {
		return "", err
	}

	// If victim role, sleep indefinitely until SIGKILL'd
	if os.Getenv("ROLE") == "victim" {
		time.Sleep(30 * time.Minute)
	}

	// Step 2: record step 2 execution
	_, err = dbos.RunAsStep(ctx, func(stepCtx context.Context) (string, error) {
		_ = recordStepExecution(stepCtx, dbURL, wfID, "step2")
		return "step2-completed", nil
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("order-%s-completed", orderID), nil
}

func main() {
	appName := os.Getenv("DBOS_APP_NAME")
	if appName == "" {
		appName = "golang-sample-app"
	}
	dbURL := os.Getenv("DBOS_SYSTEM_DATABASE_URL")

	if os.Getenv("ROLE") == "survivor" {
		time.Sleep(3 * time.Second)
	}

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
	dbos.RegisterWorkflow(dbosCtx, orderWorkflow, dbos.WithWorkflowName("orderWorkflow"))

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/trigger", func(w http.ResponseWriter, r *http.Request) {
		h, err := dbos.RunWorkflow(dbosCtx, orderWorkflow, "go-order")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"workflow_id": h.GetWorkflowID()})
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	go func() {
		_ = http.ListenAndServe(":"+port, nil)
	}()

	if err := dbos.Launch(dbosCtx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to launch DBOS: %v\n", err)
		os.Exit(1)
	}

	select {}
}
