package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
)

func main() {
	var (
		cfgPath      = flag.String("config", "", "path to JSON config (empty = defaults + env)")
		dumpDir      = flag.String("dump-dir", "", "directory for routing table dumps (empty = disabled)")
		dumpInterval = flag.Duration("dump-interval", 0, "periodic dump interval (0 = only initial dump)")
		lookupHex    = flag.String("lookup", "", "hex NodeID to run FIND_NODE after bootstrap (overrides SETI_LOOKUP_TARGET)")
		readyFile    = flag.String("ready-file", "", "touch this file once bootstrap completes")
		bootDelay    = flag.Duration("boot-delay", 200*time.Millisecond, "delay before bootstrap to let listener settle")
	)
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	nd, err := node.New(cfg)
	if err != nil {
		log.Fatalf("node: %v", err)
	}

	log.Printf("node id: %s", hex.EncodeToString(nd.NodeID()))

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := nd.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(*bootDelay):
	}

	if err := nd.Bootstrap(ctx); err != nil {
		log.Printf("bootstrap: %v", err)
	}

	if len(cfg.BootstrapPeers) == 0 && nd.TableSize() > 0 {
		res, err := nd.Lookup(ctx, nd.NodeID())
		if err != nil {
			log.Printf("seed self-lookup: %v", err)
		} else {
			log.Printf("seed self-lookup done: success=%v rpcs=%d iters=%d closest=%d",
				res.Success, res.RPCs, res.Iterations, len(res.Closest))
		}
	}

	log.Printf("node ready, table=%d contacts", nd.TableSize())

	if *dumpDir != "" {
		prefix := fmt.Sprintf("routing-%s", shortHex(nd.NodeID(), 4))
		initial := filepath.Join(*dumpDir, prefix+"-initial.json")
		if err := nd.DumpRoutingTable(initial); err != nil {
			log.Printf("dump: %v", err)
		} else {
			log.Printf("routing table dumped to %s", initial)
		}
		if *dumpInterval > 0 {
			go periodicDump(ctx, nd, *dumpDir, prefix, *dumpInterval)
		}
	}

	if *readyFile != "" {
		_ = os.MkdirAll(filepath.Dir(*readyFile), 0o755)
		_ = os.WriteFile(*readyFile, []byte(hex.EncodeToString(nd.NodeID())), 0o644)
	}

	lookupTarget := *lookupHex
	if lookupTarget == "" {
		lookupTarget = os.Getenv("SETI_LOOKUP_TARGET")
	}
	if lookupTarget != "" {
		target, err := hex.DecodeString(lookupTarget)
		if err != nil || len(target) != 32 {
			log.Printf("lookup: invalid hex NodeID (want 64 hex chars, got %q)", lookupTarget)
		} else {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			runLookup(ctx, nd, target)
		}
	}

	go watchLookupTrigger(ctx, nd, cfg.StateDir)

	<-ctx.Done()
	log.Printf("shutting down")
}

func periodicDump(ctx context.Context, nd *node.Node, dir, prefix string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			path := filepath.Join(dir,
				fmt.Sprintf("%s-%d.json", prefix, time.Now().UnixMilli()))
			if err := nd.DumpRoutingTable(path); err != nil {
				log.Printf("periodic dump: %v", err)
			}
		}
	}
}

func watchLookupTrigger(ctx context.Context, nd *node.Node, stateDir string) {
	trigger := filepath.Join(stateDir, "lookup.trigger")
	done := filepath.Join(stateDir, "lookup.done")
	invalid := filepath.Join(stateDir, "lookup.done.invalid")

	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			raw, err := os.ReadFile(trigger)
			if err != nil {
				continue
			}
			target, err := hex.DecodeString(string(bytes.TrimSpace(raw)))
			if err != nil || len(target) != 32 {
				log.Printf("lookup trigger: invalid hex %q", string(raw))
				_ = os.Rename(trigger, invalid)
				continue
			}
			runLookup(ctx, nd, target)
			_ = os.Rename(trigger, done)
		}
	}
}

func runLookup(ctx context.Context, nd *node.Node, target []byte) {
	res, err := nd.Lookup(ctx, target)
	if err != nil {
		log.Printf("lookup: %v", err)
		return
	}
	log.Printf("lookup target=%s success=%v rpcs=%d iters=%d timeouts=%d duration=%dms closest=%d",
		shortHex(target, 4), res.Success, res.RPCs, res.Iterations,
		res.Timeouts, res.DurationMs, len(res.Closest))
	for _, c := range res.Closest {
		log.Printf("  closest: %s @ %s:%d", shortHex(c.NodeID, 4), c.Host, c.Port)
	}
}

func shortHex(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return hex.EncodeToString(b[:n])
}