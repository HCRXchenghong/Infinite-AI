package store

import (
	"os"
	"strings"
	"testing"
)

func TestSeedDataDoesNotManageModelRoutes(t *testing.T) {
	raw, err := os.ReadFile("seed.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	if strings.Contains(sql, "seedModelRoutes") {
		t.Fatalf("seed data must not create or restore model routes")
	}
	if strings.Contains(sql, "INSERT INTO model_routes") {
		t.Fatalf("seed data must not insert model routes")
	}
	if strings.Contains(sql, "prompt_text = EXCLUDED.prompt_text") {
		t.Fatalf("seed data must not overwrite admin-edited prompt text")
	}
	if strings.Contains(sql, "upstream_model = EXCLUDED.upstream_model") {
		t.Fatalf("seed data must not overwrite admin-edited model config")
	}
}
