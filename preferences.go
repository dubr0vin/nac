package main

import (
	"bytes"
	"io"
	"strings"

	"github.com/labstack/echo/v4"
	"go.yaml.in/yaml/v3"
)

// YAML uses the public rule syntax; JSON keeps the existing API representation.
func (r Rule) MarshalYAML() (any, error) {
	switch r.Op {
	case "true", "false":
		return r.Op == "true", nil
	case "tag":
		return map[string]any{"tag": r.Tag}, nil
	case "not":
		return map[string]any{"not": r.Children[0]}, nil
	default:
		if len(r.Children) == 0 {
			return r.Op == "and", nil
		}
		return map[string]any{r.Op: r.Children}, nil
	}
}

func (r *Rule) UnmarshalYAML(node *yaml.Node) error {
	value, err := yamlRule(node, 0)
	*r = value
	return err
}

func yamlRule(node *yaml.Node, depth int) (Rule, error) {
	rule := Rule{}
	if depth > 20 {
		return rule, problem("rule_too_large").With("line", node.Line)
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		rule.Op = node.Value
	} else if node.Kind == yaml.MappingNode && len(node.Content) == 2 {
		key, value := node.Content[0], node.Content[1]
		rule.Op = key.Value
		switch rule.Op {
		case "tag":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return rule, problem("tag_string_required").With("line", value.Line)
			}
			rule.Tag = value.Value
		case "not", "and", "or":
			children := []*yaml.Node{value}
			if rule.Op != "not" {
				if value.Kind != yaml.SequenceNode {
					return rule, problem("rule_list_required").With("line", value.Line).With("operator", rule.Op)
				}
				children = value.Content
			}
			if len(children) > 100 {
				return rule, problem("rule_too_large").With("line", node.Line)
			}
			for _, child := range children {
				item, err := yamlRule(child, depth+1)
				if err != nil {
					return rule, err
				}
				rule.Children = append(rule.Children, item)
			}
		default:
			return rule, problem("unknown_rule").With("line", key.Line).With("operator", key.Value)
		}
	} else {
		return rule, problem("invalid_rule").With("line", node.Line)
	}
	if err := rule.Validate(depth); err != nil {
		return rule, asProblem(err).With("line", node.Line)
	}
	return rule, nil
}

type yamlColorRule struct {
	When  Rule      `yaml:"when"`
	Color yaml.Node `yaml:"color"`
}

type preferences struct {
	Tags        []string `yaml:"tags,flow"`
	DefaultTags struct {
		Created []string `yaml:"created,flow"`
		Invited []string `yaml:"invited,flow"`
	} `yaml:"default_tags"`
	Busy   Rule            `yaml:"busy"`
	Colors []yamlColorRule `yaml:"colors"`
}

func settingsYAML(settings Settings) (string, error) {
	if settings.Config != "" {
		return settings.Config, nil
	}
	var config preferences
	config.Tags, config.Busy = settings.Tags, settings.Busy
	config.DefaultTags.Created, config.DefaultTags.Invited = settings.OwnTags, settings.IncomingTags
	for _, color := range settings.Colors {
		var node yaml.Node
		var value any = color.Color
		if color.Stripe != "" {
			value = []string{color.Color, color.Stripe}
		}
		if err := node.Encode(value); err != nil {
			return "", err
		}
		node.Style = yaml.FlowStyle
		config.Colors = append(config.Colors, yamlColorRule{color.Rule, node})
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	err := encoder.Encode(config)
	return out.String(), err
}

func parseSettingsYAML(text string, settings Settings) (Settings, error) {
	if len(text) > 65536 {
		return settings, problem("config_too_large")
	}
	decoder := yaml.NewDecoder(strings.NewReader(text))
	decoder.KnownFields(true)
	var config preferences
	if err := decoder.Decode(&config); err != nil {
		return settings, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return settings, problem("single_yaml_document")
	}
	if err := config.Busy.Validate(0); err != nil {
		return settings, asProblem(err).With("field", "busy")
	}
	settings.Tags, settings.Busy = config.Tags, config.Busy
	settings.OwnTags, settings.IncomingTags = config.DefaultTags.Created, config.DefaultTags.Invited
	settings.Colors = []ColorRule{}
	for _, item := range config.Colors {
		var colors []string
		if item.Color.Kind == yaml.ScalarNode && item.Color.Tag == "!!str" {
			colors = []string{item.Color.Value}
		} else if err := item.Color.Decode(&colors); err != nil {
			return settings, err
		}
		if len(colors) < 1 || len(colors) > 2 {
			return settings, problem("color_count").With("line", item.Color.Line)
		}
		for _, color := range colors {
			if !validColor(color) {
				return settings, problem("invalid_color").With("line", item.Color.Line)
			}
		}
		color := ColorRule{Rule: item.When, Color: colors[0]}
		if len(colors) == 2 {
			color.Stripe = colors[1]
		}
		if err := color.Rule.Validate(0); err != nil {
			return settings, asProblem(err).With("line", item.Color.Line).With("field", "when")
		}
		settings.Colors = append(settings.Colors, color)
	}
	if len(settings.Colors) == 0 || settings.Colors[len(settings.Colors)-1].Rule.Op != "true" {
		return settings, problem("fallback_color_required")
	}
	settings.Config = text
	return settings, settings.Validate()
}

func (a *App) putPreferences(c echo.Context) error {
	var input struct {
		YAML     string `json:"yaml"`
		Timezone string `json:"timezone"`
		Poll     int    `json:"poll"`
	}
	if err := c.Bind(&input); err != nil {
		return err
	}
	user := currentUser(c)
	user.Settings.Timezone, user.Settings.Poll = input.Timezone, input.Poll
	settings, err := parseSettingsYAML(input.YAML, user.Settings)
	if err != nil {
		return badRequest(err)
	}
	user.Settings = settings
	if err := a.db.Model(&user).Select("Settings").Updates(user).Error; err != nil {
		return err
	}
	return c.JSON(200, settings)
}
