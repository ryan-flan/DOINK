// Command doink watches DOINK's SavedVariables files and posts new events
// to Discord.
//
// On Windows it runs as a tray app by default; elsewhere, and with --once,
// --console, --dry-run, --test-webhook or `doink test <type>`, it is a
// console program. `doink test <type>... [url]` posts the same fake events
// as the in-game /doink test, without the game. `doink decode <image>...`
// reads the realtime code in screenshots and changes nothing. During
// development pass --config: `go run` builds into a temporary folder, so
// the default (config.toml next to the executable) isn't the repo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/runner"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/state"
	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

// optionalString is a flag that may be given bare (--test-webhook) or
// with a value (--test-webhook=URL, or --test-webhook URL: see main).
type optionalString struct {
	set   bool
	value string
}

func (o *optionalString) String() string { return o.value }
func (o *optionalString) Set(v string) error {
	o.set = true
	if v != "true" {
		o.value = v
	}
	return nil
}
func (o *optionalString) IsBoolFlag() bool { return true }

type options struct {
	config      string
	dryRun      bool
	once        bool
	console     bool
	tray        bool
	testWebhook optionalString
	testEvents  bool     // doink test <type>... [url]
	testArgs    []string // its arguments
	decode      []string // doink decode <image>...
	verbose     bool
	updatedFrom string // set by the self-update: the version it replaced
}

// appDir is where config.toml, state.json and doink.log live: next to
// the executable.
func appDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func main() { os.Exit(run()) }

func run() int {
	var o options
	fs := flag.NewFlagSet("doink", flag.ContinueOnError)
	fs.StringVar(&o.config, "config", filepath.Join(appDir(), "config.toml"), "settings `file`")
	fs.BoolVar(&o.dryRun, "dry-run", false, "log embeds instead of posting; don't save state")
	fs.BoolVar(&o.once, "once", false, "process the files once and exit")
	fs.BoolVar(&o.console, "console", false, "run in the console (Windows: instead of the tray)")
	fs.BoolVar(&o.tray, "tray", false, "run as a tray app (Windows only; the default there)")
	fs.Var(&o.testWebhook, "test-webhook", "post a test message to `URL` (default: the configured webhook) and exit")
	fs.BoolVar(&o.verbose, "v", false, "debug logging")
	fs.StringVar(&o.updatedFrom, "updated-from", "", "`version` this one replaced (set by the self-update; don't pass by hand)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	// Flags may come before or after positional arguments
	// ("doink test loot --dry-run").
	var positional []string
	for args := os.Args[1:]; ; {
		if err := fs.Parse(args); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	command := func() string {
		if len(positional) == 0 {
			return ""
		}
		return positional[0]
	}
	if o.testWebhook.set && o.testWebhook.value == "" && command() != "" && command() != "test" && command() != "decode" {
		o.testWebhook.value, positional = positional[0], positional[1:]
	}
	switch command() {
	case "":
	case "test":
		o.testEvents, o.testArgs = true, positional[1:]
	case "decode":
		if len(positional) < 2 {
			fmt.Fprintln(os.Stderr, "usage: doink decode <screenshot>...")
			return 2
		}
		o.decode = positional[1:]
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (did you mean: doink test <type>?)\n", positional[0])
		return 2
	}
	if *showVersion {
		fmt.Println("DOINK companion v" + version)
		return 0
	}

	oneShot := o.testWebhook.set || o.testEvents || o.decode != nil
	consoleOnly := o.once || o.console || o.dryRun || oneShot
	if o.tray && runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "tray mode needs Windows")
		return 1
	}
	if runtime.GOOS == "windows" && (o.tray && !o.once && !oneShot || !o.tray && !consoleOnly) {
		return runTray(o)
	}
	return runConsole(o)
}

func runConsole(o options) int {
	attachConsole()
	if o.decode != nil {
		return decodeScreenshots(o.decode, os.Stdout)
	}
	level := slog.LevelInfo
	if o.verbose {
		level = slog.LevelDebug
	}
	log := slog.New(newLineHandler(os.Stderr, "15:04:05", level))
	slog.SetDefault(log)

	cfg, err := config.Load(o.config, o.dryRun)
	if err != nil && !(errors.Is(err, config.ErrNoWoW) && (o.testWebhook.set || o.testEvents)) {
		log.Error(err.Error())
		return 1
	}
	if o.testWebhook.set {
		return testWebhook(o.testWebhook.value, cfg, log)
	}
	if o.testEvents {
		return runTestEvents(o.testArgs, cfg, log)
	}

	st, err := state.Load(cfg.StatePath, !cfg.DryRun)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	d := deps(cfg, st, status.New(nil), log)
	if o.once {
		if err := runner.RunOnce(d); err != nil {
			log.Error(err.Error())
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reload := func() (*config.Config, error) { return config.Load(o.config, o.dryRun) }
	if err := runner.Run(ctx, d, o.config, reload); err != nil {
		log.Error(err.Error())
		return 1
	}
	log.Info("stopped")
	return 0
}

func deps(cfg *config.Config, st *state.State, s *status.Status, log *slog.Logger) runner.Deps {
	d := runner.Deps{
		Config:  cfg,
		State:   st,
		Sender:  discord.NewPool(nil, cfg.DryRun, version, log),
		Status:  s,
		Log:     log,
		Version: version,
	}
	if cfg.Wowhead {
		d.Items = wowhead.NewCache(func(id int) *wowhead.Item {
			return wowhead.Fetch(nil, wowhead.DefaultBase, version, id)
		})
	}
	return d
}

// gameWebhook is the account-wide webhook set in game, from the first
// watched file that has one.
func gameWebhook(cfg *config.Config) string {
	for _, path := range cfg.SavedVariablesPaths {
		if file, err := os.ReadFile(path); err == nil {
			if url := savedvars.ParseWebhooks(file)["*"]; url != "" {
				return url
			}
		}
	}
	return ""
}

// testWebhook posts the connection-test embed.
func testWebhook(url string, cfg *config.Config, log *slog.Logger) int {
	if url == "" && cfg != nil {
		url = cfg.WebhookURL
		if url == "" {
			url = gameWebhook(cfg)
		}
	}
	if url == "" {
		log.Error("no webhook to test. Pass one: --test-webhook <url>")
		return 1
	}
	if !discord.IsWebhookURL(url) {
		log.Error("that doesn't look like a Discord webhook URL")
		return 1
	}
	pool := discord.NewPool(nil, false, version, log)
	if err := pool.Send(url, []discord.Embed{discord.ConnectionTestEmbed(version)}); err != nil {
		log.Error("test message failed: " + err.Error())
		return 1
	}
	log.Info("test message posted")
	return 0
}
