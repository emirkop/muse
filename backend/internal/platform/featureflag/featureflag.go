package featureflag

import (
	"fmt"
	"sort"
	"strings"
)

type Flag struct {
	name string
}

func (f Flag) Name() string { return f.name }

func (f Flag) EnvironmentVariable() string {
	if f.name == "" {
		return ""
	}
	return envPrefix + strings.ToUpper(strings.ReplaceAll(f.name, "-", "_"))
}

const envPrefix = "MUSE_FLAG_"

var (
	VisitorAudibleRoomMusic = Flag{"visitor_audible_room_music"}
)

type Definition struct {
	Default bool

	Summary string

	RequiresExternalClearance bool

	ClearanceRequired string
}

var definitions = map[Flag]Definition{
	VisitorAudibleRoomMusic: {
		Default:                   false,
		Summary:                   "serve Room music references to visitors",
		RequiresExternalClearance: true,
		ClearanceRequired: "confirmed licensing/business/legal clearance, principally whether " +
			"playback to visitors in a shared virtual Room is a public performance requiring PRO licensing",
	},
}

type FeatureFlagProviding interface {
	IsEnabled(Flag) bool
}

type Provider struct {
	environment string
	overrides   map[Flag]bool
}

const (
	valueEnabled  = "enabled"
	valueDisabled = "disabled"
)

func NewProvider(environment string, environ []string) (*Provider, error) {
	byVariable := make(map[string]Flag, len(definitions))
	for flag := range definitions {
		byVariable[flag.EnvironmentVariable()] = flag
	}

	overrides := map[Flag]bool{}
	var problems []string
	for _, entry := range environ {
		key, rawValue, found := strings.Cut(entry, "=")
		if !found || !strings.HasPrefix(key, envPrefix) {
			continue
		}
		flag, known := byVariable[key]
		if !known {
			problems = append(problems, fmt.Sprintf(
				"%s names no known flag (known: %s)", key, strings.Join(variableNames(), ", ")))
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rawValue)) {
		case valueEnabled:
			overrides[flag] = true
		case valueDisabled:
			overrides[flag] = false
		default:
			problems = append(problems, fmt.Sprintf(
				"%s=%q is not %q or %q", key, rawValue, valueEnabled, valueDisabled))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("featureflag: %s", strings.Join(problems, "; "))
	}
	return &Provider{environment: environment, overrides: overrides}, nil
}

func (p *Provider) IsEnabled(flag Flag) bool {
	if flag.name == "" {
		return false
	}
	definition, known := definitions[flag]
	if !known {
		return false
	}
	if override, set := p.overrides[flag]; set {
		return override
	}
	return definition.Default
}

type Status struct {
	Name                      string
	Enabled                   bool
	Default                   bool
	Overridden                bool
	RequiresExternalClearance bool
	ClearanceRequired         string
	Summary                   string
	EnvironmentVariable       string
}

func (p *Provider) Snapshot() []Status {
	statuses := make([]Status, 0, len(definitions))
	for flag, definition := range definitions {
		override, overridden := p.overrides[flag]
		enabled := definition.Default
		if overridden {
			enabled = override
		}
		statuses = append(statuses, Status{
			Name:                      flag.Name(),
			Enabled:                   enabled,
			Default:                   definition.Default,
			Overridden:                overridden,
			RequiresExternalClearance: definition.RequiresExternalClearance,
			ClearanceRequired:         definition.ClearanceRequired,
			Summary:                   definition.Summary,
			EnvironmentVariable:       flag.EnvironmentVariable(),
		})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	return statuses
}

func (p *Provider) Environment() string { return p.environment }

func variableNames() []string {
	names := make([]string, 0, len(definitions))
	for flag := range definitions {
		names = append(names, flag.EnvironmentVariable())
	}
	sort.Strings(names)
	return names
}

func AllFlags() []Flag {
	flags := make([]Flag, 0, len(definitions))
	for flag := range definitions {
		flags = append(flags, flag)
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].name < flags[j].name })
	return flags
}
