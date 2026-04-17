package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "auth":
		err = runAuth(args)
	case "start":
		err = runStart(args)
	case "exec":
		err = runExec(args)
	case "upload":
		err = runUpload(args)
	case "download":
		err = runDownload(args)
	case "mount-drive":
		err = runMountDrive(args)
	case "quota":
		err = runQuota(args)
	case "status":
		err = runStatus(args)
	case "stop":
		err = runStop(args)
	case "logout":
		err = runLogout(args)
	case "version", "--version", "-v":
		fmt.Printf("colab %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Usage: colab <command> [options]

Commands:
  auth                  Authenticate with Google (OAuth2 browser flow)
  start                 Start a Colab runtime and print the session ID
  exec <file>           Execute .py or .ipynb on Colab
  exec -c "code"        Execute inline Python code on Colab
  upload <local> [remote]   Upload file to the Colab runtime
  download <remote> [local] Download file from the Colab runtime
  mount-drive           Mount Google Drive in an existing Colab session
  quota                 Show GPU quota, CCU balance, eligible accelerators
  status                Show runtime info (accelerator, memory, idle time)
  stop                  Release the Colab runtime
  logout                Revoke cached auth and delete the local token cache

Options:
  --json                Machine-readable JSON output
  --gpu t4|l4|a100      GPU type to request (default: t4)
  --cpu                 Request a CPU runtime instead of a GPU
  --timeout 30m         Execution timeout (default: 30m)
  --session <id>        Use a specific runtime session
  --authuser <value>    Google authuser value (default: 0)
  -h, --help            Show this help
  -v, --version         Show version

Examples:
  colab exec train.py                          # one-shot: assign, run, release
  colab exec --gpu a100 train.py               # one-shot with A100
  colab exec --cpu -c "print('hello from cpu')"

  # Long-running session:
  colab start --gpu t4                         # → prints GPU session ID
  colab start --cpu                            # → prints CPU session ID
  colab upload --session <id> data.tar.gz      # upload to that runtime
  colab exec --session <id> train.py           # run on that runtime
  colab mount-drive --session <id>             # print auth URL and mount Drive
  colab download --session <id> model.bin      # download results
  colab stop                                   # release when done
`)
}
