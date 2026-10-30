package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"text/template"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

const DefaultName = ".release-notes"

type Category struct {
	Name       string     `mapstructure:"name"`
	Patterns   []string   `mapstructure:"patterns"`
	Categories []Category `mapstructure:"categories"`
}

type Config struct {
	Frontmatter             string     `mapstructure:"frontmatter"`
	Backmatter              string     `mapstructure:"backmatter"`
	CommitPattern           string     `mapstructure:"commitPattern"`
	IgnoreUnmatchedCommits  bool       `mapstructure:"ignoreUnmatchedCommits"`
	ExcludedPatterns        []string   `mapstructure:"excludedPatterns"`
	ItemTitleTemplate       string     `mapstructure:"itemTitleTemplate"`
	ItemBodyTemplate        string     `mapstructure:"itemBodyTemplate"`
	Categories              []Category `mapstructure:"categories"`
	CategoryCaseInsensitive bool       `mapstructure:"categoryCaseInsensitive"`
	UncategorizedTitle      string     `mapstructure:"uncategorizedTitle"`
	SectionHeaderLevel      int        `mapstructure:"sectionHeaderLevel"`
	ItemHeaderLevel         int        `mapstructure:"itemHeaderLevel"`
}

type CompiledCategory struct {
	Name       string
	Patterns   []*regexp.Regexp
	Categories []CompiledCategory
}

type Compiled struct {
	Config
	Pattern       *regexp.Regexp
	Exclusions    []*regexp.Regexp
	TitleTemplate *template.Template
	BodyTemplate  *template.Template
	Groups        []CompiledCategory
}

func Load(r io.Reader) (*Compiled, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	return load(data, "yaml")
}

func LoadFile(root, path string) (*Compiled, error) {
	if path == "" {
		for _, extension := range []string{"json", "toml", "yaml", "yml"} {
			candidate := filepath.Join(root, DefaultName+"."+extension)
			info, err := os.Stat(candidate)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read configuration: %w", err)
			}
			if !info.IsDir() {
				path = candidate
				break
			}
		}
		if path == "" {
			return nil, fmt.Errorf("read configuration: %s.{json,toml,yaml,yml} not found in %q", DefaultName, root)
		}
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch format {
	case "json", "toml", "yaml", "yml":
	default:
		return nil, fmt.Errorf("unsupported configuration format %q; supported formats: JSON, YAML, TOML", format)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	cfg, err := load(data, format)
	if err != nil {
		return nil, fmt.Errorf("configuration %q: %w", path, err)
	}
	return cfg, nil
}

func load(data []byte, format string) (*Compiled, error) {
	if format == "json" && !json.Valid(data) {
		return nil, errors.New("decode JSON configuration: invalid JSON")
	}
	if format != "toml" {
		if err := validateStructure(data); err != nil {
			return nil, err
		}
	}
	settings := viper.New()
	settings.SetConfigType(format)
	if err := settings.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("decode %s configuration: %w", strings.ToUpper(format), err)
	}
	if format == "toml" {
		schemaData, err := yaml.Marshal(settings.AllSettings())
		if err != nil {
			return nil, fmt.Errorf("encode TOML configuration for schema validation: %w", err)
		}
		if err := validateStructure(schemaData); err != nil {
			return nil, err
		}
	}
	cfg := Config{
		CommitPattern:      `^(?P<description>[^\n]+)`,
		ItemTitleTemplate:  "{{ .Subject }}",
		ItemBodyTemplate:   "{{ .Body }}",
		UncategorizedTitle: "Other changes",
		SectionHeaderLevel: 2,
		ItemHeaderLevel:    3,
	}
	if err := settings.Unmarshal(&cfg, func(options *mapstructure.DecoderConfig) {
		options.WeaklyTypedInput = false
		options.DecodeHook = func(from, to reflect.Value) (any, error) {
			// The schema guards JSON numbers; YAML and TOML require integer literals.
			if format != "json" && to.Kind() == reflect.Int && from.Kind() == reflect.Float64 {
				return nil, errors.New("header levels must be integers from 1 to 6")
			}
			return from.Interface(), nil
		}
	}); err != nil {
		return nil, fmt.Errorf("decode %s configuration: %w", strings.ToUpper(format), err)
	}
	return compile(cfg)
}

func compile(cfg Config) (*Compiled, error) {
	if strings.TrimSpace(cfg.UncategorizedTitle) == "" || strings.ContainsAny(cfg.UncategorizedTitle, "\r\n") {
		return nil, errors.New("uncategorizedTitle must be a non-empty single line")
	}
	pattern, err := regexp.Compile(cfg.CommitPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid commitPattern: %w", err)
	}
	names := make(map[string]bool)
	for _, name := range pattern.SubexpNames() {
		if name == "" {
			continue
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate capture name %q in commitPattern", name)
		}
		names[name] = true
	}
	if strings.TrimSpace(cfg.ItemTitleTemplate) == "" {
		return nil, errors.New("itemTitleTemplate must not be empty")
	}
	titleTemplate, err := template.New("itemTitle").Option("missingkey=error").Parse(cfg.ItemTitleTemplate)
	if err != nil {
		return nil, fmt.Errorf("invalid itemTitleTemplate: %w", err)
	}
	if strings.TrimSpace(cfg.ItemBodyTemplate) == "" {
		return nil, errors.New("itemBodyTemplate must not be empty")
	}
	bodyTemplate, err := template.New("itemBody").Option("missingkey=error").Parse(cfg.ItemBodyTemplate)
	if err != nil {
		return nil, fmt.Errorf("invalid itemBodyTemplate: %w", err)
	}
	result := &Compiled{
		Config: cfg, Pattern: pattern,
		TitleTemplate: titleTemplate, BodyTemplate: bodyTemplate,
	}
	for i, expression := range cfg.ExcludedPatterns {
		re, err := regexp.Compile(expression)
		if err != nil {
			return nil, fmt.Errorf("invalid excludedPatterns[%d]: %w", i, err)
		}
		result.Exclusions = append(result.Exclusions, re)
	}
	names = map[string]bool{cfg.UncategorizedTitle: true}
	result.Groups, err = cfg.compileCategories(cfg.Categories, names, 0)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (cfg Config) compileCategories(categories []Category, names map[string]bool, depth int) ([]CompiledCategory, error) {
	var groups []CompiledCategory
	for _, category := range categories {
		if strings.TrimSpace(category.Name) == "" || strings.ContainsAny(category.Name, "\r\n") {
			return nil, errors.New("category names must be non-empty single lines")
		}
		if names[category.Name] {
			return nil, fmt.Errorf("duplicate category name %q (including uncategorizedTitle)", category.Name)
		}
		names[category.Name] = true
		if cfg.SectionHeaderLevel+depth > 6 || cfg.ItemHeaderLevel+depth > 6 {
			return nil, fmt.Errorf("category %q exceeds Markdown header level 6 at depth %d; reduce sectionHeaderLevel or itemHeaderLevel", category.Name, depth)
		}
		if len(category.Patterns) == 0 && len(category.Categories) == 0 {
			return nil, fmt.Errorf("category %q must have at least one pattern or subcategory", category.Name)
		}
		group := CompiledCategory{Name: category.Name}
		for _, expression := range category.Patterns {
			if cfg.CategoryCaseInsensitive {
				expression = "(?i)" + expression
			}
			re, err := regexp.Compile(expression)
			if err != nil {
				return nil, fmt.Errorf("invalid pattern in category %q: %w", category.Name, err)
			}
			group.Patterns = append(group.Patterns, re)
		}
		children, err := cfg.compileCategories(category.Categories, names, depth+1)
		if err != nil {
			return nil, err
		}
		group.Categories = children
		groups = append(groups, group)
	}
	return groups, nil
}
