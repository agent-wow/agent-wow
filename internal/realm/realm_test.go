package realm_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hazim-j/agent-wow/internal/config"
	"github.com/hazim-j/agent-wow/internal/realm"
)

func initRealmConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"authserver":{"host":"configured.example","port":9000}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_WOW_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_WOW_AUTHSERVER_HOST", "localhost")
	t.Setenv("AGENT_WOW_AUTHSERVER_PORT", "3724")
	if err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRealmPersistence(t *testing.T) {
	path := initRealmConfig(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := realm.Selection{ID: 7, Name: "AzerothCore", Address: "127.0.0.1:8085"}
	if err := realm.Save(want); err != nil {
		t.Fatal(err)
	}
	if err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	got := realm.Get()
	if got == nil || *got != want {
		t.Fatalf("selection did not survive reload: %#v", got)
	}
	got.Name = "changed copy"
	if realm.Get().Name != want.Name {
		t.Fatal("Get returned mutable selection")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) {
		t.Fatal("selection changed main config")
	}
	info, err := os.Stat(realm.Path())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("realm file permissions are not 0600")
	}
	assertRealmStorageFields(t)
	t.Setenv("AGENT_WOW_AUTHSERVER_HOST", "other.example")
	if err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	if realm.Get() == nil || *realm.Get() != want {
		t.Fatal("changing the configured authserver erased the selected realm")
	}
}

func TestInvalidRealmPreservesSelection(t *testing.T) {
	initRealmConfig(t)
	want := realm.Selection{ID: 1, Name: "Original", Address: "localhost:8085"}
	if err := realm.Save(want); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []realm.Selection{
		{}, {ID: 2, Name: "Invalid\nName", Address: "localhost:8085"},
		{ID: 2, Name: "Invalid Address", Address: "localhost:0"},
	} {
		before, err := os.ReadFile(realm.Path())
		if err != nil {
			t.Fatal(err)
		}
		if err := realm.Save(selection); err == nil {
			t.Fatal("accepted invalid selection")
		}
		after, err := os.ReadFile(realm.Path())
		if err != nil || string(before) != string(after) || *realm.Get() != want {
			t.Fatal("invalid selection changed config")
		}
	}
}

func TestRealmWriteFailure(t *testing.T) {
	initRealmConfig(t)
	want := realm.Selection{ID: 1, Name: "Original", Address: "localhost:8085"}
	if err := realm.Save(want); err != nil {
		t.Fatal(err)
	}
	// Force the atomic rename to fail, even when tests run as root.
	if err := os.Remove(realm.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(realm.Path(), 0700); err != nil {
		t.Fatal(err)
	}
	next := want
	next.ID = 2
	if err := realm.Save(next); err == nil {
		t.Fatal("ignored failed rename")
	}
	if *realm.Get() != want {
		t.Fatal("failed write changed loaded config")
	}
	matches, err := filepath.Glob(filepath.Join(config.Get().ConfigDir, ".realm-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatal("failed write left a temporary file")
	}
}

func TestMalformedRealmConfig(t *testing.T) {
	path := initRealmConfig(t)
	if err := os.MkdirAll(config.Get().ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{`, `null`, `{}`, `{"id":256,"name":"Bad"}`} {
		if err := os.WriteFile(realm.Path(), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := config.Init(path); err == nil {
			t.Fatalf("accepted malformed realm config %q", data)
		}
	}
}

func TestLegacyRealmConfig(t *testing.T) {
	path := initRealmConfig(t)
	if err := os.MkdirAll(config.Get().ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"id":7,"name":"AzerothCore","address":"127.0.0.1:8085","authserver":"old.example:3724"}`)
	if err := os.WriteFile(realm.Path(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	want := realm.Selection{ID: 7, Name: "AzerothCore", Address: "127.0.0.1:8085"}
	got := realm.Get()
	if got == nil || *got != want {
		t.Fatal("legacy realm config did not load")
	}
	if err := realm.Save(*got); err != nil {
		t.Fatal(err)
	}
	assertRealmStorageFields(t)
}

func assertRealmStorageFields(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(realm.Path())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["id"] == nil || fields["name"] == nil || fields["address"] == nil {
		t.Fatal("realm config must contain only id, name and address")
	}
}
