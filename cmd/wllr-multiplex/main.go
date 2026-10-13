// Command wllr-multiplex is a small multi-instance client for wllr: it connects
// to N running `wllr serve` instances, merges their event streams, and can read
// (tail, agents, models, history) and write (submit, cancel) through one tool.
//
// It is a plain HTTP/SSE client: it does not embed the wllr engine, so it is
// safe to run many of them against many servers. Instances are declared either
// as repeatable `-instance id=url` flags or in a JSON config file.
//
// Usage:
//
//	wllr-multiplex -instance local=http://127.0.0.1:8765 -instance ci=http://127.0.0.1:8766 tail
//	wllr-multiplex -config instances.json agents
//	wllr-multiplex -config instances.json submit local/main "summarize README.md"
//	wllr-multiplex -config instances.json models
//	wllr-multiplex -config instances.json history ci/main
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mattdurham/wllr/modules/client"
	"github.com/mattdurham/wllr/modules/wire"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wllr-multiplex:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("wllr-multiplex", flag.ContinueOnError)
	var instances client.InstanceFlags
	fs.Var(&instances, "instance", "instance as id=url (repeatable)")
	configPath := fs.String("config", "", "JSON file with [{id,url,token}] instances")
	token := fs.String(
		"token",
		os.Getenv("WLLR_TOKEN"),
		"bearer token applied to instances without their own",
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()

	cfgs, err := client.BuildInstances(instances, *configPath, *token, "instance", "config")
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(rest) == 0 {
		return errors.New(
			"usage: wllr-multiplex [flags] <tail|agents|models|history|submit|cancel> [args]",
		)
	}
	verb, verbArgs := rest[0], rest[1:]

	// tail streams from many instances; every other verb is a targeted REST
	// call, so it takes one client rather than a multiplexer.
	if verb == "tail" {
		return runTail(ctx, cfgs)
	}
	mux, err := client.NewMultiplexer(cfgs, client.MultiplexerOptions{})
	if err != nil {
		return err
	}
	return runCommand(ctx, mux, verb, verbArgs)
}

// runCommand dispatches a targeted REST verb.
func runCommand(ctx context.Context, mux *client.Multiplexer, verb string, args []string) error {
	switch verb {
	case "agents":
		return cmdAgents(ctx, mux)
	case "models":
		return cmdModels(ctx, mux)
	case "history":
		if len(args) < 1 {
			return errors.New("usage: history <instance/agent>")
		}
		return cmdHistory(ctx, mux, args[0])
	case "submit":
		if len(args) < 2 {
			return errors.New("usage: submit <instance/agent> <prompt>")
		}
		return cmdSubmit(ctx, mux, args[0], strings.Join(args[1:], " "))
	case "cancel":
		if len(args) < 1 {
			return errors.New("usage: cancel <instance/agent>")
		}
		return cmdCancel(ctx, mux, args[0])
	default:
		return fmt.Errorf("unknown command %q", verb)
	}
}

// resolve splits an "instance/agent" reference and returns the client and agent.
func resolve(mux *client.Multiplexer, ref string) (*client.Client, string, error) {
	ar := client.ParseAgentRef(ref)
	c := mux.Client(ar.Instance)
	if c == nil {
		return nil, "", fmt.Errorf(
			"unknown instance %q (have %s)",
			ar.Instance,
			strings.Join(mux.Instances(), ", "),
		)
	}
	return c, ar.Agent, nil
}

func cmdAgents(ctx context.Context, mux *client.Multiplexer) error {
	for _, c := range mux.Clients() {
		agents, err := c.Agents(ctx)
		if err != nil {
			fmt.Printf("%s: ERROR %v\n", c.ID(), err)
			continue
		}
		for _, a := range agents {
			flag := " "
			if a.Main {
				flag = "*"
			}
			fmt.Printf("%s %s/%s running=%v inbox=%d history=%d tool=%s\n",
				flag, c.ID(), a.ID, a.Running, a.Inbox, a.HistoryLen, a.LastTool)
		}
	}
	return nil
}

func cmdModels(ctx context.Context, mux *client.Multiplexer) error {
	for _, c := range mux.Clients() {
		m, err := c.Models(ctx)
		if err != nil {
			fmt.Printf("%s: ERROR %v\n", c.ID(), err)
			continue
		}
		fmt.Printf(
			"%s: %d models (current %s/%s)\n",
			c.ID(),
			len(m.Models),
			m.Current["provider"],
			m.Current["model"],
		)
		for _, mm := range m.Models {
			mark := " "
			if mm.Active {
				mark = "*"
			}
			fmt.Printf("  %s %s [%s] %s\n", mark, mm.Key, mm.Provider, mm.Name)
		}
	}
	return nil
}

func cmdHistory(ctx context.Context, mux *client.Multiplexer, ref string) error {
	c, agent, err := resolve(mux, ref)
	if err != nil {
		return err
	}
	h, err := c.History(ctx, client.HistoryOptions{Agent: agent})
	if err != nil {
		return err
	}
	fmt.Printf("%s/%s: %d messages\n", c.ID(), h.AgentID, h.Total)
	for _, m := range h.Messages {
		// The server already rendered HTML; for a terminal, show the raw content.
		fmt.Printf("--- %s ---\n%s\n", m.Role, m.Content)
	}
	return nil
}

func cmdSubmit(ctx context.Context, mux *client.Multiplexer, ref, prompt string) error {
	c, agent, err := resolve(mux, ref)
	if err != nil {
		return err
	}
	if err := c.Submit(ctx, agent, prompt); err != nil {
		return err
	}
	fmt.Printf("submitted to %s (accepted)\n", ref)
	return nil
}

func cmdCancel(ctx context.Context, mux *client.Multiplexer, ref string) error {
	c, agent, err := resolve(mux, ref)
	if err != nil {
		return err
	}
	if err := c.Cancel(ctx, agent); err != nil {
		return err
	}
	fmt.Printf("cancelled %s\n", ref)
	return nil
}

// runTail follows every instance and prints a merged, prefixed line stream until
// interrupted.
func runTail(ctx context.Context, cfgs []client.Config) error {
	mux, err := client.NewMultiplexer(cfgs, client.MultiplexerOptions{
		Follow: client.FollowOptions{
			OnError: func(err error) {
				fmt.Fprintf(os.Stderr, "wllr-multiplex: stream error: %v\n", err)
			},
		},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wllr-multiplex: tails %v\n", mux.Instances())

	// Run and consume concurrently: Run blocks until the context ends.
	go func() {
		if err := mux.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "wllr-multiplex: %v\n", err)
		}
	}()
	for ev := range mux.Events() {
		printEvent(ev)
	}
	return nil
}

// printEvent renders one merged event as a single prefixed line, keeping the
// output greppable and instance-attributable.
func printEvent(t client.Tagged) {
	prefix := t.AgentKey()
	if t.Gap {
		fmt.Printf("%s [gap]\n", prefix)
	}
	ev := t.Event
	switch ev.Kind {
	case wire.KindToken:
		fmt.Printf("%s token %q\n", prefix, ev.Text)
	case wire.KindUserMessage:
		fmt.Printf("%s user %q\n", prefix, ev.Text)
	case wire.KindAssistantMessage:
		fmt.Printf("%s assistant %q\n", prefix, ev.Text)
	case wire.KindTurnStarted:
		fmt.Printf("%s turn_started\n", prefix)
	case wire.KindTurnDone:
		if ev.Err != "" {
			fmt.Printf("%s turn_done error=%q\n", prefix, ev.Err)
		} else {
			fmt.Printf("%s turn_done\n", prefix)
		}
	case wire.KindToolCall:
		fmt.Printf("%s tool_call %s\n", prefix, ev.ToolName)
	case wire.KindToolResult:
		fmt.Printf("%s tool_result %s error=%v\n", prefix, ev.ToolName, ev.IsError)
	case wire.KindNotify:
		fmt.Printf("%s notify %q\n", prefix, ev.Text)
	case wire.KindError:
		fmt.Printf("%s error %q\n", prefix, ev.Err)
	case wire.KindAgentSpawned:
		fmt.Printf("%s agent_spawned %s live=%d\n", prefix, ev.Name, ev.Live)
	case wire.KindAgentClosed:
		fmt.Printf("%s agent_closed %s live=%d\n", prefix, ev.Name, ev.Live)
	case wire.KindReady:
		fmt.Printf("%s ready protocol=%d last_seq=%d\n", prefix, ev.Protocol, ev.Seq)
	case wire.KindContextUsage:
		if ev.Usage != nil {
			fmt.Printf(
				"%s context %.1f%% %d/%d\n",
				prefix,
				ev.Usage.Percent,
				ev.Usage.InputTokens,
				ev.Usage.ContextWindow,
			)
		}
	default:
		fmt.Printf("%s %s\n", prefix, ev.Kind)
	}
}
