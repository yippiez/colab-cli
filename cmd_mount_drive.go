package main

import (
	"context"
	"fmt"
	"time"
)

func runMountDrive(args []string) error {
	session := getFlagValue(args, "--session", "")
	authUser := getFlagValue(args, "--authuser", "0")

	if session == "" {
		return fmt.Errorf("usage: colab mount-drive --session <id>")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	tok, err := getToken(ctx)
	if err != nil {
		return err
	}

	client := NewColabClient(tok.AccessToken, authUser)

	fmt.Printf("Connecting to session %s...\n", session)
	rt, err := client.ResumeRuntime(ctx, "t4", false, session)
	if err != nil {
		return fmt.Errorf("connect runtime: %w", err)
	}

	fmt.Println("Connecting to kernel...")
	kc, err := NewKernelClient(ctx, rt)
	if err != nil {
		return fmt.Errorf("connect kernel: %w", err)
	}
	defer kc.Close()

	fmt.Println("Running drive mount flow...")
	_, err = kc.MountDrive(ctx, client, rt.Endpoint, func(stream, text string) {
		if stream == "stderr" {
			fmt.Print(text)
		} else {
			fmt.Print(text)
		}
	})
	if err != nil {
		return err
	}

	fmt.Println("Google Drive mounted at /content/drive")
	return nil
}
