package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"
	_ "modernc.org/sqlite"

	"github.com/yxwyoyoyo/session-try/internal/index"
	"github.com/yxwyoyoyo/session-try/internal/provider"
	"github.com/yxwyoyoyo/session-try/internal/session"
	"github.com/yxwyoyoyo/session-try/internal/ui"
)

var version = "dev"

type options struct {
	provider string
	cwd      bool
	limit    int
	json     bool
	refresh  bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "session-try:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	indexPath := filepath.Join(cache, "session-try", "index.db")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", indexPath)
	if err != nil {
		return err
	}
	store, err := index.Open(db)
	if err != nil {
		db.Close()
		return fmt.Errorf("open index: %w", err)
	}
	defer store.Close()

	openReadOnly := func(path string) (*sql.DB, error) {
		return sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	}
	providers := []provider.Provider{
		provider.NewClaude(home),
		provider.NewCodex(home),
		provider.NewOpenCode(home, openReadOnly),
		provider.NewKiro(home),
	}

	if len(args) > 0 {
		switch args[0] {
		case "index":
			return runIndex(args[1:], store, providers, stdout, stderr)
		case "doctor":
			return runDoctor(store, providers, indexPath, stdout)
		case "version", "--version", "-v":
			fmt.Fprintln(stdout, version)
			return nil
		case "help", "--help", "-h":
			printHelp(stdout)
			return nil
		}
	}

	flags := flag.NewFlagSet("session-try", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opt options
	flags.StringVar(&opt.provider, "provider", "", "only show one provider")
	flags.BoolVar(&opt.cwd, "cwd", false, "only show sessions from the current directory")
	flags.IntVar(&opt.limit, "limit", 50, "maximum number of results")
	flags.BoolVar(&opt.json, "json", false, "print matches as JSON instead of opening the picker")
	flags.BoolVar(&opt.refresh, "refresh", true, "refresh changed sessions before searching")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opt.refresh {
		if err := refresh(ctx, store, providers, stderr); err != nil {
			fmt.Fprintln(stderr, "session-try: refresh warning:", err)
		}
	}
	filters := index.Filters{Provider: opt.provider, Limit: opt.limit}
	if opt.cwd {
		filters.CWD, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	query := strings.Join(flags.Args(), " ")
	if opt.json || !term.IsTerminal(int(os.Stdin.Fd())) {
		matches, err := store.Search(ctx, query, filters)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(matches)
	}

	ui.SetHome(home)
	chosen, err := ui.Choose(store, query, filters)
	if err != nil || chosen == nil {
		return err
	}
	return resume(providers, *chosen)
}

func refresh(ctx context.Context, store *index.Store, providers []provider.Provider, stderr io.Writer) error {
	var failures []error
	for _, p := range providers {
		if !p.Available() {
			continue
		}
		known, err := store.Known(p.Name())
		if err != nil {
			failures = append(failures, fmt.Errorf("%s index: %w", p.Name(), err))
			continue
		}
		sessions, err := p.Discover(ctx, known)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", p.Name(), err))
			continue
		}
		if err := store.Upsert(ctx, sessions); err != nil {
			failures = append(failures, fmt.Errorf("%s save: %w", p.Name(), err))
			continue
		}
		if len(sessions) > 0 {
			fmt.Fprintf(stderr, "Indexed %d changed %s sessions\n", len(sessions), p.Name())
		}
	}
	return errors.Join(failures...)
}

func resume(providers []provider.Provider, chosen session.Session) error {
	if info, err := os.Stat(chosen.Directory); err != nil || !info.IsDir() {
		return fmt.Errorf("original directory no longer exists: %s", chosen.Directory)
	}
	for _, p := range providers {
		if p.Name() != chosen.Provider {
			continue
		}
		cmd, err := p.ResumeCommand(chosen)
		if err != nil {
			return err
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("unknown provider %q", chosen.Provider)
}

func runIndex(args []string, store *index.Store, providers []provider.Provider, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("session-try index", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rebuild := flags.Bool("rebuild", false, "discard and recreate all indexed data")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rebuild {
		if err := store.Rebuild(); err != nil {
			return err
		}
	}
	if err := refresh(context.Background(), store, providers, stderr); err != nil {
		return err
	}
	counts, err := store.Counts()
	if err != nil {
		return err
	}
	for _, name := range index.ProviderNames(counts) {
		fmt.Fprintf(stdout, "%s\t%d\n", name, counts[name])
	}
	return nil
}

func runDoctor(store *index.Store, providers []provider.Provider, indexPath string, stdout io.Writer) error {
	counts, err := store.Counts()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "index\t%s\n", indexPath)
	for _, p := range providers {
		status := "unavailable"
		if p.Available() {
			status = "available"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%d indexed\n", p.Name(), status, counts[p.Name()])
	}
	return nil
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, `session-try finds AI harness sessions by what you remember.

Usage:
  session-try [options] [query]
  session-try index [--rebuild]
  session-try doctor

Options:
  --provider NAME   limit to claude, codex, opencode, or kiro
  --cwd             limit to the current directory
  --limit N         maximum results (default 50)
  --json            print results without opening the picker
  --refresh=false   search the existing index without refreshing`)
}
