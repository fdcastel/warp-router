package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fdcastel/warp-router/internal/apply"
	"github.com/fdcastel/warp-router/internal/config"
	"github.com/fdcastel/warp-router/internal/health"
	"github.com/fdcastel/warp-router/internal/revision"
)

const defaultConfigPath = "/etc/warp/site.yaml"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "validate":
		cmdValidate()
	case "apply":
		cmdApply()
	case "rollback":
		cmdRollback()
	case "revisions":
		cmdRevisions()
	case "status":
		cmdStatus()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: warp <command> [options]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  validate [config.yaml]   Validate a site config file")
	fmt.Fprintln(os.Stderr, "  apply [config.yaml]      Apply a site config (render + reload services)")
	fmt.Fprintln(os.Stderr, "  rollback                 Rollback to the previous config revision")
	fmt.Fprintln(os.Stderr, "  revisions                List stored config revisions")
	fmt.Fprintln(os.Stderr, "  status                   Show current applied revision")
}

func configPath() string {
	if len(os.Args) > 2 {
		return os.Args[2]
	}
	return defaultConfigPath
}

func cmdValidate() {
	path := configPath()
	cfg, err := config.LoadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	errs := cfg.Validate()
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "Validation failed with %d error(s):\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %v\n", e)
		}
		os.Exit(1)
	}

	fmt.Printf("Config %s is valid.\n", path)
}

func cmdApply() {
	path := configPath()

	// Load and validate config
	cfg, err := config.LoadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	errs := cfg.Validate()
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "Validation failed with %d error(s):\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %v\n", e)
		}
		os.Exit(1)
	}

	// Acquire lock
	unlock, err := apply.AcquireLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer unlock()

	// Save revision
	yamlData, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
		os.Exit(1)
	}

	store := revision.NewStore(revision.DefaultStoreDir)
	revID, err := store.Save(yamlData, "apply from "+path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save revision: %v\n", err)
		// Continue with apply even if revision save fails
	} else {
		fmt.Printf("Saved revision: %s\n", revID)
	}

	// Run apply pipeline
	pipeline := apply.NewPipeline(&apply.SystemdReloader{})
	result := pipeline.Execute(cfg)

	for _, step := range result.Completed {
		fmt.Printf("  ✓ %s\n", step)
	}

	if result.Failed != "" {
		fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", result.Failed, result.Err)
		os.Exit(1)
	}

	fmt.Println("Apply complete.")
}

func cmdRollback() {
	store := revision.NewStore(revision.DefaultStoreDir)

	prevID := store.Previous()
	if prevID == "" {
		fmt.Fprintln(os.Stderr, "Error: no previous revision to rollback to")
		os.Exit(1)
	}

	content, meta, err := store.Get(prevID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading revision %s: %v\n", prevID, err)
		os.Exit(1)
	}

	fmt.Printf("Rolling back to revision %s (%s)\n", meta.ID, meta.Timestamp.Format("2006-01-02 15:04:05 UTC"))

	// Parse the stored config
	cfg, err := config.Parse(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing stored config: %v\n", err)
		os.Exit(1)
	}

	// Acquire lock
	unlock, err := apply.AcquireLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer unlock()

	// Save as new revision (rollback is also a revision)
	_, err = store.Save(content, "rollback to "+prevID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save rollback revision: %v\n", err)
	}

	// Apply
	pipeline := apply.NewPipeline(&apply.SystemdReloader{})
	result := pipeline.Execute(cfg)

	for _, step := range result.Completed {
		fmt.Printf("  ✓ %s\n", step)
	}

	if result.Failed != "" {
		fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", result.Failed, result.Err)
		os.Exit(1)
	}

	fmt.Println("Rollback complete.")
}

func cmdRevisions() {
	store := revision.NewStore(revision.DefaultStoreDir)

	revisions, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing revisions: %v\n", err)
		os.Exit(1)
	}

	if len(revisions) == 0 {
		fmt.Println("No revisions stored.")
		return
	}

	current := store.Current()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REVISION\tTIMESTAMP\tCOMMENT\tCURRENT")
	for _, rev := range revisions {
		marker := ""
		if rev.ID == current {
			marker = "  ←"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			rev.ID,
			rev.Timestamp.Format("2006-01-02 15:04:05"),
			rev.Comment,
			marker,
		)
	}
	w.Flush()
}

func cmdStatus() {
	store := revision.NewStore(revision.DefaultStoreDir)
	current := store.Current()

	if current == "" {
		fmt.Println("No config has been applied yet.")
		return
	}

	_, meta, err := store.Get(current)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading current revision: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Current revision: %s\n", meta.ID)
	fmt.Printf("Applied at:       %s\n", meta.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("SHA256:           %s\n", meta.SHA256)
	if meta.Comment != "" {
		fmt.Printf("Comment:          %s\n", meta.Comment)
	}

	// Show WAN health if available
	report, err := health.ReadStatusFile(health.StatusFilePath)
	if err == nil && len(report.Uplinks) > 0 {
		fmt.Println()
		fmt.Println("WAN Health:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  UPLINK\tSTATUS\tTARGET\tLATENCY\tPROBES\tFAILURES")
		for _, u := range report.Uplinks {
			latency := "-"
			if u.LastLatencyMs > 0 {
				latency = fmt.Sprintf("%.1fms", u.LastLatencyMs)
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%d\t%d\n",
				u.Name, u.Status, u.Target, latency, u.TotalProbes, u.TotalFailures)
		}
		w.Flush()
	}
}
