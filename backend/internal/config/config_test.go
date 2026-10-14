package config

import (
	"reflect"
	"testing"
)

func TestParseDefaults(t *testing.T) {
	got, err := Parse(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Address: "127.0.0.1:8091", DBPath: "./data/openlist2emby.db",
		TaskWorkers: 2, TaskRetentionDays: 30,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuration = %#v, want %#v", got, want)
	}
}

func TestParseEnvironmentAndFlagOverrides(t *testing.T) {
	env := map[string]string{
		"OPENLIST2EMBY_ADDR":                ":9081",
		"OPENLIST2EMBY_DB":                  "/data/openlist2emby.db",
		"OPENLIST2EMBY_TASK_WORKERS":        "4",
		"OPENLIST2EMBY_TASK_RETENTION_DAYS": "7",
	}
	lookup := func(key string) string { return env[key] }
	got, err := Parse(nil, lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Address: ":9081", DBPath: "/data/openlist2emby.db", TaskWorkers: 4, TaskRetentionDays: 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment configuration = %#v, want %#v", got, want)
	}

	got, err = Parse([]string{"-addr", ":9091", "-db", "/tmp/test.db", "-static", "./frontend", "-task-workers", "3", "-task-retention-days", "14"}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	want = Config{Address: ":9091", DBPath: "/tmp/test.db", StaticDir: "./frontend", TaskWorkers: 3, TaskRetentionDays: 14}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flag configuration = %#v, want %#v", got, want)
	}
}

func TestParseRejectsInvalidTaskEnvironment(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"OPENLIST2EMBY_TASK_WORKERS", "invalid"},
		{"OPENLIST2EMBY_TASK_WORKERS", "0"},
		{"OPENLIST2EMBY_TASK_WORKERS", "33"},
		{"OPENLIST2EMBY_TASK_RETENTION_DAYS", "invalid"},
		{"OPENLIST2EMBY_TASK_RETENTION_DAYS", "0"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			_, err := Parse(nil, func(key string) string {
				if key == tc.key {
					return tc.value
				}
				return ""
			})
			if err == nil {
				t.Fatal("invalid task configuration was accepted")
			}
		})
	}
}
