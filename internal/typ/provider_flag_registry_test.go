package typ

import (
	"reflect"
	"strings"
	"testing"
)

// TestProviderFlagRegistry_KeysMatchStructFields prevents the provider flag
// registry from drifting away from ProviderFlags — the same safety net
// TestRuleFlagRegistry_KeysMatchStructFields provides for rule flags.
func TestProviderFlagRegistry_KeysMatchStructFields(t *testing.T) {
	flagsType := reflect.TypeOf(ProviderFlags{})

	jsonTags := map[string]bool{}
	for i := 0; i < flagsType.NumField(); i++ {
		tag := flagsType.Field(i).Tag.Get("json")
		name := strings.SplitN(tag, ",", 2)[0]
		if name != "" && name != "-" {
			jsonTags[name] = true
		}
	}

	for _, spec := range ProviderFlagRegistry() {
		if !jsonTags[spec.Key] {
			t.Errorf("FlagSpec key %q has no matching json tag on ProviderFlags", spec.Key)
		}
	}
}

// TestProviderFlagRegistry_SpecsAreValid checks the metadata every spec must
// carry — the same contract RuleFlagRegistry specs are held to.
func TestProviderFlagRegistry_SpecsAreValid(t *testing.T) {
	allowedTypes := map[FlagValueType]bool{
		FlagTypeBool:       true,
		FlagTypeString:     true,
		FlagTypeEnum:       true,
		FlagTypeInt:        true,
		FlagTypeServiceRef: true,
		FlagTypeHeaders:    true,
	}
	for _, spec := range ProviderFlagRegistry() {
		if !allowedTypes[spec.Type] {
			t.Errorf("flag %q has unsupported value type %q", spec.Key, spec.Type)
		}
		if spec.Label == "" {
			t.Errorf("flag %q has empty label", spec.Key)
		}
		if spec.Description == "" {
			t.Errorf("flag %q has empty description", spec.Key)
		}
		// Provider flags have no scenario inheritance axis.
		if spec.Shared || spec.InheritanceMode != "" {
			t.Errorf("flag %q must not use the scenario-axis Shared/InheritanceMode fields", spec.Key)
		}
		// The headers control renders free-form rows — enum/suggestion
		// metadata has no meaning for it.
		if spec.Type == FlagTypeHeaders && (len(spec.Options) > 0 || len(spec.Suggestions) > 0) {
			t.Errorf("headers flag %q must not declare Options/Suggestions", spec.Key)
		}
	}
}

// TestProviderFlagRegistry_ExtraHeaders pins the first provider flag: present
// and rendered as the headers control.
func TestProviderFlagRegistry_ExtraHeaders(t *testing.T) {
	var found *FlagSpec
	specs := ProviderFlagRegistry()
	for i := range specs {
		if specs[i].Key == "extra_headers" {
			found = &specs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("extra_headers missing from ProviderFlagRegistry")
	}
	if found.Type != FlagTypeHeaders {
		t.Errorf("extra_headers type = %q, want %q", found.Type, FlagTypeHeaders)
	}
}
