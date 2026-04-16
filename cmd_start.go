package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func runStart(args []string) error {
	jsonOutput := hasFlag(args, "--json")
	gpu := getFlagValue(args, "--gpu", "t4")
	authUser := getFlagValue(args, "--authuser", "0")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tok, err := getToken(ctx)
	if err != nil {
		return err
	}

	client := NewColabClient(tok.AccessToken, authUser)

	if !jsonOutput {
		fmt.Printf("Requesting %s GPU runtime...\n", strings.ToUpper(gpu))
	}

	rt, err := client.AssignRuntime(ctx, gpu)
	if err != nil {
		return fmt.Errorf("assign runtime: %w", err)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(startOutput(rt), "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Printf("Runtime started: %s\n", rt.Accelerator)
		fmt.Printf("Session: %s\n", rt.Endpoint)
		fmt.Println("\nUse this session with other commands:")
		fmt.Printf("  colab exec --session %s script.py\n", rt.Endpoint)
		fmt.Printf("  colab upload --session %s data.tar.gz\n", rt.Endpoint)
		fmt.Printf("  colab stop\n")
	}

	return nil
}
