package hanaexecutesql

import (
	"context"
	"database/sql"
	"fmt"

	yaml "github.com/goccy/go-yaml"
	"github.com/googleapis/genai-toolbox/internal/sources"
	"github.com/googleapis/genai-toolbox/internal/sources/hana"
	"github.com/googleapis/genai-toolbox/internal/tools"
)

const kind string = "hana-execute-sql"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

type compatibleSource interface {
	HanaDB() *sql.DB
}

var _ compatibleSource = &hana.Source{}

var compatibleSources = [...]string{hana.SourceKind}

type Config struct {
	Name         string   `yaml:"name" validate:"required"`
	Kind         string   `yaml:"kind" validate:"required"`
	Source       string   `yaml:"source" validate:"required"`
	Description  string   `yaml:"description" validate:"required"`
	AuthRequired []string `yaml:"authRequired"`
}

var _ tools.ToolConfig = Config{}

func (cfg Config) ToolConfigKind() string { return kind }

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	rawS, ok := srcs[cfg.Source]
	if !ok {
		return nil, fmt.Errorf("no source named %q configured", cfg.Source)
	}

	s, ok := rawS.(compatibleSource)
	if !ok {
		return nil, fmt.Errorf("invalid source for %q tool: source kind must be one of %q", kind, compatibleSources)
	}

	sqlParam := tools.NewStringParameter("sql", "The sql to execute.")
	parameters := tools.Parameters{sqlParam}

	mcpManifest := tools.McpManifest{
		Name:        cfg.Name,
		Description: cfg.Description,
		InputSchema: parameters.McpManifest(),
	}

	t := Tool{
		Name:         cfg.Name,
		Kind:         kind,
		Parameters:   parameters,
		AuthRequired: cfg.AuthRequired,
		DB:           s.HanaDB(),
		manifest:     tools.Manifest{Description: cfg.Description, Parameters: parameters.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest:  mcpManifest,
	}
	return t, nil
}

var _ tools.Tool = Tool{}

type Tool struct {
	Name         string           `yaml:"name"`
	Kind         string           `yaml:"kind"`
	AuthRequired []string         `yaml:"authRequired"`
	Parameters   tools.Parameters `yaml:"parameters"`

	DB          *sql.DB
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
}

func (t Tool) Invoke(ctx context.Context, params tools.ParamValues) (any, error) {
	db := t.DB
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	rows, err := db.QueryContext(ctx, t.manifest.Description)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("unable to get columns: %w", err)
	}

	var results []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		valPtrs := make([]any, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}

		if err := rows.Scan(valPtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowMap := make(map[string]any)
		for i, col := range cols {
			switch v := vals[i].(type) {
			case []byte:
				rowMap[col] = string(v)
			default:
				rowMap[col] = v
			}
		}
		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

func (t Tool) ParseParams(data map[string]any, claims map[string]map[string]any) (tools.ParamValues, error) {
	return tools.ParseParams(t.Parameters, data, claims)
}

func (t Tool) Manifest() tools.Manifest { return t.manifest }

func (t Tool) McpManifest() tools.McpManifest { return t.mcpManifest }

func (t Tool) Authorized(verifiedAuthServices []string) bool {
	return tools.IsAuthorized(t.AuthRequired, verifiedAuthServices)
}
