package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func runLogout(args []string) error {
	jsonOutput := hasFlag(args, "--json")

	tok, err := loadCachedToken()
	if err != nil {
		return err
	}
	if tok == nil {
		if jsonOutput {
			out := map[string]interface{}{
				"status":  "not_authenticated",
				"message": "No cached authentication found",
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Println("No cached authentication found.")
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := revokeToken(ctx, tok); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if err := clearTokenCache(); err != nil {
		return fmt.Errorf("clear token cache: %w", err)
	}

	if jsonOutput {
		out := map[string]interface{}{
			"status": "logged_out",
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Println("Logged out successfully.")
	}

	return nil
}
