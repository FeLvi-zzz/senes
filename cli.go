package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func serverURL() string {
	if u := os.Getenv("SENES_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://" + defaultAddr
}

func api(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, serverURL()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach senes server at %s (start it with `senes serve`): %w", serverURL(), err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct{ Error string }
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return errors.New(e.Error)
	}
	if out != nil && res.StatusCode != http.StatusNoContent {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

// parseInterspersed parses flags that may appear before or after positional args.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// readArg returns s, or stdin contents when s is "-".
func readArg(s string) (string, error) {
	if s != "-" {
		return s, nil
	}
	b, err := io.ReadAll(os.Stdin)
	return strings.TrimRight(string(b), "\n"), err
}

func parseID(args []string) (int, error) {
	if len(args) != 1 {
		return 0, errors.New("expected exactly one task id")
	}
	id, err := strconv.Atoi(strings.TrimPrefix(args[0], "#"))
	if err != nil {
		return 0, fmt.Errorf("invalid id %q", args[0])
	}
	return id, nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	all := fs.Bool("a", false, "include done tasks")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	var tasks []TaskView
	if err := api("GET", "/api/tasks", nil, &tasks); err != nil {
		return err
	}
	filter := map[string]bool{}
	for _, s := range pos {
		if !validStatus(s) {
			return fmt.Errorf("invalid status %q (want one of %v)", s, Statuses)
		}
		filter[s] = true
	}
	first := true
	for _, st := range Statuses {
		if len(filter) > 0 && !filter[st] {
			continue
		}
		if len(filter) == 0 && st == "done" && !*all {
			continue
		}
		var group []TaskView
		for _, t := range tasks {
			if t.Status == st {
				group = append(group, t)
			}
		}
		if len(group) == 0 {
			continue
		}
		if !first {
			fmt.Println()
		}
		first = false
		fmt.Printf("%s (%d)\n", strings.ToUpper(st), len(group))
		for _, t := range group {
			fmt.Printf("  %4d  %s\n", t.ID, t.Title)
		}
	}
	return nil
}

func printTask(t TaskView) {
	fmt.Printf("#%d  [%s]  %s\n", t.ID, t.Status, t.Title)
	if t.Description != "" {
		fmt.Println()
		fmt.Println(t.Description)
	}
}

func cmdShow(args []string) error {
	id, err := parseID(args)
	if err != nil {
		return err
	}
	var t TaskView
	if err := api("GET", fmt.Sprintf("/api/tasks/%d", id), nil, &t); err != nil {
		return err
	}
	printTask(t)
	return nil
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	desc := fs.String("d", "", "description (markdown, \"-\" to read stdin)")
	status := fs.String("s", "todo", "status: doing|todo|waiting|backlog|done")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	title := strings.Join(pos, " ")
	if title == "" {
		return errors.New("usage: senes add <title> [-d description] [-s status]")
	}
	d, err := readArg(*desc)
	if err != nil {
		return err
	}
	var t TaskView
	if err := api("POST", "/api/tasks", TaskPatch{Title: &title, Description: &d, Status: status}, &t); err != nil {
		return err
	}
	fmt.Printf("created #%d\n", t.ID)
	return nil
}

func cmdEdit(args []string) error {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	var p TaskPatch
	fs.Func("t", "new title", func(s string) error { p.Title = &s; return nil })
	fs.Func("d", "new description (\"-\" to read stdin)", func(s string) error {
		v, err := readArg(s)
		p.Description = &v
		return err
	})
	fs.Func("s", "new status", func(s string) error { p.Status = &s; return nil })
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := parseID(pos)
	if err != nil {
		return err
	}
	if p.Title == nil && p.Description == nil && p.Status == nil {
		return errors.New("nothing to change: pass -t, -d or -s")
	}
	var t TaskView
	if err := api("PATCH", fmt.Sprintf("/api/tasks/%d", id), p, &t); err != nil {
		return err
	}
	printTask(t)
	return nil
}

func cmdMove(args []string) error {
	const usage = "usage: senes mv <id> [status] [-top | -bottom | -before <id> | -after <id>]"
	fs := flag.NewFlagSet("mv", flag.ExitOnError)
	var p TaskPatch
	top := fs.Bool("top", false, "move to the top of its column")
	bottom := fs.Bool("bottom", false, "move to the bottom of its column")
	anchor := func(dst **int) func(string) error {
		return func(s string) error {
			id, err := parseID([]string{s})
			*dst = &id
			return err
		}
	}
	fs.Func("before", "place before task <id>", anchor(&p.Before))
	fs.Func("after", "place after task <id>", anchor(&p.After))
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if *top && *bottom {
		return errors.New(usage)
	}
	if *top {
		v := "top"
		p.Position = &v
	} else if *bottom {
		v := "bottom"
		p.Position = &v
	}
	if len(pos) == 2 {
		p.Status = &pos[1]
		pos = pos[:1]
	}
	if len(pos) != 1 || (p.Status == nil && p.Before == nil && p.After == nil && p.Position == nil) {
		return errors.New(usage)
	}
	id, err := parseID(pos)
	if err != nil {
		return err
	}
	var t TaskView
	if err := api("PATCH", fmt.Sprintf("/api/tasks/%d", id), p, &t); err != nil {
		return err
	}
	fmt.Printf("#%d → %s\n", t.ID, t.Status)
	return nil
}

func cmdRemove(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: senes rm <id>...")
	}
	for _, a := range args {
		id, err := parseID([]string{a})
		if err != nil {
			return err
		}
		if err := api("DELETE", fmt.Sprintf("/api/tasks/%d", id), nil, nil); err != nil {
			return err
		}
		fmt.Printf("deleted #%d\n", id)
	}
	return nil
}
