package coding

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// New keys follow the declared settings order, while existing keys retain
// their positions (core/settings-manager.ts's parsed-object merge).
func TestSettingsNewKeysFollowDeclarationOrder(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	path := filepath.Join(agentDir, "settings.json")
	writeSettingsFile(t, path, `{"custom":"<keep>&","theme":"dark"}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	manager.SetDefaultModelAndProvider("openai", "test-model")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"custom\": \"<keep>&\",\n  \"theme\": \"dark\",\n  \"defaultProvider\": \"openai\",\n  \"defaultModel\": \"test-model\"\n}"
	if got := strings.TrimSpace(string(content)); got != want {
		t.Fatalf("settings = %s, want %s", got, want)
	}
}

// Cache warming is a port-local global setting. Like upstream's
// core/settings-manager.ts setters, it must survive a storage round trip.
func TestSettingsCacheWarmingSurvivesReload(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	manager.SetCacheWarmingMode(CacheWarmingOff)
	manager.Reload()
	if got := manager.GetCacheWarmingMode(); got != CacheWarmingOff {
		t.Fatalf("cache warming after reload = %q, want %q", got, CacheWarmingOff)
	}
}

// Every declared field must survive the typed settings storage boundary. This
// also guards future additions against silently missing serializer branches.
func TestSettingsSchemaFieldsRoundTrip(t *testing.T) {
	settings := &Settings{}
	value := reflect.ValueOf(settings).Elem()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		switch field.Kind() {
		case reflect.Pointer:
			field.Set(reflect.New(field.Type().Elem()))
			if field.Elem().Kind() == reflect.String {
				field.Elem().SetString("<schema>&")
			}
		case reflect.Slice:
			field.Set(reflect.MakeSlice(field.Type(), 0, 0))
		case reflect.Map:
			field.Set(reflect.MakeMap(field.Type()))
		case reflect.Interface:
			field.Set(reflect.ValueOf(float64(0)))
		default:
			t.Fatalf("unsupported optional settings field %s", value.Type().Field(i).Name)
		}
	}
	// Valid union values are needed for their custom decoders.
	settings.QuietStartup = &QuietStartupSetting{Header: true}
	settings.CacheWarming = strPtr(CacheWarmingOff)
	manager := NewInMemorySettingsManager(settings, SettingsManagerCreateOptions{})
	manager.Reload()
	got := reflect.ValueOf(manager.GetGlobalSettings()).Elem()
	for i := 0; i < value.NumField(); i++ {
		if !reflect.DeepEqual(got.Field(i).Interface(), value.Field(i).Interface()) {
			t.Errorf("%s round trip = %#v, want %#v", value.Type().Field(i).Name, got.Field(i).Interface(), value.Field(i).Interface())
		}
	}
}
