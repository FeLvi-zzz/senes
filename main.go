package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const defaultAddr = "127.0.0.1:7878"

const usage = `senes — a minimal todo server

Server:
  senes serve [-addr 127.0.0.1:7878] [-db path]

Client (talks to $SENES_URL, default http://127.0.0.1:7878):
  senes ls [-a] [status...]            list tasks (done hidden unless -a or given)
  senes add <title> [-d desc] [-s st]  create a task (default status: todo)
  senes show <id>                      show a task
  senes edit <id> [-t title] [-d desc] [-s status]
  senes mv <id> [status] [-top|-bottom|-before <id>|-after <id>]
                                       change status and/or position
  senes rm <id>...                     delete tasks

Status: doing | todo | waiting | backlog | done
Use "-d -" to read the description from stdin.
`

func defaultDB() string {
	if p := os.Getenv("SENES_DB"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "senes", "tasks.json")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", defaultAddr, "listen address")
		db := fs.String("db", defaultDB(), "path to the JSON data file")
		_ = fs.Parse(args)
		err = serve(*addr, *db)
	case "ls", "list":
		err = cmdList(args)
	case "add", "new":
		err = cmdAdd(args)
	case "show":
		err = cmdShow(args)
	case "edit":
		err = cmdEdit(args)
	case "mv", "move":
		err = cmdMove(args)
	case "rm", "delete":
		err = cmdRemove(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
