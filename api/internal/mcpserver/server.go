package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/james-see/everydayisaholiday/api/internal/bearer"
	"github.com/james-see/everydayisaholiday/api/internal/digest"
)

type Server struct {
	Holidays            *digest.Store
	Validator           *bearer.Validator
	ResourceMetadataURL string
	MCPResource         string
	Issuer              string
	handler             http.Handler
}

func New(holidays *digest.Store, validator *bearer.Validator, issuer string) *Server {
	issuer = strings.TrimRight(issuer, "/")
	s := &Server{
		Holidays:            holidays,
		Validator:           validator,
		Issuer:              issuer,
		MCPResource:         issuer + "/mcp",
		ResourceMetadataURL: issuer + "/.well-known/oauth-protected-resource",
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "adayisaholiday",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "holidays_today",
		Description: "List holidays/observances for today in a given IANA timezone (default UTC).",
	}, s.toolToday)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "holidays_by_date",
		Description: "List holidays for a calendar date as MM-DD (e.g. 08-17).",
	}, s.toolByDate)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "holidays_search",
		Description: "Search holidays by name/country, optional category and MM-DD date filter.",
	}, s.toolSearch)

	stream := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{})

	verify := func(ctx context.Context, token string, req *http.Request) (*mcpauth.TokenInfo, error) {
		p, err := s.Validator.ValidateToken(ctx, token)
		if err != nil {
			return nil, mcpauth.ErrInvalidToken
		}
		if err := s.Validator.CheckRateLimit(p); err != nil {
			return nil, mcpauth.ErrInvalidToken
		}
		return &mcpauth.TokenInfo{
			Scopes:     p.Scopes,
			Expiration: time.Now().Add(time.Hour),
			UserID:     fmt.Sprintf("%d", p.UserID),
			Extra: map[string]any{
				"email":    p.Email,
				"auth_via": p.AuthVia,
			},
		}, nil
	}

	s.handler = mcpauth.RequireBearerToken(verify, &mcpauth.RequireBearerTokenOptions{
		Scopes:                 []string{"holidays:read"},
		ResourceMetadataURL:    s.ResourceMetadataURL,
		AllowMissingExpiration: false,
	})(stream)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) ProtectedResourceMetadata() http.Handler {
	meta := &oauthex.ProtectedResourceMetadata{
		Resource:                 s.MCPResource,
		AuthorizationServers:     []string{s.Issuer},
		ScopesSupported:          []string{"holidays:read"},
		BearerMethodsSupported:   []string{"header"},
	}
	return mcpauth.ProtectedResourceMetadataHandler(meta)
}

type todayArgs struct {
	Timezone string `json:"timezone" jsonschema:"IANA timezone, e.g. America/New_York"`
}

func (s *Server) toolToday(ctx context.Context, req *mcp.CallToolRequest, args todayArgs) (*mcp.CallToolResult, any, error) {
	loc := time.UTC
	if args.Timezone != "" {
		if l, err := time.LoadLocation(args.Timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	list := s.Holidays.ForDate(int(now.Month()), now.Day(), nil)
	return textResult(map[string]any{
		"date": now.Format("01-02"), "timezone": loc.String(), "count": len(list), "holidays": list,
	})
}

type byDateArgs struct {
	Date string `json:"date" jsonschema:"Date as MM-DD, e.g. 12-25"`
}

func (s *Server) toolByDate(ctx context.Context, req *mcp.CallToolRequest, args byDateArgs) (*mcp.CallToolResult, any, error) {
	parts := strings.Split(args.Date, "-")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("date must be MM-DD")
	}
	month, err1 := strconv.Atoi(parts[0])
	day, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil, nil, fmt.Errorf("invalid date")
	}
	list := s.Holidays.ForDate(month, day, nil)
	return textResult(map[string]any{"date": args.Date, "count": len(list), "holidays": list})
}

type searchArgs struct {
	Query    string `json:"query" jsonschema:"Search string for name or country"`
	Category string `json:"category" jsonschema:"Optional category filter"`
	Date     string `json:"date" jsonschema:"Optional MM-DD filter"`
	Limit    int    `json:"limit" jsonschema:"Max results (default 50)"`
}

func (s *Server) toolSearch(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
	list := s.Holidays.Search(args.Query, args.Category, args.Date, args.Limit)
	return textResult(map[string]any{"count": len(list), "holidays": list})
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, v, nil
}
