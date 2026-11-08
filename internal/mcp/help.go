package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// helpToolName is the name of the tool returning the services' guides,
// which isn't an RPC.
const helpToolName = "GetHelp"

// serviceGuide is a service's description and the tools of its methods.
type serviceGuide struct {
	name        protoreflect.FullName
	description string
	tools       []string
}

// summary returns the first paragraph of the service's description,
// without a closing sentence that introduces a list, like "A plan is made
// of:", which would read as cut off without the list.
func (g serviceGuide) summary() string {
	summary, _, _ := strings.Cut(g.description, "\n\n")
	if strings.HasSuffix(summary, ":") {
		if i := strings.LastIndex(summary, ". "); i >= 0 {
			summary = summary[:i+1]
		}
	}
	return summary
}

// help serves GetHelp: the full guides of the services, the tools of each,
// and the version of the tool list.
type help struct {
	guides []serviceGuide
	// version identifies the tool list: it changes whenever a tool is
	// added, removed or changed. Clients cache the tool list, and some keep
	// it for a long time after a deploy, so an agent missing a tool can
	// compare it to tell an old tool list from a server without the tool.
	version string
}

// newHelp returns the help of the services with the tools.
func newHelp(guides []serviceGuide, tools []*mcp.Tool) *help {
	hash := sha256.New()
	for _, t := range tools {
		// Tools marshal deterministically: their schemas' properties are
		// maps, which encoding/json sorts.
		data, err := json.Marshal(t)
		if err != nil {
			panic(fmt.Sprintf("marshaling tool %s: %v", t.Name, err))
		}
		hash.Write(data)
	}
	return &help{guides: guides, version: hex.EncodeToString(hash.Sum(nil))[:12]}
}

// instructions returns the server instructions: the preamble, a summary of
// each service, and the tool list's version.
func (h *help) instructions() string {
	parts := []string{instructionsPreamble}
	for _, g := range h.guides {
		if summary := g.summary(); summary != "" {
			parts = append(parts, fmt.Sprintf("## %s\n\n%s", g.name, summary))
		}
	}
	parts = append(parts, fmt.Sprintf("## Tool list\n\nThis server's tool list is version %s. If a tool the guides or the user mention is missing, call GetHelp: it lists the tools the server has now, so a tool it lists that you don't have means your client's tool list is out of date, and reconnecting the server refreshes it.", h.version))
	return strings.Join(parts, "\n\n")
}

// tool returns the definition of GetHelp.
func (h *help) tool() *mcp.Tool {
	var names []string
	for _, g := range h.guides {
		names = append(names, string(g.name.Name()))
	}
	return &mcp.Tool{
		Name:  helpToolName,
		Title: "Get help",
		Description: "Returns the guide of a service: how its tools fit together, with the steps for common tasks. " +
			"With no service, returns every guide. Each guide lists the service's tools, and the result starts with the version of the tool list, " +
			"which the server instructions also give: a tool listed here that your client doesn't have means its tool list is out of date.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"service": {
					Type:        "string",
					Description: "The service, e.g. " + strings.Join(names, ", ") + ". Leave it empty for all of them.",
				},
			},
		},
		Annotations: &mcp.ToolAnnotations{
			Title:          "Get help",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  new(false),
		},
	}
}

// handle serves a GetHelp call.
func (h *help) handle(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		Service string `json:"service"`
	}
	if args := req.Params.Arguments; len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return textResult(true, "Invalid arguments: pass an object with an optional service, e.g. {\"service\": \"PlanService\"}."), nil
		}
	}
	guides := h.guides
	if in.Service != "" {
		i := slices.IndexFunc(h.guides, func(g serviceGuide) bool {
			return strings.EqualFold(string(g.name), in.Service) || strings.EqualFold(string(g.name.Name()), in.Service)
		})
		if i < 0 {
			var names []string
			for _, g := range h.guides {
				names = append(names, string(g.name.Name()))
			}
			return textResult(true, fmt.Sprintf("There's no service %q: use one of %s, or leave service empty for all of them.", in.Service, strings.Join(names, ", "))), nil
		}
		guides = h.guides[i : i+1]
	}
	parts := []string{fmt.Sprintf("Tool list version %s.", h.version)}
	for _, g := range guides {
		parts = append(parts, fmt.Sprintf("## %s\n\n%s\n\nTools: %s.", g.name, g.description, strings.Join(g.tools, ", ")))
	}
	return textResult(false, strings.Join(parts, "\n\n")), nil
}

// textResult returns a tool result of a text.
func textResult(isError bool, text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: isError, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
