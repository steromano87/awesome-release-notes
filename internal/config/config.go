package config

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"text/template"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const DefaultName = ".release-notes"

type Category struct {
	Name       string           `mapstructure:"name"`
	Patterns   []*regexp.Regexp `mapstructure:"patterns"`
	Categories []Category       `mapstructure:"categories"`
}

type Config struct {
	Frontmatter             string             `mapstructure:"frontmatter"`
	Backmatter              string             `mapstructure:"backmatter"`
	CommitPattern           *regexp.Regexp     `mapstructure:"commitPattern"`
	IgnoreUnmatchedCommits  bool               `mapstructure:"ignoreUnmatchedCommits"`
	ExcludedPatterns        []*regexp.Regexp   `mapstructure:"excludedPatterns"`
	ItemTitleTemplate       *template.Template `mapstructure:"itemTitleTemplate"`
	ItemBodyTemplate        *template.Template `mapstructure:"itemBodyTemplate"`
	Categories              []Category         `mapstructure:"categories"`
	CategoryCaseInsensitive bool               `mapstructure:"categoryCaseInsensitive"`
	UncategorizedTitle      string             `mapstructure:"uncategorizedTitle"`
	SectionHeaderLevel      int                `mapstructure:"sectionHeaderLevel"`
	ItemHeaderLevel         int                `mapstructure:"itemHeaderLevel"`
}

var (
	regexpType   = reflect.TypeFor[*regexp.Regexp]()
	templateType = reflect.TypeFor[*template.Template]()
)

// LoadFile reads path (the format comes from its extension), or searches root for DefaultName with any extension viper supports.
func LoadFile(root, path string) (*Config, error) {
	settings := viper.New()
	if path == "" {
		settings.SetConfigName(DefaultName)
		settings.AddConfigPath(root)
	} else {
		settings.SetConfigFile(path)
	}
	if err := settings.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	cfg, err := load(settings)
	if err != nil {
		return nil, fmt.Errorf("configuration %q: %w", settings.ConfigFileUsed(), err)
	}
	return cfg, nil
}

func load(settings *viper.Viper) (*Config, error) {
	for key, value := range map[string]any{
		"commitPattern":      `^(?P<description>[^\n]+)`,
		"itemTitleTemplate":  "{{ .Subject }}",
		"itemBodyTemplate":   "{{ .Body }}",
		"uncategorizedTitle": "Other changes",
		"sectionHeaderLevel": 2,
		"itemHeaderLevel":    3,
	} {
		settings.SetDefault(key, value)
	}
	cfg := &Config{}
	if err := settings.Unmarshal(cfg, func(options *mapstructure.DecoderConfig) {
		options.ErrorUnused = true
		options.WeaklyTypedInput = false
		options.DecodeHook = decodeHook
	}); err != nil {
		return nil, fmt.Errorf("decode configuration: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newTemplate(text string) (*template.Template, error) {
	return template.New("item").Option("missingkey=error").Parse(text)
}

// decodeHook turns strings into regular expressions and templates, and rejects
// fractional numbers that mapstructure would silently truncate (e.g. 2.5).
func decodeHook(from, to reflect.Type, data any) (any, error) {
	if f, ok := data.(float64); ok && to.Kind() == reflect.Int && f != math.Trunc(f) {
		return nil, fmt.Errorf("%v is not an integer", f)
	}
	text, ok := data.(string)
	if !ok || (to != regexpType && to != templateType) {
		return data, nil
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("value must not be empty")
	}
	if to == regexpType {
		return regexp.Compile(text)
	}
	return newTemplate(text)
}

func (cfg *Config) validate() error {
	if strings.TrimSpace(cfg.UncategorizedTitle) == "" || strings.ContainsAny(cfg.UncategorizedTitle, "\r\n") {
		return errors.New("uncategorizedTitle must be a non-empty single line")
	}
	for name, level := range map[string]int{"sectionHeaderLevel": cfg.SectionHeaderLevel, "itemHeaderLevel": cfg.ItemHeaderLevel} {
		if level < 1 || level > 6 {
			return fmt.Errorf("%s must be an integer from 1 to 6", name)
		}
	}
	names := make(map[string]bool)
	for _, name := range cfg.CommitPattern.SubexpNames() {
		if name == "" {
			continue
		}
		if names[name] {
			return fmt.Errorf("duplicate capture name %q in commitPattern", name)
		}
		names[name] = true
	}
	return cfg.validateCategories(cfg.Categories, map[string]bool{cfg.UncategorizedTitle: true}, 0)
}

func (cfg *Config) validateCategories(categories []Category, names map[string]bool, depth int) error {
	for i := range categories {
		category := &categories[i]
		if strings.TrimSpace(category.Name) == "" || strings.ContainsAny(category.Name, "\r\n") {
			return errors.New("category names must be non-empty single lines")
		}
		if names[category.Name] {
			return fmt.Errorf("duplicate category name %q (including uncategorizedTitle)", category.Name)
		}
		names[category.Name] = true
		if cfg.SectionHeaderLevel+depth > 6 || cfg.ItemHeaderLevel+depth > 6 {
			return fmt.Errorf("category %q exceeds Markdown header level 6 at depth %d; reduce sectionHeaderLevel or itemHeaderLevel", category.Name, depth)
		}
		if len(category.Patterns) == 0 && len(category.Categories) == 0 {
			return fmt.Errorf("category %q must have at least one pattern or subcategory", category.Name)
		}
		if cfg.CategoryCaseInsensitive {
			for j, re := range category.Patterns {
				category.Patterns[j] = regexp.MustCompile("(?i)" + re.String())
			}
		}
		if err := cfg.validateCategories(category.Categories, names, depth+1); err != nil {
			return err
		}
	}
	return nil
}
