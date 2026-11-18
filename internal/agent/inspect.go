package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Options struct {
	Workspace, File, Operation, Gopls, Insert string
	Line, Column                              int
}
type Result struct {
	Operation string          `json:"operation"`
	Workspace string          `json:"workspace"`
	File      string          `json:"file"`
	Position  Position        `json:"position"`
	Server    ServerInfo      `json:"server"`
	Payload   json.RawMessage `json:"result"`
	Timing    Timing          `json:"timing"`
}

// Timing separates protocol request latency from server startup. Request time
// includes any gopls package loading caused by that request; it is not CPU time.
// Process shutdown and document preparation belong to the caller's wall time.
type Timing struct {
	InitializeNanoseconds int64 `json:"initialize_ns"`
	RequestNanoseconds    int64 `json:"request_ns"`
}

// Inspect launches an existing gopls executable, sends one unsaved document and
// returns its actual response. No source files or source-file probes are written.
func Inspect(ctx context.Context, options Options) (Result, error) {
	if options.Gopls == "" {
		options.Gopls = "gopls"
	}
	return inspect(ctx, options, options.Gopls, []string{"serve"})
}
func inspect(ctx context.Context, options Options, executable string, args []string) (Result, error) {
	results, err := inspectBatch(ctx, []Options{options}, 0, executable, args)
	if len(results) == 0 {
		return Result{}, err
	}
	return results[0], err
}

// WriteText gives a compact view while JSON output preserves the complete LSP
// payload, including documentation, edits and location links.
func WriteText(w io.Writer, result Result) error {
	switch result.Operation {
	case "complete":
		var list struct {
			Items []struct {
				Label, Detail string
				Documentation json.RawMessage
			} `json:"items"`
		}
		if err := json.Unmarshal(result.Payload, &list); err != nil {
			if err := json.Unmarshal(result.Payload, &list.Items); err != nil {
				return err
			}
		}
		for _, item := range list.Items {
			if _, err := fmt.Fprintf(w, "%s\t%s\n", item.Label, item.Detail); err != nil {
				return err
			}
			documentation, err := completionDocumentation(item.Documentation)
			if err != nil {
				return err
			}
			if documentation != "" {
				if _, err := fmt.Fprintln(w, "  "+strings.ReplaceAll(strings.TrimSpace(documentation), "\n", "\n  ")); err != nil {
					return err
				}
			}
		}
		return nil
	case "hover":
		var hover struct {
			Contents json.RawMessage `json:"contents"`
		}
		if err := json.Unmarshal(result.Payload, &hover); err != nil {
			return err
		}
		if len(hover.Contents) == 0 {
			return nil
		}
		var content struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(hover.Contents, &content); err == nil {
			_, err = fmt.Fprintln(w, content.Value)
			return err
		}
	}
	var pretty any
	if err := json.Unmarshal(result.Payload, &pretty); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(pretty)
}

// LSP completion documentation is either a string or MarkupContent. Preserve
// the server's actual text, including generated model-field behavior notices.
func completionDocumentation(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var markup struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &markup); err != nil {
		return "", err
	}
	return markup.Value, nil
}
